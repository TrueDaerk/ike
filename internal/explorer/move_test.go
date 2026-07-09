package explorer

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestMoveToMsgMovesFileWithUndoRedo guards the move operation (#175): the
// file lands in the target directory, and undo/redo walk it back and forth on
// the shared file-op stack.
func TestMoveToMsgMovesFileWithUndoRedo(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "sub", "a.txt")

	m, cmd := m.Update(MoveToMsg{Path: src, TargetDir: filepath.Join(root, "sub")})
	m, _ = pumpScans(m, cmd)
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("move failed: %v", err)
	}
	if _, err := os.Stat(src); err == nil {
		t.Fatal("source must be gone after the move")
	}

	m, cmd = m.Update(UndoMsg{})
	m, _ = pumpScans(m, cmd)
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("undo should move back: %v", err)
	}

	m, cmd = m.Update(RedoMsg{})
	pumpScans(m, cmd)
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("redo should move again: %v", err)
	}
}

// TestMoveDirIntoItselfRefused guards the self-nesting guard.
func TestMoveDirIntoItselfRefused(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	sub := filepath.Join(root, "sub")

	m, cmd := m.Update(MoveToMsg{Path: sub, TargetDir: sub})
	pumpScans(m, cmd)
	if m.err == nil {
		t.Fatal("moving a folder into itself must error")
	}
	if _, err := os.Stat(filepath.Join(sub, "c.txt")); err != nil {
		t.Fatalf("guarded move must leave the tree untouched: %v", err)
	}
}

// TestMoveToExistingTargetRefused: a name collision in the target directory
// must error instead of clobbering.
func TestMoveToExistingTargetRefused(t *testing.T) {
	root := tree(t)
	mustWrite(t, filepath.Join(root, "sub", "a.txt"), "existing")
	m := mounted(t, root, 40, 20)

	m, cmd := m.Update(MoveToMsg{Path: filepath.Join(root, "a.txt"), TargetDir: filepath.Join(root, "sub")})
	pumpScans(m, cmd)
	if m.err == nil {
		t.Fatal("moving onto an existing target must error")
	}
	data, _ := os.ReadFile(filepath.Join(root, "sub", "a.txt"))
	if string(data) != "existing" {
		t.Fatalf("existing target must be untouched, got %q", data)
	}
}

// TestRenameEmitsFileMovedMsg: rename (and its undo) must announce the path
// change so the app re-points editors instead of closing them (#175).
func TestRenameEmitsFileMovedMsg(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	src := filepath.Join(root, "a.txt")

	m, cmd := m.Update(RenamePathMsg{Path: src, Name: "z.txt"})
	moved := findMoved(t, m, cmd)
	if moved.Old != src || moved.New != filepath.Join(root, "z.txt") || moved.IsDir {
		t.Fatalf("unexpected FileMovedMsg: %+v", moved)
	}

	m, cmd = m.Update(UndoMsg{})
	moved = findMoved(t, m, cmd)
	if moved.Old != filepath.Join(root, "z.txt") || moved.New != src {
		t.Fatalf("undo must announce the reverse move: %+v", moved)
	}
}

// findMoved pumps cmd like pumpScans but captures the emitted FileMovedMsg.
func findMoved(t *testing.T, m Model, cmd tea.Cmd) FileMovedMsg {
	t.Helper()
	var moved *FileMovedMsg
	pending := []tea.Cmd{cmd}
	for len(pending) > 0 {
		c := pending[0]
		pending = pending[1:]
		if c == nil {
			continue
		}
		switch v := c().(type) {
		case tea.BatchMsg:
			pending = append(pending, v...)
		case ScanDoneMsg:
			var next tea.Cmd
			m, next = m.Update(v)
			pending = append(pending, next)
		case FileMovedMsg:
			mv := v
			moved = &mv
		}
	}
	if moved == nil {
		t.Fatal("no FileMovedMsg emitted")
	}
	return *moved
}
