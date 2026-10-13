package editor

// complexitysites_test.go covers the per-site complexity marks (#2947):
// trailing rendering on the contributing lines, the header hint winning a
// shared line, the version-stale drop, the warning colour, independence
// from editor.marks.complexity, and the never-overdraw rule.

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/host"
)

func cxSitesMsg(path string, version int, marks ...ComplexitySiteMark) ComplexitySiteMarksMsg {
	return ComplexitySiteMarksMsg{Path: path, Marks: marks, Version: version}
}

// viewLine is the n-th rendered row, ANSI-stripped and right-trimmed.
func viewLine(m Model, n int) string {
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	if n >= len(rows) {
		return ""
	}
	return strings.TrimRight(rows[n], " ")
}

// TestComplexitySitesRenderTrailing: site marks render after their lines,
// a header hint on the same line wins, and nil clears.
func TestComplexitySitesRenderTrailing(t *testing.T) {
	m, path := loaded(t, "func A(x int) {\n\tif x > 0 {\n\t\tfor {\n\t\t}\n\t}\n}\n")
	m, _ = m.Update(cxSitesMsg(path, m.DocVersion(),
		ComplexitySiteMark{Line: 0, Text: "+1"},
		ComplexitySiteMark{Line: 1, Text: "+1"},
		ComplexitySiteMark{Line: 2, Text: "+2 (nesting 1)"},
	))
	if got := viewLine(m, 1); !strings.HasSuffix(got, "if x > 0 {  +1") {
		t.Errorf("line 1 = %q, want the +1 mark after the text", got)
	}
	if got := viewLine(m, 2); !strings.HasSuffix(got, "for {  +2 (nesting 1)") {
		t.Errorf("line 2 = %q, want the nesting mark", got)
	}
	if got, want := viewLine(m, 0), "func A(x int) {  +1"; got != want {
		t.Errorf("line 0 = %q, want %q (no header hint yet)", got, want)
	}
	m, _ = m.Update(cxHintsMsg(path, m.DocVersion(), ComplexityHint{Line: 0, Text: "CCN 3 · cog 3"}))
	if got, want := viewLine(m, 0), "func A(x int) {  CCN 3 · cog 3"; got != want {
		t.Errorf("line 0 = %q, want %q (the header hint wins)", got, want)
	}
	if m.buf.Line(1) != "\tif x > 0 {" {
		t.Error("site marks are virtual text and must not change the buffer")
	}
	// The header hint toggle does not hide the site marks, and frees the
	// header line for its site mark.
	m.Configure(host.MapConfig{"editor.marks.complexity": "off"})
	if got := viewLine(m, 1); !strings.HasSuffix(got, "+1") {
		t.Errorf("editor.marks.complexity=off must not hide site marks: %q", got)
	}
	if got, want := viewLine(m, 0), "func A(x int) {  +1"; got != want {
		t.Errorf("line 0 = %q, want %q (hint hidden, site mark shows)", got, want)
	}
	m, _ = m.Update(cxSitesMsg(path, m.DocVersion()))
	if m.HasComplexitySiteMarks() || strings.Contains(viewLine(m, 1), "+1") {
		t.Error("nil marks must clear")
	}
}

// TestComplexitySitesStaleVersionDropped: a push measured at another
// document version is dropped; a clear never is.
func TestComplexitySitesStaleVersionDropped(t *testing.T) {
	m, path := loaded(t, "func A() {\n}\n")
	v := m.DocVersion()
	m = send(m, key('O'), special(27))
	m, _ = m.Update(cxSitesMsg(path, v, ComplexitySiteMark{Line: 0, Text: "+1"}))
	if m.HasComplexitySiteMarks() {
		t.Fatal("a stale-version push must be dropped")
	}
	if !m.SetComplexitySiteMarks([]ComplexitySiteMark{{Line: 1, Text: "+1"}}, m.DocVersion()) {
		t.Fatal("a current-version push must be adopted")
	}
	if s, ok := m.ComplexitySiteMarkAt(1); !ok || s.Text != "+1" {
		t.Errorf("ComplexitySiteMarkAt(1) = %+v, %v", s, ok)
	}
	if !m.SetComplexitySiteMarks(nil, v) || m.HasComplexitySiteMarks() {
		t.Error("a clear must apply regardless of version")
	}
	m, _ = m.Update(cxSitesMsg("/other.go", m.DocVersion(), ComplexitySiteMark{Line: 0, Text: "+1"}))
	if m.HasComplexitySiteMarks() {
		t.Error("other-path marks must be ignored")
	}
}

// TestComplexitySitesWarnColour: a Warn mark draws in the warning colour,
// an ordinary one dimmed — the header hint's styles.
func TestComplexitySitesWarnColour(t *testing.T) {
	m, path := loaded(t, "a\nb\n")
	m, _ = m.Update(cxSitesMsg(path, m.DocVersion(),
		ComplexitySiteMark{Line: 0, Text: "+3 (nesting 2)", Warn: true},
		ComplexitySiteMark{Line: 1, Text: "+1 ||"},
	))
	view := m.View()
	if warn := lipgloss.NewStyle().Italic(true).Foreground(m.theme().Warning).Render("  +3 (nesting 2)"); !strings.Contains(view, warn) {
		t.Errorf("a warn mark must render in the warning colour; view:\n%s", view)
	}
	if plain := lipgloss.NewStyle().Italic(true).Foreground(m.theme().InlayHint).Faint(true).Render("  +1 ||"); !strings.Contains(view, plain) {
		t.Errorf("an ordinary mark must render dimmed; view:\n%s", view)
	}
}

// TestComplexitySitesNeverOverdraw: a line that leaves no room for the
// whole mark shows none.
func TestComplexitySitesNeverOverdraw(t *testing.T) {
	m, path := loaded(t, "if a && b {\n}\n")
	m, _ = m.Update(cxSitesMsg(path, m.DocVersion(), ComplexitySiteMark{Line: 0, Text: "+1 · +1 &&"}))
	for w := 40; w >= 1; w-- {
		m.SetSize(w, 5)
		got := firstLine(m)
		if strings.HasSuffix(got, "+1 · +1 &&") {
			continue
		}
		if strings.Contains(got, "+1") {
			t.Fatalf("width %d: a site mark must never truncate: %q", w, got)
		}
	}
}
