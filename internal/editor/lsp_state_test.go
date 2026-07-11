package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	ilsp "ike/internal/lsp"
)

func TestDiagnosticsIndexAndCounts(t *testing.T) {
	m, _ := loaded(t, "package main\nbad line\n")
	m, _ = m.Update(ilsp.DiagnosticsMsg{Path: m.path, Diagnostics: []ilsp.Diagnostic{
		{Range: buffer.Range{Start: buffer.Position{Line: 1, Col: 0}, End: buffer.Position{Line: 1, Col: 3}}, Severity: 1, Message: "oops"},
		{Range: buffer.Range{Start: buffer.Position{Line: 0, Col: 0}, End: buffer.Position{Line: 0, Col: 2}}, Severity: 2, Message: "warn"},
	}})
	errs, warns := m.DiagnosticCounts()
	if errs != 1 || warns != 1 {
		t.Fatalf("counts = %d/%d, want 1/1", errs, warns)
	}
	if sev, ok := m.worstSeverityOnLine(1); !ok || sev != 1 {
		t.Errorf("line 1 worst severity = %d (%v)", sev, ok)
	}
	if sev, ok := m.diagSeverityAt(1, 1); !ok || sev != 1 {
		t.Errorf("diagSeverityAt(1,1) = %d (%v)", sev, ok)
	}
	if _, ok := m.diagSeverityAt(1, 5); ok {
		t.Error("col 5 is outside the diagnostic range")
	}
}

func TestDiagnosticsWrongPathIgnored(t *testing.T) {
	m, _ := loaded(t, "x\n")
	m, _ = m.Update(ilsp.DiagnosticsMsg{Path: "/other.go", Diagnostics: []ilsp.Diagnostic{
		{Range: buffer.Range{Start: buffer.Position{Line: 0, Col: 0}, End: buffer.Position{Line: 0, Col: 1}}, Severity: 1},
	}})
	if e, w := m.DiagnosticCounts(); e != 0 || w != 0 {
		t.Fatalf("diagnostics for another path should be ignored, got %d/%d", e, w)
	}
}

// enterInsertAtEnd puts the editor in insert mode at end of the buffer.
func insertModeAt(m Model, line, col int) Model {
	m.mode = Insert
	m.insert = insertSession{active: true}
	m.cursor = buffer.Position{Line: line, Col: col}
	return m
}

func TestCompletionOpenFilterAccept(t *testing.T) {
	m, _ := loaded(t, "fmt.\n")
	m = insertModeAt(m, 0, 4) // just after the dot
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 4, Items: []ilsp.CompletionItem{
		{Label: "Println", InsertText: "Println"},
		{Label: "Printf", InsertText: "Printf"},
		{Label: "Sprintf", InsertText: "Sprintf"},
	}})
	if !m.CompletionOpen() {
		t.Fatal("completion popup should be open")
	}
	// Type "Pr" to filter to Println/Printf.
	m = send(m, key('P'), key('r'))
	if got := len(m.filteredCompletion()); got != 2 {
		t.Fatalf("filtered = %d, want 2 (Println, Printf)", got)
	}
	// Accept the first (Println): the prefix "Pr" is replaced by the full insert.
	m = send(m, special(tea.KeyEnter))
	if got := line(m, 0); got != "fmt.Println" {
		t.Fatalf("after accept line = %q, want fmt.Println", got)
	}
	if m.CompletionOpen() {
		t.Error("popup should close after accept")
	}
}

// TestCompletionAcceptReplacesTypedPrefix guards #330: a completion whose insert
// text starts with the identifier already typed before the cursor must replace
// that partial word rather than duplicate it. This is the manual (ctrl+space)
// trigger shape, where the popup is anchored at the cursor.
func TestCompletionAcceptReplacesTypedPrefix(t *testing.T) {
	m, _ := loaded(t, "xyz.__\n")
	m = insertModeAt(m, 0, 6) // after "xyz.__", as a manual trigger would anchor
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 6, Items: []ilsp.CompletionItem{
		{Label: "__dict__", InsertText: "__dict__"},
	}})
	if !m.CompletionOpen() {
		t.Fatal("completion popup should be open")
	}
	m = send(m, special(tea.KeyEnter))
	if got := line(m, 0); got != "xyz.__dict__" {
		t.Fatalf("after accept line = %q, want xyz.__dict__", got)
	}
}

