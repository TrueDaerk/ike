package httppane

// copystates_test.go covers the copy-consistency audit of #2062 for the
// response pane: every state that captures keys ahead of the normal copy
// case must still let the copy chord through. The "/" prompt half of that
// rule is #2051's TestCopySelectionWhileSearching in selection_test.go.

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestCopyChordAfterPendingFold: "z" arms the fold sequence and consumes the
// next key, but the copy chord is no fold command — it must copy (the
// selection when there is one, the body otherwise) instead of being
// swallowed.
func TestCopyChordAfterPendingFold(t *testing.T) {
	m := selViewer(t)
	fixedClock(t)
	r := bodyRow(m, "alpha beta")
	press(m, r, 0)
	drag(m, r, 5)

	m.handleKey(keyPress("z"))
	cmd := m.handleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c after z must copy the selection")
	}
	msg, ok := cmd().(CopyMsg)
	if !ok {
		t.Fatalf("message type: %T", cmd())
	}
	if msg.Text != "alpha" || msg.What != "selection" {
		t.Errorf("copy message: %+v", msg)
	}
	if m.HasSelection() {
		t.Error("copying must clear the selection")
	}

	// The armed sequence is spent either way, so the next "y" is the plain
	// copy key again, not the fold copy.
	cmd = m.handleKey(keyPress("y"))
	if cmd == nil {
		t.Fatal("y must copy")
	}
	if msg := cmd().(CopyMsg); msg.What != "response body" {
		t.Errorf("y after a spent z: %+v", msg)
	}

	// Without a selection the reserved chord means what it means at rest.
	m.handleKey(keyPress("z"))
	cmd = m.handleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModSuper})
	if cmd == nil {
		t.Fatal("cmd+c after z must copy the body")
	}
	if msg := cmd().(CopyMsg); msg.What != "response body" {
		t.Errorf("cmd+c after z: %+v", msg)
	}
}

// TestPendingFoldKeepsFoldCopy: reserving the chord must not cost "zy" its
// meaning — the fold copy (#1787) still runs on the bare key, and the chord
// copies the whole body over the very same fold.
func TestPendingFoldKeepsFoldCopy(t *testing.T) {
	m := foldViewer(t)
	m.ScrollToRow(bodyRow(m, "  \"mapping\": {"))

	m.handleKey(keyPress("z"))
	cmd := m.handleKey(keyPress("y"))
	if cmd == nil {
		t.Fatal("zy must copy the target fold")
	}
	msg, ok := cmd().(CopyMsg)
	if !ok {
		t.Fatalf("message type: %T", cmd())
	}
	if msg.Text != mappingFold {
		t.Errorf("zy copied %q, want the fold %q", msg.Text, mappingFold)
	}

	m.handleKey(keyPress("z"))
	cmd = m.handleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModSuper})
	if cmd == nil {
		t.Fatal("cmd+c after z must copy")
	}
	if msg := cmd().(CopyMsg); msg.What != "response body" {
		t.Errorf("the chord is not the fold copy: %+v", msg)
	}
}

// TestPendingFoldStillCancelsOnOtherKeys: only the copy chord is reserved —
// any other key spends the sequence as before.
func TestPendingFoldStillCancelsOnOtherKeys(t *testing.T) {
	m := selViewer(t)
	m.handleKey(keyPress("z"))
	if cmd := m.handleKey(keyPress("q")); cmd != nil {
		t.Errorf("an unknown fold key must be inert, got %+v", cmd())
	}
	if m.pendingZ {
		t.Error("the sequence must be spent")
	}
}
