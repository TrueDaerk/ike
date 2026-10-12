package editor

// complexityhints_test.go covers the inline complexity hints (0550, #2919):
// rendering after the buffer text and after a code lens, the version-stale
// drop, the warning colour, the editor.marks.complexity toggle, and the
// no-room rule (a hint never truncates and never overdraws text).

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

func cxHintsMsg(path string, version int, hints ...ComplexityHint) ComplexityHintsMsg {
	return ComplexityHintsMsg{Path: path, Hints: hints, Version: version}
}

// TestComplexityHintsRenderTrailing: the hint renders dimmed after the
// header text without touching the buffer; nil clears.
func TestComplexityHintsRenderTrailing(t *testing.T) {
	m, path := loaded(t, "func A(x int) int {\n\treturn x\n}\n")
	m, _ = m.Update(cxHintsMsg(path, m.DocVersion(), ComplexityHint{Line: 0, Text: "CCN 2 · cog 1"}))
	if got, want := firstLine(m), "func A(x int) int {  CCN 2 · cog 1"; got != want {
		t.Errorf("rendered = %q, want %q", got, want)
	}
	if m.buf.Line(0) != "func A(x int) int {" {
		t.Error("hints are virtual text and must not change the buffer")
	}
	if h, ok := m.ComplexityHintAt(0); !ok || h.Text != "CCN 2 · cog 1" || h.Warn {
		t.Errorf("ComplexityHintAt(0) = %+v, %v", h, ok)
	}
	if _, ok := m.ComplexityHintAt(1); ok {
		t.Error("line 1 carries no hint")
	}
	m, _ = m.Update(cxHintsMsg(path, m.DocVersion()))
	if got := firstLine(m); got != "func A(x int) int {" || m.HasComplexityHints() {
		t.Errorf("after clear rendered = %q (has=%v)", got, m.HasComplexityHints())
	}
}

// TestComplexityHintsAfterCodeLens: the hint follows the code lens on the
// same line.
func TestComplexityHintsAfterCodeLens(t *testing.T) {
	m, path := loaded(t, "func TestFoo(t *testing.T) {\n}\n")
	m, _ = m.Update(lensesMsg(path, ilsp.CodeLens{Line: 0, Title: "run test"}))
	m, _ = m.Update(cxHintsMsg(path, m.DocVersion(), ComplexityHint{Line: 0, Text: "CCN 1 · cog 0"}))
	if got, want := firstLine(m), "func TestFoo(t *testing.T) {  run test  CCN 1 · cog 0"; got != want {
		t.Errorf("rendered = %q, want %q", got, want)
	}
}

// TestComplexityHintsStaleVersionDropped: a push measured at another
// document version is dropped, so a hint never lands on a moved line.
func TestComplexityHintsStaleVersionDropped(t *testing.T) {
	m, path := loaded(t, "func A() {\n}\n")
	v := m.DocVersion()
	m = send(m, key('O'), special(27)) // open a line above: the header moves to line 1
	if m.DocVersion() == v {
		t.Fatal("the edit must bump the document version")
	}
	m, _ = m.Update(cxHintsMsg(path, v, ComplexityHint{Line: 0, Text: "CCN 1 · cog 0"}))
	if m.HasComplexityHints() {
		t.Fatal("a stale-version push must be dropped")
	}
	if !m.SetComplexityHints([]ComplexityHint{{Line: 1, Text: "CCN 1 · cog 0"}}, m.DocVersion()) {
		t.Fatal("a current-version push must be adopted")
	}
	if _, ok := m.ComplexityHintAt(1); !ok {
		t.Error("the adopted hint must sit on line 1")
	}
	// A clear is never stale.
	if !m.SetComplexityHints(nil, v) || m.HasComplexityHints() {
		t.Error("a clear must apply regardless of version")
	}
}

// TestComplexityHintsOtherPathIgnored keeps another document's hints out.
func TestComplexityHintsOtherPathIgnored(t *testing.T) {
	m, _ := loaded(t, "func A() {\n}\n")
	m, _ = m.Update(cxHintsMsg("/other.go", m.DocVersion(), ComplexityHint{Line: 0, Text: "CCN 9 · cog 9"}))
	if m.HasComplexityHints() {
		t.Error("other-path hints must be ignored")
	}
}

