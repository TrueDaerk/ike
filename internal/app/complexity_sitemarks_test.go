package app

// complexity_sitemarks_test.go covers the per-site complexity marks feed
// (#2947): the mark builder per editor.marks.complexity_sites mode, the
// wiring through the settled pass, the panel's one-shot `m` in off mode,
// and one measurement per key feeding both the hints and the marks.

import (
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/config"
	"ike/internal/editor"
)

// cxSiteFixture is one function under the 15/15 thresholds and one over.
func cxSiteFixture() []complexity.Function {
	return []complexity.Function{
		{Name: "under", Line: 0, CCN: 2, Cognitive: 1, Sites: []complexity.Site{
			{Line: 1, Col: 1, Kind: "if", CCN: 1, Cognitive: 1},
		}},
		{Name: "over", Line: 10, CCN: 20, Cognitive: 16, Sites: []complexity.Site{
			{Line: 11, Col: 1, Kind: "if", CCN: 1, Cognitive: 1},
			{Line: 11, Col: 6, Kind: "||", CCN: 1, Cognitive: 1},
			{Line: 12, Col: 2, Kind: "for", CCN: 1, Cognitive: 2, Nesting: 1, Level: 1},
			{Line: 13, Col: 3, Kind: "if", CCN: 1, Cognitive: 3, Nesting: 2, Level: 2},
			{Line: 14, Col: 3, Kind: "case", CCN: 1},
		}},
	}
}

func cxMarkLines(marks []editor.ComplexitySiteMark) map[int]editor.ComplexitySiteMark {
	out := map[int]editor.ComplexitySiteMark{}
	for _, mk := range marks {
		out[mk.Line] = mk
	}
	return out
}

// TestComplexitySiteMarksForOff: off paints nothing.
func TestComplexitySiteMarksForOff(t *testing.T) {
	if got := complexitySiteMarksFor(cxSiteFixture(), 15, 15, config.HintOff, -1); len(got) != 0 {
		t.Fatalf("off: got %+v, want none", got)
	}
}

// TestComplexitySiteMarksForWarn: warn paints only the function over a
// threshold, with the Sonar texts, joined per line, warn from +3, and no
// mark for a CCN-only arm.
func TestComplexitySiteMarksForWarn(t *testing.T) {
	got := cxMarkLines(complexitySiteMarksFor(cxSiteFixture(), 15, 15, config.HintWarn, -1))
	if _, ok := got[1]; ok {
		t.Error("warn: the under-threshold function must carry no marks")
	}
	want := map[int]editor.ComplexitySiteMark{
		11: {Line: 11, Text: "+1 · +1 ||"},
		12: {Line: 12, Text: "+2 (nesting 1)"},
		13: {Line: 13, Text: "+3 (nesting 2)", Warn: true},
	}
	if len(got) != len(want) {
		t.Fatalf("warn: got %+v, want %+v", got, want)
	}
	for line, w := range want {
		if got[line] != w {
			t.Errorf("line %d: got %+v, want %+v", line, got[line], w)
		}
	}
}

// TestComplexitySiteMarksForAll: all paints every function; a site the
// encloser shares with a nested unit is marked once.
func TestComplexitySiteMarksForAll(t *testing.T) {
	fns := cxSiteFixture()
	fns = append(fns, complexity.Function{Name: "nested", Line: 12, Sites: []complexity.Site{
		{Line: 13, Col: 3, Kind: "if", CCN: 1, Cognitive: 1},
	}})
	got := cxMarkLines(complexitySiteMarksFor(fns, 15, 15, config.HintAll, -1))
	if got[1].Text != "+1" || got[11].Text == "" {
		t.Fatalf("all: got %+v, want both functions marked", got)
	}
	if got[13].Text != "+3 (nesting 2)" {
		t.Errorf("shared site = %q, want the encloser's copy once", got[13].Text)
	}
}

// TestComplexitySiteMarksForShow: a panel `m` paints its function in off
// mode and only that one.
func TestComplexitySiteMarksForShow(t *testing.T) {
	got := cxMarkLines(complexitySiteMarksFor(cxSiteFixture(), 15, 15, config.HintOff, 0))
	if len(got) != 1 || got[1].Text != "+1" {
		t.Fatalf("show: got %+v, want only the shown function's site", got)
	}
}

// cxSitesConfig applies a marks configuration and settles the reload.
func cxSitesConfig(t *testing.T, m Model, hints, sites config.HintMode) Model {
	t.Helper()
	c, _ := config.Load(config.Options{})
	c.Editor.Marks.Complexity, c.Editor.Marks.ComplexitySites = hints, sites
	config.Set(c)
	out, _ := m.Update(config.ConfigReloadedMsg{Config: c})
	return out.(Model)
}

// cxSitedFunction is an indexed function of the source with cognitive
// sites, for the wiring tests.
func cxSitedFunction(t *testing.T, m Model, src string) complexity.Function {
	t.Helper()
	for _, fn := range m.ComplexityIndex().Snapshot().Files[canonicalPath(src)] {
		for _, s := range fn.Sites {
			if s.Cognitive > 0 && s.Line != fn.Line {
				return fn
			}
		}
	}
	t.Fatal("the source holds no function with cognitive sites")
	return complexity.Function{}
}

