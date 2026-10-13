package app

// complexity_hintdelta_test.go covers the live Δ against the Changes
// diff's base in the header hints (#2949): the builder's join by (name,
// ordinal), the tone of a fall and a rise, the diff armed by an open
// modified file with the window closed and the segment off, and a landed
// diff run repainting the hints without a second measurement.

import (
	"path/filepath"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/config"
	"ike/internal/editor"
	"ike/internal/vcs"
)

func TestComplexityHintsDeltaBuilder(t *testing.T) {
	base := []complexity.Function{
		{Name: "f", Line: 1, CCN: 20, Cognitive: 31},
		{Name: "g", Line: 9, CCN: 14, Cognitive: 31},
		{Name: "same", Line: 20, CCN: 3, Cognitive: 2},
		{Name: "dup", Line: 30, CCN: 1, Cognitive: 0},
		{Name: "dup", Line: 40, CCN: 2, Cognitive: 1},
	}
	head := []complexity.Function{
		{Name: "f", Line: 2, CCN: 14, Cognitive: 24},
		{Name: "g", Line: 12, CCN: 14, Cognitive: 24},
		{Name: "added", Line: 15, CCN: 9, Cognitive: 11},
		{Name: "same", Line: 22, CCN: 3, Cognitive: 2},
		{Name: "dup", Line: 31, CCN: 1, Cognitive: 0},
		{Name: "dup", Line: 41, CCN: 5, Cognitive: 1},
	}
	got := complexityHintsFor(head, 100, 100, 0, false, base, true)
	want := []string{
		"CCN 14 (−6) · cog 24 (−7)",
		"CCN 14 · cog 24 (−7)",
		"CCN 9 · cog 11 (new)",
		"CCN 3 · cog 2",
		"CCN 1 · cog 0",
		"CCN 5 (+3) · cog 1",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d hints, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if p := got[i].Plain(); p != w {
			t.Errorf("hint %d reads %q, want %q", i, p, w)
		}
	}
	if d := got[0].Deltas; d[0].DeltaWarn || d[1].DeltaWarn {
		t.Errorf("a fall must not warn: %+v", d)
	}
	if d := got[5].Deltas[0]; !d.DeltaWarn {
		t.Errorf("a rise must warn: %+v", d)
	}
	// Outside the diff nothing joins the hint.
	for i, h := range complexityHintsFor(head, 100, 100, 0, false, nil, false) {
		if h.Plain() != h.Text || h.Deltas != ([2]editor.ComplexityDelta{}) {
			t.Errorf("hint %d outside the diff carries a Δ: %+v", i, h)
		}
	}
}

// cxDeltaLand stores a finished diff run turning base into head for path,
// as applyComplexityDiff does on success.
func cxDeltaLand(m Model, path string, base, head []complexity.Function) Model {
	p := canonicalPath(path)
	d := complexity.DiffProject(map[string][]complexity.Function{p: base}, map[string][]complexity.Function{p: head}, 15, 15, 40)
	out, _ := m.Update(complexityDiffMsg{key: m.currentCxDiffKey(), changes: complexitypanel.Changes{Ready: true, Diff: d}})
	return out.(Model)
}

