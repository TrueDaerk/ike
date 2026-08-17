package manager

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// TestManagerWillRenameFiles fans the pending rename out to the server that
// registered a matching filter and converts the returned WorkspaceEdit into
// editor-coordinate FileEdits (#1912).
func TestManagerWillRenameFiles(t *testing.T) {
	renames := make(chan protocol.RenameFilesParams, 4)
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{
		syncKind:          protocol.SyncFull,
		willRenameFilters: []protocol.FileOperationFilter{{Scheme: "file", Pattern: protocol.FileOperationPattern{Glob: "**/*.go"}}},
		willRenames:       renames,
	}), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(dir, "renamed.go")
	edits, err := m.WillRenameFiles(context.Background(), path, newPath)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-renames:
		if len(p.Files) != 1 || p.Files[0].OldURI != protocol.PathToURI(path) || p.Files[0].NewURI != protocol.PathToURI(newPath) {
			t.Fatalf("request files = %+v", p.Files)
		}
	default:
		t.Fatal("no workspace/willRenameFiles request reached the server")
	}
	if len(edits) != 1 || edits[0].Path != path || !edits[0].Open {
		t.Fatalf("edits = %+v, want one open-file entry for %s", edits, path)
	}
	fe := edits[0].Edits[0]
	if fe.Text != "moved" || fe.StartCol != 0 || fe.EndCol != 3 {
		t.Fatalf("edit = %+v, want first 3 columns replaced by 'moved'", fe)
	}
}

// TestManagerWillRenameFilesFilterMismatch skips a server whose filters do not
// cover the renamed path: no request, no edits.
func TestManagerWillRenameFilesFilterMismatch(t *testing.T) {
	renames := make(chan protocol.RenameFilesParams, 4)
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{
		syncKind:          protocol.SyncFull,
		willRenameFilters: []protocol.FileOperationFilter{{Pattern: protocol.FileOperationPattern{Glob: "**/*.py"}}},
		willRenames:       renames,
	}), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	edits, err := m.WillRenameFiles(context.Background(), path, filepath.Join(dir, "renamed.go"))
	if err != nil || len(edits) != 0 {
		t.Fatalf("mismatched filter should be a no-op, got %+v, %v", edits, err)
	}
	if len(renames) != 0 {
		t.Fatal("no request may reach a server whose filters do not match")
	}
}

// TestManagerWillRenameFilesUnregistered skips a server that never registered
// fileOperations.willRename at all.
func TestManagerWillRenameFilesUnregistered(t *testing.T) {
	renames := make(chan protocol.RenameFilesParams, 4)
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, willRenames: renames}), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	edits, err := m.WillRenameFiles(context.Background(), path, filepath.Join(dir, "renamed.go"))
	if err != nil || len(edits) != 0 || len(renames) != 0 {
		t.Fatalf("unregistered server should be skipped, got %+v, %v", edits, err)
	}
}

// TestMatchesFileOperation covers the filter dimensions: scheme, the
// file/folder matches kind, absolute and root-relative globs, and brace
// alternation via the shared pathglob matcher (#1912).
func TestMatchesFileOperation(t *testing.T) {
	root := "/ws"
	cases := []struct {
		name   string
		filter protocol.FileOperationFilter
		path   string
		isDir  bool
		want   bool
	}{
		{"anchored glob matches under root", protocol.FileOperationFilter{Pattern: protocol.FileOperationPattern{Glob: "**/*.go"}}, "/ws/pkg/a.go", false, true},
		{"suffix mismatch", protocol.FileOperationFilter{Pattern: protocol.FileOperationPattern{Glob: "**/*.go"}}, "/ws/pkg/a.py", false, false},
		{"root-relative glob", protocol.FileOperationFilter{Pattern: protocol.FileOperationPattern{Glob: "src/*.go"}}, "/ws/src/a.go", false, true},
		{"root-relative glob outside root", protocol.FileOperationFilter{Pattern: protocol.FileOperationPattern{Glob: "src/*.go"}}, "/elsewhere/src/a.go", false, false},
		{"scheme file accepted", protocol.FileOperationFilter{Scheme: "file", Pattern: protocol.FileOperationPattern{Glob: "**"}}, "/ws/a.go", false, true},
		{"scheme untitled rejected", protocol.FileOperationFilter{Scheme: "untitled", Pattern: protocol.FileOperationPattern{Glob: "**"}}, "/ws/a.go", false, false},
		{"matches file rejects folder", protocol.FileOperationFilter{Pattern: protocol.FileOperationPattern{Glob: "**", Matches: "file"}}, "/ws/pkg", true, false},
		{"matches folder accepts folder", protocol.FileOperationFilter{Pattern: protocol.FileOperationPattern{Glob: "**", Matches: "folder"}}, "/ws/pkg", true, true},
		{"matches folder rejects file", protocol.FileOperationFilter{Pattern: protocol.FileOperationPattern{Glob: "**", Matches: "folder"}}, "/ws/a.go", false, false},
		{"brace alternation", protocol.FileOperationFilter{Pattern: protocol.FileOperationPattern{Glob: "**/*.{go,mod}"}}, "/ws/go.mod", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesFileOperation(tc.filter, tc.path, root, tc.isDir); got != tc.want {
				t.Fatalf("matchesFileOperation = %v, want %v", got, tc.want)
			}
		})
	}
}
