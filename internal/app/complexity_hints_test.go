package app

// complexity_hints_test.go covers the inline complexity hint feed (#2919):
// the pure hint builder (text, warn thresholds, one hint per header line)
// and the app wiring — an opened, indexed buffer gets its hints in the
// settled pass with no parse, an edit arms the debounce whose tick measures
// the dirty text off-loop and stamps the new version, a stale tick is
// dropped, and switching the index off clears the hints.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/config"
	"ike/internal/editor"
)

func TestComplexityHintsForBuilder(t *testing.T) {
	fns := []complexity.Function{
		{Name: "low", Line: 2, CCN: 3, Cognitive: 2},
		{Name: "ccnOnly", Line: 10, CCN: 40, Cognitive: 4},
		{Name: "cogOnly", Line: 20, CCN: 5, Cognitive: 25},
		{Name: "lambda", Line: 20, CCN: 1, Cognitive: 0, Kind: complexity.KindLambda},
	}
	got := complexityHintsFor(fns, 15, 15, 0)
	want := []editor.ComplexityHint{
		{Line: 2, Text: "CCN 3 · cog 2"},
		{Line: 10, Text: "CCN 40 · cog 4", Warn: true},
		{Line: 20, Text: "CCN 5 · cog 25", Warn: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d hints, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("hint %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if complexityHintsFor(nil, 15, 15, 40) != nil {
		t.Error("no functions, no hints")
	}
}

// TestComplexityHintsCommentDensity: " · cmt 55%" joins the hint only at or
// above complexity.comment_density_warn (#2934) and warns; 0 never shows it.
func TestComplexityHintsCommentDensity(t *testing.T) {
	fns := []complexity.Function{
		{Name: "chatty", Line: 2, CCN: 3, Cognitive: 2, NLOC: 9, CommentLines: 11, CommentDensity: 0.55},
		{Name: "edge", Line: 10, CCN: 1, Cognitive: 0, NLOC: 3, CommentLines: 2, CommentDensity: 0.4},
		{Name: "quiet", Line: 20, CCN: 1, Cognitive: 0, NLOC: 7, CommentLines: 3, CommentDensity: 0.3},
	}
	got := complexityHintsFor(fns, 15, 15, 40)
	want := []editor.ComplexityHint{
		{Line: 2, Text: "CCN 3 · cog 2 · cmt 55%", Warn: true},
		{Line: 10, Text: "CCN 1 · cog 0 · cmt 40%", Warn: true},
		{Line: 20, Text: "CCN 1 · cog 0"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("hint %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	for _, h := range complexityHintsFor(fns, 15, 15, 0) {
		if h.Warn || strings.Contains(h.Text, "cmt") {
			t.Errorf("threshold 0 must disable the density: %+v", h)
		}
	}
}

// settleHints runs the given commands to completion but keeps debounce
// ticks out: a tea.Tick never quiesces in a test, and the test fires the
// tick messages itself.
func settleHints(t *testing.T, m Model, cmds ...tea.Cmd) Model {
	t.Helper()
	for len(cmds) > 0 {
		cmd := cmds[0]
		cmds = cmds[1:]
		if cmd == nil {
			continue
		}
		if m.cxHints != nil && len(m.cxHints.pending) > 0 {
			// A pending debounce means this batch holds its tick; the tick
			// is driven explicitly by the test.
			return m
		}
		switch msg := cmd().(type) {
		case nil:
		case tea.BatchMsg:
			cmds = append(cmds, msg...)
		default:
			out, next := m.Update(msg)
			m = out.(Model)
			cmds = append(cmds, next)
		}
	}
	return m
}

func TestComplexityHintsWiring(t *testing.T) {
	m, src := complexityApp(t)
	out, cmd := m.openPathAt(src, 0, 0)
	m = out.(Model)
	m = settleHints(t, m, cmd)
	// A settled pass: the clean, indexed buffer is served from the snapshot.
	out, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	ed := m.editorForPath(src)
	if ed == nil {
		t.Fatal("the source did not open")
	}
	if !ed.HasComplexityHints() {
		t.Fatal("an indexed buffer must get its hints in the settled pass")
	}
	fns := m.ComplexityIndex().Snapshot().Files[canonicalPath(src)]
	if len(fns) == 0 {
		t.Fatal("the index holds no functions for the source")
	}
	for _, fn := range fns {
		if h, ok := ed.ComplexityHintAt(fn.Line); !ok || h.Text == "" {
			t.Errorf("function %s at line %d has no hint", fn.Name, fn.Line)
		}
	}
	key := m.cxHints.sent[src]
	if key.gen == 0 || key.version != ed.DocVersion() {
		t.Fatalf("sent key = %+v, want the index generation and the buffer version", key)
	}

	// An edit: the next settled pass arms the debounce, the tick measures
	// the dirty text and stamps the new version.
	ed.JumpTo(fns[0].Line+1, 0)
	before := ed.DocVersion()
	m = dismissOnboarding(m)
	out, _ = m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	m = out.(Model)
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	ed = m.editorForPath(src)
	if ed.DocVersion() == before || !ed.Dirty() {
		t.Fatalf("the edit must dirty the buffer (version %d → %d, dirty=%v)", before, ed.DocVersion(), ed.Dirty())
	}
	pend, ok := m.cxHints.pending[src]
	if !ok || pend.version != ed.DocVersion() || pend.gen != 0 {
		t.Fatalf("pending key = %+v (ok=%v), want a measured key at version %d", pend, ok, ed.DocVersion())
	}
	// A stale tick (an older key) is dropped without launching anything.
	if cmd := m.complexityHintTick(complexityHintTickMsg{path: src, key: key}); cmd != nil {
		t.Fatal("a stale tick must not launch a measurement")
	}
	cmd = m.complexityHintTick(complexityHintTickMsg{path: src, key: pend})
	if cmd == nil {
		t.Fatal("the current tick must launch the off-loop measurement")
	}
	msg, ok := cmd().(complexityHintMeasuredMsg)
	if !ok || msg.version != ed.DocVersion() || len(msg.fns) == 0 {
		t.Fatalf("measurement = %+v (ok=%v)", msg, ok)
	}
	m = settleHints(t, m, func() tea.Msg { return msg })
	ed = m.editorForPath(src)
	if h, ok := ed.ComplexityHintAt(fns[0].Line); !ok || h.Text == "" {
		t.Fatalf("the measured hints must land on the header line: %+v %v", h, ok)
	}
	if m.cxHints.sent[src] != pend {
		t.Fatalf("sent key = %+v, want %+v", m.cxHints.sent[src], pend)
	}

	// The master switch off clears the hints once.
	c, _ := config.Load(config.Options{})
	c.Complexity.Enabled = false
	config.Set(c)
	t.Cleanup(func() { c, _ := config.Load(config.Options{}); config.Set(c) })
	out, _ = m.Update(config.ConfigReloadedMsg{Config: c})
	m = out.(Model)
	if ed = m.editorForPath(src); ed.HasComplexityHints() {
		t.Fatal("complexity.enabled=false must clear the hints")
	}
	if k := m.cxHints.sent[src]; k.enabled {
		t.Fatalf("sent key = %+v, want the disabled key", k)
	}
}
