package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/registry"
)

// complexity_histogram_test.go covers complexity.histogram (#2953): the
// command opens the panel with the distribution row, D in the complexity
// context flips it back (and saves the layout at once), and the state
// survives a layout save / restore.
func TestComplexityHistogram(t *testing.T) {
	m, _ := timeApp(t)
	m = dismissOnboarding(m)
	out, _ := m.Update(ComplexityHistogramMsg{})
	m = out.(Model)
	p := m.complexityPanel()
	if p == nil || !p.Histogram() {
		t.Fatalf("complexity.histogram: panel %v", p)
	}
	shiftD := tea.KeyPressMsg{Code: 'd', ShiftedCode: 'D', Text: "D", Mod: tea.ModShift}
	tm, cmd := m.Update(shiftD)
	m = drainCmd(asModel(tm), cmd)
	if m.complexityPanel().Histogram() {
		t.Fatal("D must hide the distribution row")
	}
	tm, cmd = m.Update(shiftD)
	m = drainCmd(asModel(tm), cmd)
	// The key goes through the command, which saves the layout itself.
	_, ids, ok := loadLayout()
	if want := "hist:on"; !ok || ids[pane.ComplexityKey].Mode != want {
		t.Fatalf("saved identity = %+v (ok %v), want mode %q", ids[pane.ComplexityKey], ok, want)
	}
	m2 := NewWith(registry.New(), host.MapConfig{})
	inst := m2.activeWS().Panes.Get(pane.ComplexityKey)
	if inst == nil || !inst.Complexity().Histogram() {
		t.Fatal("the distribution row did not restore")
	}
}
