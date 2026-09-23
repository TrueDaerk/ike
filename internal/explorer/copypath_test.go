package explorer

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestCopyPathDuplicatesFileWithUndo guards the core of file.copy (f5, #2696):
// the copy lands at the named destination, the source stays, and one undo
// trashes exactly the copy.
func TestCopyPathDuplicatesFileWithUndo(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	src := filepath.Join(root, "a.txt")
	dest := filepath.Join(root, "a-copy.txt")

	m, cmd := m.Update(CopyPathMsg{Path: src, Dest: dest})
	m, _ = pumpScans(m, cmd)
	if data, err := os.ReadFile(dest); err != nil || string(data) != "a" {
		t.Fatalf("copy = %q, %v; want the source's content", data, err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("the source must stay put: %v", err)
	}
	if got := m.cursorPath(); got != dest {
		t.Fatalf("cursor = %q, want the copy %q", got, dest)
	}

	m, cmd = m.Update(UndoMsg{})
	pumpScans(m, cmd)
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("undo must remove the copy")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("undo must leave the source alone: %v", err)
	}
}

// TestCopyPathAnnouncesCreation: the copy is announced with FileCreatedMsg so
// the app refreshes its VCS status snapshot.
func TestCopyPathAnnouncesCreation(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	dest := filepath.Join(root, "a-copy.txt")

	_, cmd := m.Update(CopyPathMsg{Path: filepath.Join(root, "a.txt"), Dest: dest})
	if !emitsCreated(cmd, dest) {
		t.Fatal("a copy must emit FileCreatedMsg for the new path")
	}
}

// emitsCreated reports whether cmd's message tree carries a FileCreatedMsg for
// path. Scan results are stepped over, not fed back — only the announcement
// matters here.
func emitsCreated(cmd tea.Cmd, path string) bool {
	pending := []tea.Cmd{cmd}
	for len(pending) > 0 {
		c := pending[0]
		pending = pending[1:]
		if c == nil {
			continue
		}
		msg := c()
		if b, ok := msg.(tea.BatchMsg); ok {
			pending = append(pending, b...)
			continue
		}
		if fc, ok := msg.(FileCreatedMsg); ok && fc.Path == path {
			return true
		}
	}
	return false
}

// TestCopyPathRecursesAndKeepsSymlinks: a directory copy walks the whole
// subtree and recreates symlinks as links rather than following them.
func TestCopyPathRecursesAndKeepsSymlinks(t *testing.T) {
	root := tree(t)
	nested := filepath.Join(root, "sub", "deep")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(nested, "d.txt"), "d")
	link := filepath.Join(root, "sub", "link.txt")
	if err := os.Symlink("c.txt", link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	m := mounted(t, root, 40, 20)
	dest := filepath.Join(root, "sub-copy")

	m, cmd := m.Update(CopyPathMsg{Path: filepath.Join(root, "sub"), Dest: dest})
	pumpScans(m, cmd)

	if data, err := os.ReadFile(filepath.Join(dest, "deep", "d.txt")); err != nil || string(data) != "d" {
		t.Fatalf("nested file = %q, %v; want the recursive copy", data, err)
	}
	info, err := os.Lstat(filepath.Join(dest, "link.txt"))
	if err != nil {
		t.Fatalf("the symlink must be copied: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("a symlink must be copied as a link, not followed")
	}
}

// TestCopyPathExistingDestNeedsOverwrite: without the app's guard answered an
// existing destination errors and keeps its content; with Overwrite it is
// replaced.
func TestCopyPathExistingDestNeedsOverwrite(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	src, dest := filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt")

	m, cmd := m.Update(CopyPathMsg{Path: src, Dest: dest})
	m, _ = pumpScans(m, cmd)
	if m.err == nil {
		t.Fatal("copying onto an existing path must error without Overwrite")
	}
	if data, _ := os.ReadFile(dest); string(data) != "b" {
		t.Fatalf("the existing file must be untouched, got %q", data)
	}

	m.err = nil
	m, cmd = m.Update(CopyPathMsg{Path: src, Dest: dest, Overwrite: true})
	m, _ = pumpScans(m, cmd)
	if m.err != nil {
		t.Fatalf("an overwriting copy must succeed: %v", m.err)
	}
	if data, _ := os.ReadFile(dest); string(data) != "a" {
		t.Fatalf("overwrite left %q, want the source's content", data)
	}
}

// TestCopyPathDirIntoItselfRefused: the destination may not live inside the
// directory being copied — the copy would consume its own source.
func TestCopyPathDirIntoItselfRefused(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	sub := filepath.Join(root, "sub")

	m, cmd := m.Update(CopyPathMsg{Path: sub, Dest: filepath.Join(sub, "sub-copy")})
	pumpScans(m, cmd)
	if m.err == nil {
		t.Fatal("copying a folder into itself must error")
	}
	if _, err := os.Stat(filepath.Join(sub, "sub-copy")); err == nil {
		t.Fatal("the refused copy must not exist")
	}
}

// TestCopyPathCreatesMissingParents: a destination under a directory that does
// not exist yet is created rather than refused — the prompt's "new path" hint
// is a promise.
func TestCopyPathCreatesMissingParents(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	dest := filepath.Join(root, "fresh", "a.txt")

	m, cmd := m.Update(CopyPathMsg{Path: filepath.Join(root, "a.txt"), Dest: dest})
	m, _ = pumpScans(m, cmd)
	if m.err != nil {
		t.Fatalf("a copy into a new directory must succeed: %v", m.err)
	}
	if data, _ := os.ReadFile(dest); string(data) != "a" {
		t.Fatalf("copy = %q, want the source's content", data)
	}
}