func TestCompletionEscapeCloses(t *testing.T) {
	m, _ := loaded(t, "x.\n")
	m = insertModeAt(m, 0, 2)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 2, Items: []ilsp.CompletionItem{{Label: "foo", InsertText: "foo"}}})
	if !m.CompletionOpen() {
		t.Fatal("popup should be open")
	}
	m = send(m, special(tea.KeyEscape))
	if m.CompletionOpen() {
		t.Error("escape should close the popup")
	}
	if line(m, 0) != "x." {
		t.Errorf("escape should not insert, line = %q", line(m, 0))
	}
}

func TestHoverShowsAndDismisses(t *testing.T) {
	m, _ := loaded(t, "code\n")
	m, _ = m.Update(ilsp.HoverMsg{Path: m.path, Contents: "func Foo()"})
	if !m.HoverOpen() {
		t.Fatal("hover should be open")
	}
	if !strings.Contains(m.HoverView(), "Foo") {
		t.Errorf("hover view missing content: %q", m.HoverView())
	}
	m = send(m, key('j')) // any key dismisses
	if m.HoverOpen() {
		t.Error("hover should be dismissed on key press")
	}
}

// TestHoverStripsMarkdownFences guards #379: LSP hover markdown fence markers
// ("```go", "```") are markup, not content — they must not appear in the popup;
// the thematic break ("---") renders as a horizontal rule instead of dashes.
func TestHoverStripsMarkdownFences(t *testing.T) {
	m, _ := loaded(t, "code\n")
	contents := "```go\nfunc Printf(format string) (n int, err error)\n```\n---\nPrintf formats stuff."
	m, _ = m.Update(ilsp.HoverMsg{Path: m.path, Contents: contents})
	if !m.HoverOpen() {
		t.Fatal("hover should be open")
	}
	v := m.HoverView()
	if strings.Contains(v, "```") {
		t.Errorf("fence markers leaked into hover view:\n%s", v)
	}
	if strings.Contains(v, "---") {
		t.Errorf("thematic break rendered as text:\n%s", v)
	}
	if !strings.Contains(v, "─") {
		t.Errorf("thematic break should render as a rule:\n%s", v)
	}
	if !strings.Contains(v, "func Printf(format string) (n int, err error)") {
		t.Errorf("signature missing from hover view:\n%s", v)
	}
	if !strings.Contains(v, "Printf formats stuff.") {
		t.Errorf("doc prose missing from hover view:\n%s", v)
	}
}

// TestHoverCodeBlockVisuallyDistinct guards #379's second criterion: fenced code
// carries styling (syntax captures or the accent fallback) that the plain doc
// prose does not.
func TestHoverCodeBlockVisuallyDistinct(t *testing.T) {
	m, _ := loaded(t, "code\n")
	m, _ = m.Update(ilsp.HoverMsg{Path: m.path, Contents: "```go\nfunc Foo()\n```\nprose line"})
	var code, prose string
	for _, l := range m.hover.lines {
		if strings.Contains(l.text, "Foo") {
			code = l.text
		}
		if strings.Contains(l.text, "prose") {
			prose = l.text
		}
	}
	if code == "" || prose == "" {
		t.Fatalf("hover lines missing code or prose: %+v", m.hover.lines)
	}
	if !strings.Contains(code, "\x1b[") {
		t.Errorf("code line should carry styling, got %q", code)
	}
	if strings.Contains(prose, "\x1b[") {
		t.Errorf("prose line should stay unstyled, got %q", prose)
	}
}

// TestCtrlSpaceTriggersCompletion guards #302: ctrl+space in insert mode
// emits the completion-trigger event (both the Kitty ctrl+' ' and the legacy
// ctrl+@ spellings), and a re-press with the popup open re-queries.
func TestCtrlSpaceTriggersCompletion(t *testing.T) {
	m, _ := loaded(t, "fmt\n")
	var got []EventKind
	m.SetEmitter(EmitterFunc(func(e Event) { got = append(got, e.Kind) }))
	m = insertModeAt(m, 0, 3)

	m = send(m, tea.KeyPressMsg{Code: ' ', Mod: tea.ModCtrl})
	m = send(m, tea.KeyPressMsg{Code: '@', Mod: tea.ModCtrl})
	n := 0
	for _, k := range got {
		if k == EventCompletionTrigger {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("ctrl+space variants must emit completion triggers, got %d of %v", n, got)
	}
	if line(m, 0) != "fmt" {
		t.Fatalf("ctrl+space must not insert text, line=%q", line(m, 0))
	}
}