// TestComplexityHintsDeltaRepaintsWithoutMeasuring: a landed diff run
// repaints the hints of its files — the clean, indexed buffer from the
// snapshot and the dirty one from the measurement it already holds — and
// the measure counter shows no additional Measure call.
func TestComplexityHintsDeltaRepaintsWithoutMeasuring(t *testing.T) {
	var calls atomic.Int32
	orig := cxHintMeasure
	cxHintMeasure = func(path, text string) []complexity.Function {
		calls.Add(1)
		return orig(path, text)
	}
	t.Cleanup(func() { cxHintMeasure = orig })

	m, src := cxOpenSource(t)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	m = out.(Model)
	fns := m.ComplexityIndex().Snapshot().Files[canonicalPath(src)]
	if len(fns) == 0 {
		t.Fatal("the index holds no functions for the source")
	}
	moved := func(fns []complexity.Function) []complexity.Function {
		base := append([]complexity.Function(nil), fns...)
		base[0].CCN += 6
		base[0].Cognitive += 7
		return base
	}

	// Clean buffer: served from the index, the Δ joins on the landing.
	m = cxDeltaLand(m, src, moved(fns), fns)
	ed := m.editorForPath(src)
	h, ok := ed.ComplexityHintAt(fns[0].Line)
	if !ok || h.Deltas[0].Delta != "(−6)" || h.Deltas[1].Delta != "(−7)" {
		t.Fatalf("clean buffer hint = %+v (ok=%v), want the −6 / −7 Δ", h, ok)
	}

	// Dirty buffer: one measurement, then a landed run repaints from it.
	ed.JumpTo(fns[len(fns)-1].EndLine+1, 0)
	m = dismissOnboarding(m)
	out, _ = m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	m = out.(Model)
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	pend, ok := m.cxHints.pending[src]
	if !ok {
		t.Fatal("the edit must arm the debounce")
	}
	m = settleHints(t, m, m.complexityHintTick(complexityHintTickMsg{path: src, key: pend}))
	if n := calls.Load(); n != 1 {
		t.Fatalf("Measure ran %d times, want 1", n)
	}
	measured := m.cxHints.measured[src].fns
	if len(measured) == 0 {
		t.Fatal("the landed measurement must be kept")
	}
	base := append([]complexity.Function(nil), measured...)
	base[0].Cognitive += 7
	m = cxDeltaLand(m, src, base, measured)
	out, _ = m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	m = out.(Model)
	if n := calls.Load(); n != 1 {
		t.Fatalf("a landed diff run measured again: %d Measure calls, want 1", n)
	}
	if len(m.cxHints.pending) != 0 {
		t.Fatalf("a landed diff run must not arm a measurement: %+v", m.cxHints.pending)
	}
	h, ok = m.editorForPath(src).ComplexityHintAt(measured[0].Line)
	if !ok || h.Deltas[0].Delta != "" || h.Deltas[1].Delta != "(−7)" {
		t.Fatalf("dirty buffer hint = %+v (ok=%v), want only the cognitive Δ", h, ok)
	}
	if got := m.cxHints.sent[src]; got.diffGen != m.cxDiff.gen || got.diffGen == 0 {
		t.Fatalf("sent key = %+v, want the diff generation %d", got, m.cxDiff.gen)
	}
}

// TestComplexityDiffWantedByModifiedEditor: with the window closed and the
// segment off, an open editor of a measurable file the snapshot lists as
// modified arms the diff; a clean one does not, nor hints off.
func TestComplexityDiffWantedByModifiedEditor(t *testing.T) {
	m, src := cxOpenSource(t)
	cxSegmentConfig(t, false, true)
	if m.complexityPanel() != nil {
		t.Fatal("the window must start closed")
	}
	root := filepath.Dir(canonicalPath(src))
	m.vcs.snap = vcs.NewSnapshot(root, map[string]vcs.FileStatus{"other.go": vcs.StatusModified})
	if m.complexityDiffWanted() {
		t.Fatal("no open modified file: the diff must not be wanted")
	}
	out, _ := m.Update(vcs.SnapshotMsg{Snap: vcs.NewSnapshot(root, map[string]vcs.FileStatus{filepath.Base(src): vcs.StatusModified})})
	m = out.(Model)
	if !m.complexityDiffWanted() {
		t.Fatal("an open modified measurable file must want the diff")
	}
	if !m.cxDiff.tickArmed {
		t.Fatal("the snapshot's settled pass must arm the debounced diff")
	}

	c, _ := config.Load(config.Options{})
	c.Editor.Marks.Complexity = config.HintOff
	config.Set(c)
	if m.complexityDiffWanted() {
		t.Fatal("hints off: nothing consumes the Δ")
	}
}
