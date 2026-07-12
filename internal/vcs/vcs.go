// Package vcs owns all Git interaction for IKE (Roadmap 0320). It shells out
// to the git CLI — never from Update: every call runs inside a tea.Cmd with a
// timeout. The package produces immutable Snapshots of the repository state
// (branch, ahead/behind, per-file status) that consumers (explorer coloring,
// status line, commit UI) read; consumers never run git themselves.
package vcs

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// gitTimeout bounds every git subprocess. Status/rev-parse on a healthy repo
// is milliseconds; the timeout only guards against hung filesystems.
const gitTimeout = 5 * time.Second

// FileStatus classifies one path in the working tree relative to HEAD/index.
type FileStatus int

const (
	StatusNone FileStatus = iota
	StatusModified
	StatusAdded
	StatusDeleted
	StatusRenamed
	StatusUntracked
	StatusConflicted
)

// String returns the single-letter JetBrains-style badge for the status.
func (s FileStatus) String() string {
	switch s {
	case StatusModified:
		return "M"
	case StatusAdded:
		return "A"
	case StatusDeleted:
		return "D"
	case StatusRenamed:
		return "R"
	case StatusUntracked:
		return "?"
	case StatusConflicted:
		return "U"
	}
	return ""
}

// Snapshot is one immutable view of the repository state. A nil *Snapshot
// means "not a git repository" — consumers must treat that as a clean no-op.
type Snapshot struct {
	// Root is the absolute repository top-level directory.
	Root string
	// Branch is the current branch name; for a detached HEAD it holds the
	// short commit hash and Detached is true.
	Branch   string
	Detached bool
	// Ahead/Behind count commits relative to the upstream; both zero when no
	// upstream is configured.
	Ahead  int
	Behind int
	// Files maps repo-relative slash-separated paths to their status. Clean
	// files are absent.
	Files map[string]FileStatus
	// dirs holds every repo-relative directory (slash-separated, "" for the
	// root) that contains at least one changed file, for explorer tinting.
	dirs map[string]bool
}

// NewSnapshot builds a snapshot from explicit per-file statuses (repo-relative
// slash paths), propagating dirty directories — for tests and synthetic states.
func NewSnapshot(root string, files map[string]FileStatus) *Snapshot {
	s := &Snapshot{Root: root, Files: map[string]FileStatus{}, dirs: map[string]bool{}}
	for p, st := range files {
		s.add(p, st)
	}
	return s
}

// Status reports the status of path, which may be absolute or repo-relative.
// It returns StatusNone for clean files, paths outside the repo, or a nil
// snapshot.
func (s *Snapshot) Status(path string) FileStatus {
	if s == nil {
		return StatusNone
	}
	rel, ok := s.relPath(path)
	if !ok {
		return StatusNone
	}
	return s.Files[rel]
}

// DirDirty reports whether the directory at path (absolute or repo-relative)
// contains at least one changed file, at any depth.
func (s *Snapshot) DirDirty(path string) bool {
	if s == nil {
		return false
	}
	rel, ok := s.relPath(path)
	if !ok {
		return false
	}
	return s.dirs[rel]
}

// relPath normalizes path to the repo-relative slash form used as map key.
func (s *Snapshot) relPath(path string) (string, bool) {
	if path == "" {
		return "", true // the repo root itself
	}
	if filepath.IsAbs(path) {
		rel, ok := relInside(s.Root, path)
		if !ok {
			// macOS tempdirs reach git through /private symlinks: retry with
			// both sides resolved before declaring the path outside the repo.
			root, err1 := filepath.EvalSymlinks(s.Root)
			p, err2 := filepath.EvalSymlinks(path)
			if err1 != nil || err2 != nil {
				return "", false
			}
			if rel, ok = relInside(root, p); !ok {
				return "", false
			}
		}
		path = rel
	}
	path = filepath.ToSlash(path)
	if path == "." {
		path = ""
	}
	return path, true
}

// relInside computes path relative to root, rejecting paths outside it.
func relInside(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// SnapshotMsg carries a refreshed snapshot back into Update. A nil Snap means
// the workspace is not a git repository (or git is unavailable).
type SnapshotMsg struct {
	Snap *Snapshot
}

// Refresh returns a command that loads a fresh snapshot for the workspace at
// dir. It never fails loudly: any error (no repo, no git binary, timeout)
// resolves to SnapshotMsg{Snap: nil} so non-git workspaces stay quiet.
func Refresh(dir string) tea.Cmd {
	return func() tea.Msg {
		snap, err := Load(dir)
		if err != nil {
			return SnapshotMsg{}
		}
		return SnapshotMsg{Snap: snap}
	}
}

// Load synchronously builds a snapshot for the repository containing dir.
// Callers inside the UI must wrap it via Refresh; Load itself is exported for
// commands that already run inside a tea.Cmd.
func Load(dir string) (*Snapshot, error) {
	root, err := DetectRoot(dir)
	if err != nil {
		return nil, err
	}
	out, err := runGit(root, "status", "--porcelain=v2", "--branch", "-z")
	if err != nil {
		return nil, err
	}
	snap := parseStatus(out)
	snap.Root = root
	return snap, nil
}

// DetectRoot resolves the repository top-level directory containing dir.
func DetectRoot(dir string) (string, error) {
	out, err := runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// runGit executes one git command in dir with the package timeout.
func runGit(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, gitError(err, stderr.String())
	}
	return stdout.Bytes(), nil
}
