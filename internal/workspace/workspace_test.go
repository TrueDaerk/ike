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

func TestParkResumeRoundTrip(t *testing.T) {
	a := New("/proj/a", pane.NewRegistry(nil))
	b := New("/proj/b", pane.NewRegistry(nil))
	m := NewManager(a)
	m.Park()
	if m.Active() != nil {
		t.Fatal("Park must clear the active slot")
	}
	m.SetActive(b)
	if got := m.Background(); len(got) != 1 || got[0] != "/proj/a" {
		t.Fatalf("background = %v, want [/proj/a]", got)
	}
	if m.Peek("/proj/a") != a {
		t.Fatal("Peek must see the parked workspace")
	}
	if w := m.Resume("/proj/x"); w != nil {
		t.Fatal("Resume of an unknown root must return nil")
	}
	if w := m.Resume("/proj/a"); w != a || m.Active() != a {
		t.Fatal("Resume must pop the parked workspace and activate it")
	}
	if len(m.Background()) != 0 {
		t.Fatal("resumed root must leave the background set")
	}
}

func TestParkWithoutRootDrops(t *testing.T) {
	m := NewManager(New("", pane.NewRegistry(nil)))
	m.Park()
	if len(m.Background()) != 0 {
		t.Fatal("a rootless workspace cannot be parked")
	}
}

func TestBackgroundLRUOrderAndDrop(t *testing.T) {
	m := NewManager(New("/a", pane.NewRegistry(nil)))
	m.Park()
	m.SetActive(New("/b", pane.NewRegistry(nil)))
	m.Park()
	m.SetActive(New("/a", pane.NewRegistry(nil))) // fresh /a again
	m.Park()
	if got := m.Background(); len(got) != 2 || got[0] != "/b" || got[1] != "/a" {
		t.Fatalf("LRU order = %v, want [/b /a]", got)
	}
	if w := m.Drop("/b"); w == nil || m.Peek("/b") != nil {
		t.Fatal("Drop must remove and return the parked workspace")
	}
	if w := m.Drop("/b"); w != nil {
		t.Fatal("double Drop must return nil")
	}
}
