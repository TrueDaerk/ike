package manager

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ike/internal/lsp/protocol"
	"ike/internal/pathglob"
)

// willrename.go serves workspace/willRenameFiles (#1912): before the explorer
// renames or moves a file/folder on disk, every running server that registered
// interest (fileOperations.willRename with a matching filter) is asked for the
// workspace edit the rename implies — gopls rewrites the import paths of a
// moved package this way. Unlike the per-document features this fans out over
// all servers: a rename is workspace-wide, not bound to one open buffer.

// WillRenameFiles asks every interested server for the edits a pending rename
// of oldPath to newPath implies and returns them merged, ordered by path.
// Best-effort per server: a slow or failing server only loses its own edits —
// the rename itself must never hinge on one server's health. ctx bounds the
// whole fan-out; each individual call additionally gets the usual request
// timeout.
func (m *Manager) WillRenameFiles(ctx context.Context, oldPath, newPath string) ([]FileEdits, error) {
	isDir := false
	if fi, err := os.Stat(oldPath); err == nil {
		isDir = fi.IsDir()
	}

	m.mu.Lock()
	servers := make([]*server, 0, len(m.servers))
	for _, srv := range m.servers {
		servers = append(servers, srv)
	}
	m.mu.Unlock()
	// Deterministic fan-out order, so merged edits are stable across runs.
	sort.Slice(servers, func(i, j int) bool { return servers[i].key() < servers[j].key() })

	files := []protocol.FileRename{{
		OldURI: protocol.PathToURI(oldPath),
		NewURI: protocol.PathToURI(newPath),
	}}
	var out []FileEdits
	for _, srv := range servers {
		caps := srv.cl.Caps()
		if !caps.WillRename || !filtersMatch(caps.WillRenameFilters, oldPath, srv.root, isDir) {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, requestTimeout)
		we, err := srv.cl.WillRenameFiles(cctx, protocol.RenameFilesParams{Files: files})
		cancel()
		if err != nil || we == nil {
			continue // best-effort: this server's edits are lost, the rename is not
		}
		out = append(out, m.convertWorkspaceEdit(*we, srv.cl.Encoding())...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// filtersMatch reports whether at least one file-operation filter matches
// path. No filters means the server wants every operation (the registration
// itself is the announcement).
func filtersMatch(filters []protocol.FileOperationFilter, path, root string, isDir bool) bool {
	if len(filters) == 0 {
		return true
	}
	for _, f := range filters {
		if matchesFileOperation(f, path, root, isDir) {
			return true
		}
	}
	return false
}

// matchesFileOperation evaluates one filter: the scheme must be "file" (or
// unset), the matches kind must fit the operand (file vs folder), and the glob
// must match the path — absolute, or relative to the server root for anchored
// patterns like `**/*.go` written against the workspace. The matcher is the
// shared internal/pathglob (`**`, `*`, `?`, `{a,b}`, `[...]`).
func matchesFileOperation(f protocol.FileOperationFilter, path, root string, isDir bool) bool {
	if f.Scheme != "" && f.Scheme != "file" {
		return false
	}
	switch f.Pattern.Matches {
	case "file":
		if isDir {
			return false
		}
	case "folder":
		if !isDir {
			return false
		}
	}
	pat := filepath.ToSlash(f.Pattern.Glob)
	slashPath := filepath.ToSlash(path)
	if pathglob.Match(pat, slashPath) {
		return true
	}
	if rel, err := filepath.Rel(root, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return pathglob.Match(pat, filepath.ToSlash(rel))
	}
	return false
}
