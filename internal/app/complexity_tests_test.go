package app

import (
	"testing"

	"ike/internal/complexity"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/registry"
)

// complexity_tests_test.go covers complexity.toggleTests (#2950): the
// command opens the panel and hides the tests, t in the complexity context
// flips them back, and the state survives a layout save / restore beside
// the grouping.
func TestComplexityToggleTests(t *testing.T) {
	m, _ := timeApp(t)
	m = dismissOnboarding(m)
	out, _ := m.Update(ComplexityToggleTestsMsg{})
	m = out.(Model)
	p := m.complexityPanel()
	if p == nil || !p.HideTests() {
		t.Fatalf("complexity.toggleTests: panel %v", p)
	}
	m = cxGroupType(m, 't')
	if m.complexityPanel().HideTests() {
		t.Fatal("t must show the tests again")
	}
	m = cxGroupType(m, 't')
	m = cxGroupType(m, 'b')
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	_, ids, ok := loadLayout()
	if want := "project:class,tests:hidden"; !ok || ids[pane.ComplexityKey].Mode != want {
		t.Fatalf("saved identity = %+v (ok %v), want mode %q", ids[pane.ComplexityKey], ok, want)
	}
	m2 := NewWith(registry.New(), host.MapConfig{})
	inst := m2.activeWS().Panes.Get(pane.ComplexityKey)
	if inst == nil || !inst.Complexity().HideTests() || inst.Complexity().Grouping() != complexity.ByClass {
		t.Fatal("hide-tests did not restore")
	}
}