// TestComplexityHintsWarnColour: a Warn hint draws in the theme's warning
// colour, an ordinary one in the dimmed inlay tone.
func TestComplexityHintsWarnColour(t *testing.T) {
	m, path := loaded(t, "func A() {\n}\nfunc B() {\n}\n")
	m, _ = m.Update(cxHintsMsg(path, m.DocVersion(),
		ComplexityHint{Line: 0, Text: "CCN 20 · cog 3", Warn: true},
		ComplexityHint{Line: 2, Text: "CCN 1 · cog 0"},
	))
	view := m.View()
	warn := lipgloss.NewStyle().Italic(true).Foreground(m.theme().Warning).Render("  CCN 20 · cog 3")
	if !strings.Contains(view, warn) {
		t.Errorf("a warn hint must render in the warning colour; view:\n%s", view)
	}
	plain := lipgloss.NewStyle().Italic(true).Foreground(m.theme().InlayHint).Faint(true).Render("  CCN 1 · cog 0")
	if !strings.Contains(view, plain) {
		t.Errorf("an ordinary hint must render dimmed; view:\n%s", view)
	}
}

// TestComplexityHintsConfigToggle hides the hints while
// editor.marks.complexity is off and resumes from the cached set when it
// comes back — rendering-only, like the code-lens toggle.
func TestComplexityHintsConfigToggle(t *testing.T) {
	m, path := loaded(t, "func A() {\n}\n")
	m, _ = m.Update(cxHintsMsg(path, m.DocVersion(), ComplexityHint{Line: 0, Text: "CCN 1 · cog 0"}))
	m.Configure(host.MapConfig{"editor.marks.complexity": "false"})
	if got := firstLine(m); got != "func A() {" {
		t.Errorf("rendered = %q, hints must hide while toggled off", got)
	}
	if _, ok := m.ComplexityHintAt(0); ok {
		t.Error("ComplexityHintAt must answer nothing while toggled off")
	}
	m.Configure(host.MapConfig{"editor.marks.complexity": "true"})
	if got, want := firstLine(m), "func A() {  CCN 1 · cog 0"; got != want {
		t.Errorf("rendered = %q, want %q (cached hints resume)", got, want)
	}
}

// TestComplexityHintsNeverOverdraw: a header that leaves no room for the
// whole hint shows none — never a truncated one, never one over the text —
// and a line wider than the pane shows none either.
func TestComplexityHintsNeverOverdraw(t *testing.T) {
	header := "func A() {"
	m, path := loaded(t, header+"\n}\n")
	m.SetSize(40, 5)
	hint := ComplexityHint{Line: 0, Text: "CCN 1 · cog 0"}
	m, _ = m.Update(cxHintsMsg(path, m.DocVersion(), hint))
	full := firstLine(m)
	if !strings.HasSuffix(full, "  CCN 1 · cog 0") {
		t.Fatalf("at 40 columns the hint must fit: %q", full)
	}
	// Shrink until the hint no longer fits whole: the row must then be the
	// bare header, with no partial hint.
	textW := lipgloss.Width(full)
	for w := textW + 2; w >= 1; w-- {
		m.SetSize(w, 5)
		got := firstLine(m)
		if strings.HasSuffix(got, "CCN 1 · cog 0") {
			continue
		}
		if strings.Contains(got, "CCN") || strings.Contains(got, "cog") {
			t.Fatalf("width %d: a hint must never truncate: %q", w, got)
		}
	}
	// A line wider than the pane shows no hint at all.
	m.SetSize(6, 5)
	if got := firstLine(m); strings.Contains(got, "CCN") {
		t.Fatalf("a line wider than the pane must show no hint: %q", got)
	}
}

// TestComplexityHintsCaretLine: the hint stays visible on the caret line
// while there is room, and yields once the caret cell takes the last cell.
func TestComplexityHintsCaretLine(t *testing.T) {
	m, path := loaded(t, "func A() {\n}\n")
	m, _ = m.Update(cxHintsMsg(path, m.DocVersion(), ComplexityHint{Line: 0, Text: "CCN 1 · cog 0"}))
	m = send(m, keys("$")...) // caret on the header's last cell
	if got := firstLine(m); !strings.HasSuffix(got, "CCN 1 · cog 0") {
		t.Errorf("with room, the caret line keeps its hint: %q", got)
	}
	// Exactly header + caret cell + hint fits at this width; one less and
	// the hint must go rather than overlap the caret.
	need := lipgloss.Width("func A() {") + 1 + lipgloss.Width("  CCN 1 · cog 0")
	m.SetSize(need+m.GutterWidth(), 5)
	m = send(m, keys("A")...) // insert mode, caret past the end
	if got := firstLine(m); !strings.HasSuffix(got, "CCN 1 · cog 0") {
		t.Errorf("header + caret + hint must fit exactly: %q", got)
	}
	m.SetSize(need-1+m.GutterWidth(), 5)
	if got := firstLine(m); strings.Contains(got, "CCN") {
		t.Errorf("one cell short, the caret line must drop the hint: %q", got)
	}
}
