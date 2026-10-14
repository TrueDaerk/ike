package app

// complexity_intention_test.go covers the alt+enter entries on a function
// (#2965): which items the provider offers where, and what each command
// does through the pane's messages.

import (
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/config"
)

// cxIntentionApp opens src/a.go of the baseline fixture with the caret on
// the given 0-based line.
func cxIntentionApp(t *testing.T, line int) (Model, string) {
	t.Helper()
	m, proj := baselineApp(t)
	a := filepath.Join(proj, "src", "a.go")
	out, _ := m.openPathAt(a, line, 0)
	m = out.(Model)
	if ed := m.editorForPath(a); ed == nil {
		t.Fatal("a.go did not open")
	}
	return m, proj
}

// cxIntentionTitles lists the provider's items at the caret as title=command.
func cxIntentionTitles(m Model) []string {
	cx, ok := m.intentionContext()
	if !ok {
		return nil
	}
	var out []string
	for _, it := range m.complexityIntention().Items(cx) {
		out = append(out, it.Title+"="+it.CommandID)
	}
	return out
}

func TestComplexityIntentionItems(t *testing.T) {
	m, _ := cxIntentionApp(t, 2) // the header of A
	got := strings.Join(cxIntentionTitles(m), "\n")
	for _, want := range []string{
		"Show complexity breakdown=complexity.breakdownAtCaret",
		"Open in Complexity window=complexity.openAtCaret",
		"Accept into complexity baseline=complexity.baselineAtCaret",
		"Pin in Complexity window=complexity.pinAtCaret",
		"Ask agent to simplify=complexity.askAtCaret",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("header items lack %q:\n%s", want, got)
		}
	}
	// Inside the body: the same function's items.
	m.activeEditor().JumpTo(4, 0)
	if n := len(cxIntentionTitles(m)); n != 5 {
		t.Fatalf("body items = %d, want 5", n)
	}
	// Outside any function: none.
	m.activeEditor().JumpTo(0, 0)
	if got := cxIntentionTitles(m); len(got) != 0 {
		t.Fatalf("outside a function: %v", got)
	}
	// The baseline off drops its entry.
	m.activeEditor().JumpTo(2, 0)
	m = baselineReload(t, m, func(c *config.Config) { c.Complexity.Baseline = false })
	if got := strings.Join(cxIntentionTitles(m), "\n"); strings.Contains(got, "baseline") {
		t.Fatalf("baseline off still offers it:\n%s", got)
	}
	// The index off: none.
	m = baselineReload(t, m, func(c *config.Config) { c.Complexity.Enabled = false })
	if got := cxIntentionTitles(m); len(got) != 0 {
		t.Fatalf("index off still offers %v", got)
	}
}

func TestComplexityIntentionCommands(t *testing.T) {
	m, proj := cxIntentionApp(t, 4)
	out, _ := m.Update(ComplexityOpenAtCaretMsg{})
	m = out.(Model)
	p := m.complexityPanel()
	if p == nil || p.Tab() != complexitypanel.TabFile {
		t.Fatal("open must show the File tab")
	}
	if r, ok := p.Selected(); !ok || r.Fn.Name != "A" {
		t.Fatalf("selected = %+v", r)
	}

	out, _ = m.Update(ComplexityBreakdownAtCaretMsg{})
	m = out.(Model)
	if r, ok := p.Expanded(); !ok || r.Fn.Name != "A" {
		t.Fatal("breakdown must expand A")
	}
	// A repeated breakdown keeps it expanded.
	out, _ = m.Update(ComplexityBreakdownAtCaretMsg{})
	m = out.(Model)
	if _, ok := p.Expanded(); !ok {
		t.Fatal("a repeated breakdown must not collapse")
	}

	out, cmd := m.Update(ComplexityBaselineAtCaretMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("baseline must yield the toggle")
	}
	msg := cmd()
	if bt, ok := msg.(complexitypanel.BaselineToggleMsg); !ok || bt.Remove || len(bt.Items) != 1 || bt.Items[0].Fn.Name != "A" {
		t.Fatalf("baseline msg = %+v", msg)
	}
	out, _ = m.Update(msg)
	m = out.(Model)
	if b, err := complexity.LoadBaseline(complexity.BaselinePath(proj)); err != nil || b.Len() != 1 {
		t.Fatalf("baseline file = %+v, %v", b, err)
	}
	if got := strings.Join(cxIntentionTitles(m), "\n"); !strings.Contains(got, "Remove from complexity baseline") {
		t.Fatalf("an accepted function must offer the removal:\n%s", got)
	}

	out, cmd = m.Update(ComplexityPinAtCaretMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("pin must yield the toggle")
	}
	pm, ok := cmd().(complexitypanel.PinToggleMsg)
	if !ok || pm.Fn.Name != "A" {
		t.Fatalf("pin msg = %+v", pm)
	}
	out, _ = m.Update(pm)
	m = out.(Model)
	if got := strings.Join(cxIntentionTitles(m), "\n"); !strings.Contains(got, "Unpin") {
		t.Fatalf("a pinned function must offer the unpin:\n%s", got)
	}
}

func TestComplexityIntentionOutsideFunctionNotifies(t *testing.T) {
	m, _ := cxIntentionApp(t, 0)
	out, _ := m.Update(ComplexityPinAtCaretMsg{})
	m = out.(Model)
	if m.complexityPanel() != nil {
		t.Fatal("no function, no window")
	}
	if !strings.Contains(cxNotices(m), "not inside a measured function") {
		t.Fatalf("notices = %q", cxNotices(m))
	}
}
