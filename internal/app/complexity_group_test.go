package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/registry"
)

// complexity_group_test.go covers the Complexity window's grouping on the
// app side (#2937): the complexity.group command, its b keybind in the
// complexity context, a b typed into the filter row staying text, and the
// grouping surviving a layout save / restore.

// cxGroupType types one printable key the way a terminal delivers it.
func cxGroupType(m Model, r rune) Model {
	tm, cmd := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	return drainCmd(asModel(tm), cmd)
}

func TestComplexityGroupCommandAndKey(t *testing.T) {
	m, _ := timeApp(t)
	m = dismissOnboarding(m)
	// The command opens a closed panel and cycles its Project tab.
	out, _ := m.Update(ComplexityGroupMsg{})
	m = out.(Model)
	p := m.complexityPanel()
	if p == nil || p.Grouping() != complexity.ByClass {
		t.Fatalf("complexity.group: panel %v", p)
	}
	if m.activeWS().Panes.Focused() != pane.ComplexityKey {
		t.Fatalf("focus = %q, want the panel", m.activeWS().Panes.Focused())
	}
	// b resolves through the complexity context to the same command.
	m = cxGroupType(m, 'b')
	if g := m.complexityPanel().Grouping(); g != complexity.ByFile {
		t.Fatalf("b: grouping = %v, want file", g)
	}
	// A b typed into the open filter row is text, not the command.
	m = cxGroupType(m, '/')
	if !m.complexityPanel().Filtering() {
		t.Fatal("/ must focus the filter row")
	}
	m = cxGroupType(m, 'b')
	if g := m.complexityPanel().Grouping(); g != complexity.ByFile {
		t.Fatalf("b in the filter row changed the grouping to %v", g)
	}
	// The Changes tab groups per file (#2955).
	m.complexityPanel().SetTab(complexitypanel.TabChanges)
	out, _ = m.Update(ComplexityGroupMsg{})
	m = out.(Model)
	if g := m.complexityPanel().Grouping(); g != complexity.ByFile {
		t.Fatalf("Changes tab: grouping = %v, want file", g)
	}
	if hasToast(m, "does not group") {
		t.Fatalf("toasts = %v", toastTexts(m))
	}
}

func TestComplexityGroupPersists(t *testing.T) {
	m, _ := timeApp(t)
	out, _ := m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	for i := 0; i < 2; i++ { // none → class → file
		out, _ = m.Update(ComplexityGroupMsg{})
		m = out.(Model)
	}
	p := m.complexityPanel()
	p.SetTab(complexitypanel.TabFile)
	p.CycleGroup() // File tab: class
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	_, ids, ok := loadLayout()
	if want := "project:file,file:class"; !ok || ids[pane.ComplexityKey].Mode != want {
		t.Fatalf("saved identity = %+v (ok %v), want mode %q", ids[pane.ComplexityKey], ok, want)
	}
	// A hosted tab carries the mode in its content identity too.
	if id, _ := contentIdentity(m.activeWS().Panes.Get(pane.ComplexityKey)); id.Mode != "project:file,file:class" {
		t.Errorf("content identity mode = %q", id.Mode)
	}

	m2 := NewWith(registry.New(), host.MapConfig{})
	inst := m2.activeWS().Panes.Get(pane.ComplexityKey)
	if inst == nil || inst.Kind() != pane.KindComplexity {
		t.Fatal("panel did not restore")
	}
	r := inst.Complexity()
	if r.Grouping() != complexity.ByFile || r.GroupMode() != "project:file,file:class" {
		t.Fatalf("restored grouping %v mode %q", r.Grouping(), r.GroupMode())
	}
	// A named layout snapshot keeps it as well.
	st := &snapState{reg: m.activeWS().Panes}
	if _, id, ok := st.leafIdentity(pane.ComplexityKey); !ok || id.Mode != "project:file,file:class" {
		t.Errorf("named-layout identity = %+v", id)
	}
}
