package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
)

// complexity_bulk_test.go covers the app side of the Complexity window's
// bulk keys (#3082): shift+space and complexity.sitesAll, B and the
// grouping picker, I and complexity.baselineMarked behind its dialog.

func TestComplexitySitesAllCommandAndKey(t *testing.T) {
	m, _ := timeApp(t)
	m = dismissOnboarding(m)
	out, _ := m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	p := m.complexityPanel()
	site := []complexity.Site{{Line: 2, Col: 1, EndLine: 4, Kind: "if", CCN: 1, Cognitive: 1}}
	a := complexity.Function{Name: "a", Kind: complexity.KindFunction, Line: 0, EndLine: 9, NLOC: 10, CCN: 3, Cognitive: 4, Sites: site}
	b := complexity.Function{Name: "b", Kind: complexity.KindFunction, Line: 20, EndLine: 29, NLOC: 10, CCN: 2, Cognitive: 1, Sites: site}
	p.Set(complexity.View{Gen: 1, Files: map[string][]complexity.Function{"/proj/a.go": {a, b}}}, complexity.Stats{Enabled: true})
	out, _ = m.Update(ComplexitySitesAllMsg{})
	m = out.(Model)
	if n := len(m.complexityPanel().Rows()); n != 4 {
		t.Fatalf("complexity.sitesAll: rows %d, want 4", n)
	}
	// shift+space resolves through the complexity context and collapses.
	tm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModShift})
	m = drainCmd(asModel(tm), cmd)
	if n := len(m.complexityPanel().Rows()); n != 2 {
		t.Fatalf("shift+space: rows %d, want 2", n)
	}
	// A grouped tab has nothing to expand; the command says so.
	m.complexityPanel().CycleGroup()
	out, _ = m.Update(ComplexitySitesAllMsg{})
	m = out.(Model)
	if !hasToast(m, "no visible function has sites") {
		t.Fatalf("toasts = %v", toastTexts(m))
	}
}

func TestComplexityGroupPickerCommandAndKey(t *testing.T) {
	m, _ := timeApp(t)
	m = dismissOnboarding(m)
	out, _ := m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	// B opens the locked picker listing the Project tab's modes.
	tm, cmd := m.Update(tea.KeyPressMsg{Code: 'b', ShiftedCode: 'B', Text: "B", Mod: tea.ModShift})
	m = drainCmd(asModel(tm), cmd)
	if !m.palette.IsOpen() {
		t.Fatal("B must open the grouping picker")
	}
	var titles []string
	for _, it := range m.cxGroupPick.Results("", m.paletteContext()) {
		titles = append(titles, it.Title+it.Badge)
	}
	if got := strings.Join(titles, ","); got != "none●,class,file,tree view" {
		t.Fatalf("picker rows = %s", got)
	}
	// Typing narrows, enter applies the pick.
	for _, r := range "file" {
		m = cxGroupType(m, r)
	}
	tm, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainCmd(asModel(tm), cmd)
	if m.palette.IsOpen() || m.complexityPanel().Grouping() != complexity.ByFile {
		t.Fatalf("pick file: open %v grouping %v", m.palette.IsOpen(), m.complexityPanel().Grouping())
	}
	// The picked message applies the tree view as well.
	out, _ = m.Update(ComplexityGroupPickedMsg{Choice: complexitypanel.GroupChoice{Tree: true}})
	m = out.(Model)
	if !m.complexityPanel().TreeView() {
		t.Fatal("the tree choice must turn the tree view on")
	}
	// A tab that does not group says so instead of opening the picker.
	m.complexityPanel().SetTab(complexitypanel.TabTrend)
	out, _ = m.Update(ComplexityGroupPickMsg{})
	m = out.(Model)
	if m.palette.IsOpen() || !hasToast(m, "does not group") {
		t.Fatalf("Trend tab: open %v toasts %v", m.palette.IsOpen(), toastTexts(m))
	}
}

func TestComplexityBaselineMarkedDialog(t *testing.T) {
	m, proj := baselineApp(t)
	path := complexity.BaselinePath(proj)
	m = baselineGen(t, m)
	out, _ := m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	// I asks first, naming the count; esc writes nothing.
	tm, cmd := m.Update(tea.KeyPressMsg{Code: 'i', ShiftedCode: 'I', Text: "I", Mod: tea.ModShift})
	m = drainCmd(asModel(tm), cmd)
	if !m.complexityBaselineDialogOpen() {
		t.Fatal("I must open the confirmation dialog")
	}
	if v := plainView(m); !strings.Contains(v, "Record 1 function the list shows") || !strings.Contains(v, "accept 1") {
		t.Fatalf("dialog body:\n%s", v)
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if b, _ := complexity.LoadBaseline(path); b != nil && b.Len() != 0 {
		t.Fatalf("esc must not write: %+v", b)
	}
	// enter writes every listed function in one save.
	out, _ = m.Update(ComplexityBaselineMarkedMsg{})
	m = out.(Model)
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	b, err := complexity.LoadBaseline(path)
	if err != nil || b == nil || b.Len() != 1 || b.Accepted[0].Function != "A" {
		t.Fatalf("saved baseline = %+v, %v", b, err)
	}
	if m.complexityBaselineDialogOpen() || m.cxBaseMarked != nil {
		t.Fatal("enter must close the dialog and drop the pending toggle")
	}
	// All listed accepted: the same command drops them.
	out, _ = m.Update(ComplexityBaselineMarkedMsg{})
	m = out.(Model)
	if v := plainView(m); !strings.Contains(v, "Drop 1 function") {
		t.Fatalf("drop dialog:\n%s", v)
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m = out.(Model)
	if b, _ := complexity.LoadBaseline(path); b == nil || b.Len() != 0 {
		t.Fatalf("the drop must empty the baseline: %+v", b)
	}
	// A filter listing nothing over threshold has nothing to ask.
	m.complexityPanel().SetTab(complexitypanel.TabChanges)
	out, _ = m.Update(ComplexityBaselineMarkedMsg{})
	m = out.(Model)
	if m.complexityBaselineDialogOpen() || !strings.Contains(cxNotices(m), "no listed function") {
		t.Fatalf("Changes tab: %q", cxNotices(m))
	}
}
