package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
)

// complexity_sites_test.go covers complexity.sites (#2946) on the app side:
// the command expands the selected function of the Complexity window into
// its sites, space in the complexity context toggles the same, and a
// selection with nothing to expand says so.
func TestComplexitySitesCommandAndKey(t *testing.T) {
	m, _ := timeApp(t)
	m = dismissOnboarding(m)
	out, _ := m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	p := m.complexityPanel()
	if p == nil {
		t.Fatal("no panel")
	}
	withSites := complexity.Function{Name: "render", Kind: complexity.KindFunction, Line: 0, EndLine: 9, NLOC: 10, CCN: 3, Cognitive: 4,
		Sites: []complexity.Site{
			{Line: 2, Col: 1, EndLine: 4, Kind: "if", CCN: 1, Cognitive: 1},
			{Line: 3, Col: 2, EndLine: 3, Kind: "if", CCN: 1, Cognitive: 2, Nesting: 1, Level: 1},
			{Line: 6, Col: 4, EndLine: 6, Kind: "recursion", Cognitive: 1},
		}}
	plain := complexity.Function{Name: "tiny", Kind: complexity.KindFunction, Line: 20, EndLine: 21, NLOC: 2, CCN: 1}
	p.Set(complexity.View{Gen: 1, Files: map[string][]complexity.Function{"/proj/a.go": {withSites, plain}}}, complexity.Stats{Enabled: true})
	// The command expands the selected row (render, CCN descending).
	out, _ = m.Update(ComplexitySitesMsg{})
	m = out.(Model)
	p = m.complexityPanel()
	if r, ok := p.Expanded(); !ok || r.Fn.Name != "render" || len(p.Rows()) != 5 {
		t.Fatalf("complexity.sites: expanded %+v %v, rows %d", r, ok, len(p.Rows()))
	}
	// space resolves through the complexity context and collapses again.
	tm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	m = drainCmd(asModel(tm), cmd)
	p = m.complexityPanel()
	if _, ok := p.Expanded(); ok || len(p.Rows()) != 2 {
		t.Fatalf("space: still expanded, rows %d", len(p.Rows()))
	}
	// A function without sites has nothing to expand; the command says so.
	m = cxGroupType(m, 'G')
	out, _ = m.Update(ComplexitySitesMsg{})
	m = out.(Model)
	if !hasToast(m, "no sites") {
		t.Fatalf("toasts = %v", toastTexts(m))
	}
}
