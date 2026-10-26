package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
)

// complexity_duplicates_test.go covers the Duplicates tab's app half
// (#3044): a closed window launches nothing, an open one launches exactly
// one detection per stale generation on the settled pass, the result lands
// in the pane, a generation arriving mid-detection leaves it stale, and a
// changed clone size re-runs.

func TestComplexityDuplicatesBookkeeping(t *testing.T) {
	m, _ := timeApp(t)
	st := m.cxDup
	if st == nil || !st.stale || st.computing {
		t.Fatalf("initial state = %+v, want stale and idle", st)
	}
	// The index settles while the window is closed: stale, nothing runs.
	out, cmd := m.Update(ComplexityChangedMsg{Gen: 1})
	m = out.(Model)
	if st.computing || st.seen != 1 {
		t.Fatalf("closed window: computing=%v seen=%d", st.computing, st.seen)
	}
	// Opening the window runs the detector once on the settled pass.
	out, cmd = m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	if !st.computing || cmd == nil || st.started != 1 || st.minTokens != complexity.DefaultDuplicatesMinTokens {
		t.Fatalf("open window must launch: computing=%v cmd=%v started=%d min=%d", st.computing, cmd != nil, st.started, st.minTokens)
	}
	p := m.complexityPanel()
	p.SetTab(complexitypanel.TabDuplicates)
	if d := p.DuplicatesData(); !d.Computing || d.Ready || d.MinTokens != 50 {
		t.Fatalf("pane while detecting = %+v", d)
	}
	if !strings.Contains(p.View(), "detecting duplicates…") {
		t.Fatalf("pane must say detecting:\n%s", p.View())
	}
	// A second settled pass launches nothing while one runs.
	out, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	if st.started != 1 || !st.computing {
		t.Fatal("a running detection must not be doubled")
	}
	// The result lands: the pane shows it, the state is fresh.
	groups := []complexity.CloneGroup{{Lang: "go", Tokens: 80, Occurrences: []complexity.Occurrence{
		{Path: "/p/a.go", Line: 1, EndLine: 9, Start: 0, End: 80}, {Path: "/p/b.go", Line: 4, EndLine: 12, Start: 0, End: 80}}}}
	out, _ = m.Update(complexityDuplicatesMsg{started: 1, minTokens: 50, groups: groups, ms: 3})
	m = out.(Model)
	if st.computing || st.stale {
		t.Fatalf("after the result: computing=%v stale=%v", st.computing, st.stale)
	}
	if d := m.ComplexityDuplicates(); !d.Ready || d.Computing || len(d.Groups) != 1 || d.MinTokens != 50 {
		t.Fatalf("result = %+v", d)
	}
	p = m.complexityPanel()
	if rows := p.Rows(); len(rows) != 1 || rows[0].Dup == nil || rows[0].Dup.Group.Tokens != 80 {
		t.Fatalf("pane rows = %+v", rows)
	}
	// Another settled pass is quiet: nothing is stale.
	out, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	if st.computing {
		t.Fatal("a fresh result must not re-run")
	}
	// A generation during a detection leaves the result stale and the next
	// settled pass runs again.
	out, _ = m.Update(ComplexityChangedMsg{Gen: 2})
	m = out.(Model)
	if !st.computing || st.started != 2 {
		t.Fatalf("a stale generation must launch on the settled pass: computing=%v started=%d", st.computing, st.started)
	}
	out, _ = m.Update(ComplexityChangedMsg{Gen: 3})
	m = out.(Model)
	out, _ = m.Update(complexityDuplicatesMsg{started: 2, minTokens: 50, groups: nil})
	m = out.(Model)
	// The result is stale, so the same settled pass re-runs at once.
	if !st.computing || st.started != 3 {
		t.Fatalf("a generation mid-detection must leave the result stale and re-run: computing=%v started=%d", st.computing, st.started)
	}
	if d := m.ComplexityDuplicates(); !d.Ready || len(d.Groups) != 0 || !d.Computing {
		t.Fatalf("the landed result still shows while the next runs: %+v", d)
	}
	out, _ = m.Update(complexityDuplicatesMsg{started: 3, minTokens: 50})
	m = out.(Model)
	if st.stale || st.computing {
		t.Fatal("caught up")
	}
	// A changed clone size re-runs with the new size.
	st.minTokens = 120 // as if the setting moved under the last result
	out, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	if !st.computing || st.minTokens != 50 {
		t.Fatalf("a changed clone size must re-run: computing=%v min=%d", st.computing, st.minTokens)
	}
	// A real detection over the test project's snapshot lands through the
	// command: the index holds no clones, so the result is empty but ready.
	out, _ = m.Update(complexityDuplicatesMsg{started: 3, minTokens: 50})
	m = out.(Model)
	cmd = m.complexityDuplicatesSyncCmd()
	if cmd != nil {
		t.Fatal("fresh: no launch")
	}
	st.stale = true
	cmd = m.complexityDuplicatesSyncCmd()
	if cmd == nil {
		t.Fatal("stale: launch")
	}
	res, ok := cmd().(complexityDuplicatesMsg)
	if !ok || res.minTokens != 50 {
		t.Fatalf("the command must deliver the detection: %+v", res)
	}
	out, _ = m.Update(res)
	m = out.(Model)
	if d := m.ComplexityDuplicates(); !d.Ready || d.Computing {
		t.Fatalf("landed = %+v", d)
	}
	_ = m
}
