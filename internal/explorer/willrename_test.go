package explorer

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/lsp"
)

// TestRenameDefersBehindWillRenameProvider guards the willRenameFiles hook
// (#1912): with a provider registered, the FS rename must not happen until the
// WillRenameDoneMsg arrives; committing then performs the rename, records it
// on the undo stack and announces the move.
func TestRenameDefersBehindWillRenameProvider(t *testing.T) {
	var got lsp.WillRenameRequest
	lsp.SetWillRename(func(req lsp.WillRenameRequest) tea.Cmd {
		got = req
		return func() tea.Msg {
			return lsp.WillRenameDoneMsg{Old: req.Old, New: req.New, IsDir: req.IsDir}
		}
	})
	defer lsp.SetWillRename(nil)

	root := tree(t)
	m := mounted(t, root, 40, 20)
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "z.txt")

	m, cmd := m.Update(RenamePathMsg{Path: src, Name: "z.txt"})
	if cmd == nil {
		t.Fatal("expected the provider command, got nil")
	}
	if got.Old != src || got.New != dst || got.IsDir {
		t.Fatalf("unexpected WillRenameRequest: %+v", got)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("rename must be deferred while the provider runs: %v", err)
	}

	done, ok := cmd().(lsp.WillRenameDoneMsg)
	if !ok {
		t.Fatalf("provider command must yield the done msg, got %T", cmd())
	}
	m, cmd = m.Update(done)
	moved := findMoved(t, m, cmd)
	if moved.Old != src || moved.New != dst {
		t.Fatalf("unexpected FileMovedMsg: %+v", moved)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("commit must perform the rename: %v", err)
	}
	if _, err := os.Stat(src); err == nil {
		t.Fatal("source must be gone after the commit")
	}

	// The deferred rename stays a regular undoable file op.
	m, cmd = m.Update(UndoMsg{})
	findMoved(t, m, cmd)
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("undo should rename back: %v", err)
	}
}

// TestCommitRelocateRefusesFreshTarget guards the re-run existing-target
// check: a file appearing at the target during the server round trip must
// fail the commit instead of being overwritten.
func TestCommitRelocateRefusesFreshTarget(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "z.txt")
	mustWrite(t, dst, "already here")

	if cmd := m.commitRelocate(src, dst, false); cmd != nil {
		t.Fatal("commit on an existing target must refuse")
	}
	if m.err == nil {
		t.Fatal("expected the already-exists error")
	}
	b, err := os.ReadFile(dst)
	if err != nil || string(b) != "already here" {
		t.Fatalf("target must be untouched: %q %v", b, err)
	}
}
