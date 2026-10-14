package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/registry"
)

// complexity_view_test.go covers complexity.view (#2954): the command opens
// the panel with the Project tab's tree view, v in the complexity context
// flips it back (and saves the layout at once), and the state survives a
// layout save / restore.
func TestComplexityTreeView(t *testing.T) {
	m, _ := timeApp(t)
	m = dismissOnboarding(m)
	out, _ := m.Update(ComplexityViewMsg{})
	m = out.(Model)
	p := m.complexityPanel()
	if p == nil || !p.TreeView() {
		t.Fatalf("complexity.view: panel %v", p)
	}
	v := tea.KeyPressMsg{Code: 'v', Text: "v"}
	tm, cmd := m.Update(v)
	m = drainCmd(asModel(tm), cmd)
	if m.complexityPanel().TreeView() {
		t.Fatal("v must return to the grid")
	}
	tm, cmd = m.Update(v)
	m = drainCmd(asModel(tm), cmd)
	// The key goes through the command, which saves the layout itself.
	_, ids, ok := loadLayout()
	if want := "view:tree"; !ok || ids[pane.ComplexityKey].Mode != want {
		t.Fatalf("saved identity = %+v (ok %v), want mode %q", ids[pane.ComplexityKey], ok, want)
	}
	m2 := NewWith(registry.New(), host.MapConfig{})
	inst := m2.activeWS().Panes.Get(pane.ComplexityKey)
	if inst == nil || !inst.Complexity().TreeView() {
		t.Fatal("the tree view did not restore")
	}
}