func cxOpenSource(t *testing.T) (Model, string) {
	t.Helper()
	m, src := complexityApp(t)
	t.Cleanup(func() { c, _ := config.Load(config.Options{}); config.Set(c) })
	out, cmd := m.openPathAt(src, 0, 0)
	m = settleHints(t, out.(Model), cmd)
	return m, src
}

// TestComplexitySiteMarksModeWiring: the site mode is part of the key —
// all marks every function's sites, off clears them, and the hint mode
// off leaves them alone (independent toggles).
func TestComplexitySiteMarksModeWiring(t *testing.T) {
	m, src := cxOpenSource(t)
	fn := cxSitedFunction(t, m, src)
	m = cxSitesConfig(t, m, config.HintOff, config.HintAll)
	ed := m.editorForPath(src)
	if ed.HasComplexityHints() {
		t.Error("editor.marks.complexity=off must clear the header hints")
	}
	if !ed.HasComplexitySiteMarks() {
		t.Fatal("complexity_sites=all must mark the indexed buffer although the hints are off")
	}
	for _, s := range fn.Sites {
		if s.Cognitive > 0 {
			if _, ok := ed.ComplexitySiteMarkAt(s.Line); !ok {
				t.Errorf("site %s at line %d carries no mark", s.Kind, s.Line)
			}
		}
	}
	m = cxSitesConfig(t, m, config.HintAll, config.HintOff)
	if ed = m.editorForPath(src); ed.HasComplexitySiteMarks() || !ed.HasComplexityHints() {
		t.Fatalf("sites off: marks=%v hints=%v, want hints only", ed.HasComplexitySiteMarks(), ed.HasComplexityHints())
	}
	// warn with thresholds nothing reaches: no marks.
	c, _ := config.Load(config.Options{})
	c.Editor.Marks.ComplexitySites = config.HintWarn
	c.Complexity.CCNWarn, c.Complexity.CognitiveWarn = 200, 200
	config.Set(c)
	out, _ := m.Update(config.ConfigReloadedMsg{Config: c})
	m = out.(Model)
	if m.editorForPath(src).HasComplexitySiteMarks() {
		t.Fatal("warn: no function reaches 200, so no marks")
	}
}

// TestComplexitySiteMarksShowOnce: the panel's `m` paints the function's
// sites with both modes off; an edit drops them.
func TestComplexitySiteMarksShowOnce(t *testing.T) {
	m, src := cxOpenSource(t)
	fn := cxSitedFunction(t, m, src)
	m = cxSitesConfig(t, m, config.HintOff, config.HintOff)
	if m.editorForPath(src).HasComplexitySiteMarks() {
		t.Fatal("off: no marks before the request")
	}
	out, cmd := m.Update(complexitypanel.ShowSitesMsg{Path: src, Line: fn.Line})
	m = settleHints(t, out.(Model), cmd)
	out, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	ed := m.editorForPath(src)
	if !ed.HasComplexitySiteMarks() || ed.HasComplexityHints() {
		t.Fatalf("after m: marks=%v hints=%v, want marks only", ed.HasComplexitySiteMarks(), ed.HasComplexityHints())
	}
	for _, s := range fn.Sites {
		if s.Cognitive > 0 {
			if _, ok := ed.ComplexitySiteMarkAt(s.Line); !ok {
				t.Errorf("site %s at line %d carries no mark", s.Kind, s.Line)
			}
		}
	}

	ed.JumpTo(fn.Line+1, 0)
	m = dismissOnboarding(m)
	out, _ = m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	m = out.(Model)
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.editorForPath(src).HasComplexitySiteMarks() {
		t.Fatal("typing must drop the one-shot marks")
	}
	if len(m.cxHints.show) != 0 {
		t.Fatalf("the spent request must be forgotten: %+v", m.cxHints.show)
	}
}

// TestComplexitySiteMarksShareMeasurement: a dirty buffer is measured once
// per key, and that one measurement lands both the hints and the marks.
func TestComplexitySiteMarksShareMeasurement(t *testing.T) {
	var calls atomic.Int32
	orig := cxHintMeasure
	cxHintMeasure = func(path, text string) []complexity.Function {
		calls.Add(1)
		return orig(path, text)
	}
	t.Cleanup(func() { cxHintMeasure = orig })

	m, src := cxOpenSource(t)
	fn := cxSitedFunction(t, m, src)
	m = cxSitesConfig(t, m, config.HintAll, config.HintAll)
	ed := m.editorForPath(src)
	ed.JumpTo(fn.EndLine+1, 0)
	m = dismissOnboarding(m)
	out, _ := m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	m = out.(Model)
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	pend, ok := m.cxHints.pending[src]
	if !ok {
		t.Fatal("the edit must arm the debounce")
	}
	cmd := m.complexityHintTick(complexityHintTickMsg{path: src, key: pend})
	if cmd == nil {
		t.Fatal("the tick must launch the measurement")
	}
	m = settleHints(t, m, cmd)
	if n := calls.Load(); n != 1 {
		t.Fatalf("Measure ran %d times for one key, want 1", n)
	}
	ed = m.editorForPath(src)
	if _, ok := ed.ComplexityHintAt(fn.Line); !ok {
		t.Error("the measurement must land the header hint")
	}
	if !ed.HasComplexitySiteMarks() {
		t.Error("the same measurement must land the site marks")
	}
}
