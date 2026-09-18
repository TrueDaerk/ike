package diff

// copycmd_test.go covers CopyKeyCmd (#2628): the exported entry point the
// bound diff.copy command runs, which must do exactly what the pane-local
// copy key does — the selection when there is one, else the current hunk.

import "testing"

func TestCopyKeyCmdCopiesSelection(t *testing.T) {
	m := selModel(t)
	fixedClock(t)
	uniPress(m, 1, 0)
	uniDrag(m, 1, 5)
	cmd := m.CopyKeyCmd()
	if cmd == nil {
		t.Fatal("CopyKeyCmd with a selection must emit a copy command")
	}
	msg, ok := cmd().(CopyMsg)
	if !ok {
		t.Fatalf("message type: %T", cmd())
	}
	if msg.Text != "gamma" || msg.What != "selection" {
		t.Errorf("copy message: %+v", msg)
	}
	if m.HasSelection() {
		t.Error("copying must clear the selection")
	}
}

func TestCopyKeyCmdWithoutSelectionCopiesHunk(t *testing.T) {
	m := selModel(t)
	cmd := m.CopyKeyCmd()
	if cmd == nil {
		t.Fatal("CopyKeyCmd without a selection must copy the current hunk")
	}
	msg, ok := cmd().(CopyMsg)
	if !ok {
		t.Fatalf("message type: %T", cmd())
	}
	if msg.What != "hunk" || msg.Text == "" {
		t.Errorf("copy message: %+v", msg)
	}
}
