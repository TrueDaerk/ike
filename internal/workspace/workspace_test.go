package workspace

import (
	"testing"

	"ike/internal/pane"
)

func TestManagerHoldsActiveWorkspace(t *testing.T) {
	panes := pane.NewRegistry(nil)
	ws := New("/proj/a", panes)
	m := NewManager(ws)
	if m.Active() != ws {
		t.Fatal("Active must return the constructed workspace")
	}
	if m.Active().Panes != panes || m.Active().Root != "/proj/a" {
		t.Fatalf("workspace fields lost: %+v", m.Active())
	}
	other := New("/proj/b", pane.NewRegistry(nil))
	m.SetActive(other)
	if m.Active() != other {
		t.Fatal("SetActive must swap the active workspace")
	}
}

func TestWorkspaceStateIsSharedAcrossModelCopies(t *testing.T) {
	// The root model is copied by value on every bubbletea Update; the
	// workspace is the pointer seam that keeps panes/tree/focus one unit.
	m := NewManager(New("", pane.NewRegistry(nil)))
	copy := m // the Manager pointer itself is what Model embeds
	copy.Active().ReturnFocus = "editor:1"
	if m.Active().ReturnFocus != "editor:1" {
		t.Fatal("workspace state must be shared through the manager pointer")
	}
}
