package explorer

import (
	"path/filepath"
	"testing"

	"ike/internal/vcs"
)

// TestNodeVCSStatus covers the status resolution behind the tree coloring
// (Roadmap 0320, #463): files take their snapshot status, dirty directories
// read as modified, and a nil snapshot (not a git repo) stays plain.
func TestNodeVCSStatus(t *testing.T) {
	dir := t.TempDir()
	m := New(dir)

	file := &node{name: "a.go", path: filepath.Join(dir, "sub", "a.go")}
	sub := &node{name: "sub", path: filepath.Join(dir, "sub"), isDir: true}
	clean := &node{name: "b.go", path: filepath.Join(dir, "b.go")}
	cleanDir := &node{name: "docs", path: filepath.Join(dir, "docs"), isDir: true}

	if got := m.nodeVCSStatus(file); got != vcs.StatusNone {
		t.Fatalf("nil snapshot: status = %v, want none", got)
	}

	m.SetVCS(vcs.NewSnapshot(dir, map[string]vcs.FileStatus{
		"sub/a.go": vcs.StatusModified,
	}))
	if got := m.nodeVCSStatus(file); got != vcs.StatusModified {
		t.Errorf("modified file = %v", got)
	}
	if got := m.nodeVCSStatus(sub); got != vcs.StatusModified {
		t.Errorf("dirty dir = %v, want modified tint", got)
	}
	if got := m.nodeVCSStatus(clean); got != vcs.StatusNone {
		t.Errorf("clean file = %v", got)
	}
	if got := m.nodeVCSStatus(cleanDir); got != vcs.StatusNone {
		t.Errorf("clean dir = %v", got)
	}

	m.SetVCS(nil)
	if got := m.nodeVCSStatus(file); got != vcs.StatusNone {
		t.Errorf("after SetVCS(nil): %v", got)
	}
}
