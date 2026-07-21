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

// jumpNotice runs a diagnostic-jump action and returns the toast text.
func jumpNotice(t *testing.T, m *Model, action string) string {
	t.Helper()
	next, cmd := m.runAction(action)
	*m = next
	if cmd == nil {
		t.Fatalf("%s must produce a NoticeMsg", action)
	}
	n, ok := cmd().(NoticeMsg)
	if !ok {
		t.Fatalf("%s: expected a NoticeMsg, got %#v", action, cmd())
	}
	return n.Text
}

func TestDiagnosticJumpNextPrevWraps(t *testing.T) {
	m, _ := loaded(t, "aaa\nbbb\nccc\nddd\n")
	m, _ = m.Update(ilsp.DiagnosticsMsg{Path: m.path, Diagnostics: []ilsp.Diagnostic{
		// Deliberately unsorted: the walk must be document-ordered.
		{Range: buffer.Range{Start: buffer.Position{Line: 2, Col: 1}, End: buffer.Position{Line: 2, Col: 2}}, Severity: 2, Message: "warn here"},
		{Range: buffer.Range{Start: buffer.Position{Line: 0, Col: 1}, End: buffer.Position{Line: 0, Col: 2}}, Severity: 1, Message: "first line\nsecond detail line"},
	}})
	if got := jumpNotice(t, &m, "next_diagnostic"); got != "error: first line" {
		t.Errorf("first jump toast = %q (message must be its first line)", got)
	}
	if m.cursor != (buffer.Position{Line: 0, Col: 1}) {
		t.Fatalf("first jump cursor = %+v", m.cursor)
	}
	if got := jumpNotice(t, &m, "next_diagnostic"); got != "warning: warn here" {
		t.Errorf("second jump toast = %q", got)
	}
	if m.cursor != (buffer.Position{Line: 2, Col: 1}) {
		t.Fatalf("second jump cursor = %+v", m.cursor)
	}
	// Past the last diagnostic: wrap to the first, flagged in the toast.
	if got := jumpNotice(t, &m, "next_diagnostic"); got != "error: first line (wrapped)" {
		t.Errorf("wrap jump toast = %q", got)
	}
	if m.cursor != (buffer.Position{Line: 0, Col: 1}) {
		t.Fatalf("wrap jump cursor = %+v", m.cursor)
	}
	// Backwards from the first diagnostic: wrap to the last.
	if got := jumpNotice(t, &m, "prev_diagnostic"); got != "warning: warn here (wrapped)" {
		t.Errorf("prev wrap toast = %q", got)
	}
	if m.cursor != (buffer.Position{Line: 2, Col: 1}) {
		t.Fatalf("prev wrap cursor = %+v", m.cursor)
	}
	if got := jumpNotice(t, &m, "prev_diagnostic"); got != "error: first line" {
		t.Errorf("prev jump toast = %q", got)
	}
}

func TestDiagnosticJumpEmptyNotifies(t *testing.T) {
	m, _ := loaded(t, "clean\n")
	if got := jumpNotice(t, &m, "next_diagnostic"); got != "no diagnostics in this file" {
		t.Errorf("empty next toast = %q", got)
	}
	if got := jumpNotice(t, &m, "prev_diagnostic"); got != "no diagnostics in this file" {
		t.Errorf("empty prev toast = %q", got)
	}
	if m.cursor != (buffer.Position{}) {
		t.Errorf("empty jump must not move the cursor, got %+v", m.cursor)
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
	// Type "Pr": fuzzy matching keeps all three ("Pr" is a subsequence of
	// Sprintf too, #845), but the boundary-anchored Println/Printf rank first.
	m = send(m, key('P'), key('r'))
	if got := len(m.filteredCompletion()); got != 3 {
		t.Fatalf("filtered = %d, want 3 (Println, Printf, Sprintf)", got)
	}
	// Equal fuzzy scores keep the sortText base order (label fallback), so
	// "Printf" sorts before "Println"; the scattered Sprintf ranks last.
	if got := m.filteredCompletion()[0].Label; got != "Printf" {
		t.Fatalf("top item = %q, want Printf (start-anchored match outranks scattered)", got)
	}
	// Accept the first (Printf): the prefix "Pr" is replaced by the full insert.
	m = send(m, special(tea.KeyEnter))
	if got := line(m, 0); got != "fmt.Printf" {
		t.Fatalf("after accept line = %q, want fmt.Printf", got)
	}
	if m.CompletionOpen() {
		t.Error("popup should close after accept")
	}
}

// TestCompletionAcceptReplacesTypedPrefix guards #330: a completion whose insert
// text starts with the identifier already typed before the cursor must replace
// that partial word rather than duplicate it. This is the manual (ctrl+space)
// trigger shape, where the popup is anchored at the cursor.
// TestCompletionFuzzyCamelCase guards #845: CamelCase initials fuzzy-match
// ("gCN" → getClassName), snake_case initials too, and non-matches drop out.
func TestCompletionFuzzyCamelCase(t *testing.T) {
	m, _ := loaded(t, "x.\n")
	m = insertModeAt(m, 0, 2)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 2, Items: []ilsp.CompletionItem{
		{Label: "getClassName", InsertText: "getClassName"},
		{Label: "getCount", InsertText: "getCount"},
		{Label: "do_request", InsertText: "do_request"},
	}})
	// Case-insensitive fuzzy also lets "gCN" hit getCount's mid-word n, but
	// the hump-anchored getClassName must rank first; do_request drops out.
	m = send(m, key('g'), key('C'), key('N'))
	got := labels(m.filteredCompletion())
	if len(got) == 0 || got[0] != "getClassName" {
		t.Fatalf("gCN filtered = %v, want getClassName ranked first", got)
	}
	for _, l := range got {
		if l == "do_request" {
			t.Fatalf("do_request must not match gCN, got %v", got)
		}
	}
}

// TestCompletionFilterTextWins guards #845: when a server sends filterText, the
// match runs against it instead of the label.
func TestCompletionFilterTextWins(t *testing.T) {
	m, _ := loaded(t, "x.\n")
	m = insertModeAt(m, 0, 2)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 2, Items: []ilsp.CompletionItem{
		{Label: "•decorated", FilterText: "decorated", InsertText: "decorated"},
		{Label: "plain", InsertText: "plain"},
	}})
	m = send(m, key('d'), key('e'), key('c'))
	got := m.filteredCompletion()
	if len(got) != 1 || got[0].Label != "•decorated" {
		t.Fatalf("dec filtered = %v, want the filterText-carrying item", labels(got))
	}
}

// TestCompletionSortTextOrder guards #845: with no prefix typed, items show in
// server sortText order, not arrival order.
func TestCompletionSortTextOrder(t *testing.T) {
	m, _ := loaded(t, "x.\n")
	m = insertModeAt(m, 0, 2)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 2, Items: []ilsp.CompletionItem{
		{Label: "zeta", SortText: "0002", InsertText: "zeta"},
		{Label: "alpha", SortText: "0003", InsertText: "alpha"},
		{Label: "mid", SortText: "0001", InsertText: "mid"},
	}})
	got := labels(m.filteredCompletion())
	want := []string{"mid", "zeta", "alpha"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func labels(items []ilsp.CompletionItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Label
	}
	return out
}

// TestCompletionSnippetAcceptAndTabstops guards #846: accepting a snippet item
// expands the placeholders, puts the cursor on the first tabstop, and
// tab/shift+tab walk the stops with typed text shifting the later ones.
func TestCompletionSnippetAcceptAndTabstops(t *testing.T) {
	m, _ := loaded(t, "\n")
	m = insertModeAt(m, 0, 0)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 0, Items: []ilsp.CompletionItem{
		{Label: "f", InsertText: "f($1, $2)", IsSnippet: true},
	}})
	m = send(m, special(tea.KeyEnter))
	if got := line(m, 0); got != "f(, )" {
		t.Fatalf("expanded line = %q, want f(, )", got)
	}
	if m.cursor.Col != 2 {
		t.Fatalf("cursor col = %d, want 2 (first tabstop)", m.cursor.Col)
	}
	m = send(m, key('x'), special(tea.KeyTab))
	if m.cursor.Col != 5 {
		t.Fatalf("after tab cursor col = %d, want 5 (second stop shifted by typed x)", m.cursor.Col)
	}
	m = send(m, key('y'))
	if got := line(m, 0); got != "f(x, y)" {
		t.Fatalf("line = %q, want f(x, y)", got)
	}
	// Shift+tab returns to the first stop's area (after its typed text).
	m = send(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.cursor.Col != 2 {
		t.Fatalf("after shift+tab cursor col = %d, want 2", m.cursor.Col)
	}
	// Esc ends the session; the next tab indents normally again.
	m = send(m, special(tea.KeyEscape))
	if m.snippet != nil {
		t.Fatal("esc must end the snippet session")
	}
}

// TestCompletionSnippetMalformedFallsBack guards #846: an unparsable snippet
// inserts its raw text and starts no session.
func TestCompletionSnippetMalformedFallsBack(t *testing.T) {
	m, _ := loaded(t, "\n")
	m = insertModeAt(m, 0, 0)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 0, Items: []ilsp.CompletionItem{
		{Label: "bad", InsertText: "bad${1:unterminated", IsSnippet: true},
	}})
	m = send(m, special(tea.KeyEnter))
	if got := line(m, 0); got != "bad${1:unterminated" {
		t.Fatalf("line = %q, want the raw text", got)
	}
	if m.snippet != nil {
		t.Fatal("malformed snippet must not start a session")
	}
}

// TestCompletionSnippetTrailingStopOnly guards #846: a snippet whose only stop
// is the end of the text ("foo()$0" shapes) needs no session — the cursor is
// already there.
func TestCompletionSnippetTrailingStopOnly(t *testing.T) {
	m, _ := loaded(t, "\n")
	m = insertModeAt(m, 0, 0)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 0, Items: []ilsp.CompletionItem{
		{Label: "now", InsertText: "now()$0", IsSnippet: true},
	}})
	m = send(m, special(tea.KeyEnter))
	if got := line(m, 0); got != "now()" {
		t.Fatalf("line = %q, want now()", got)
	}
	if m.snippet != nil {
		t.Fatal("trailing-only stop must not start a session")
	}
	if m.cursor.Col != 5 {
		t.Fatalf("cursor col = %d, want 5 (end of insert)", m.cursor.Col)
	}
}

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

// TestCompletionAcceptSigilPrefix guards #427: a completion whose insert text
// carries a sigil the identifier scan does not cover (PHP's "$") must replace
// the sigil too — "$he" completed to "$hello" is "$hello", not "$$hello".
func TestCompletionAcceptSigilPrefix(t *testing.T) {
	m, _ := loaded(t, "echo $he\n")
	m = insertModeAt(m, 0, 8) // after "$he"
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 8, Items: []ilsp.CompletionItem{
		{Label: "$hello", InsertText: "$hello"},
	}})
	if !m.CompletionOpen() {
		t.Fatal("completion popup should be open")
	}
	m = send(m, special(tea.KeyEnter))
	if got := line(m, 0); got != "echo $hello" {
		t.Fatalf("after accept line = %q, want echo $hello", got)
	}
}

// TestCompletionAcceptUnrelatedPunctuationStays ensures the sigil widening
// stops at punctuation the insert text does not start with: completing "he"
// after a dot must keep the dot.
func TestCompletionAcceptUnrelatedPunctuationStays(t *testing.T) {
	m, _ := loaded(t, "obj.he\n")
	m = insertModeAt(m, 0, 6)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 6, Items: []ilsp.CompletionItem{
		{Label: "hello", InsertText: "hello"},
	}})
	m = send(m, special(tea.KeyEnter))
	if got := line(m, 0); got != "obj.hello" {
		t.Fatalf("after accept line = %q, want obj.hello", got)
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

// TestShowDiagnosticsPopup covers the diagnostic details popup (#739): every
// diagnostic on the caret line shows with severity, source, code and message;
// a clean line opens nothing.
func TestShowDiagnosticsPopup(t *testing.T) {
	m, _ := loaded(t, "package main\nbad line\n")
	m, _ = m.Update(ilsp.DiagnosticsMsg{Path: m.path, Diagnostics: []ilsp.Diagnostic{
		{Range: buffer.Range{Start: buffer.Position{Line: 1, Col: 0}, End: buffer.Position{Line: 1, Col: 3}},
			Severity: 1, Message: "name is not defined", Source: "pyright", Code: "reportUndefinedVariable"},
		{Range: buffer.Range{Start: buffer.Position{Line: 1, Col: 4}, End: buffer.Position{Line: 1, Col: 8}},
			Severity: 2, Message: "unused variable"},
	}})
	m = typeKeys(m, "j") // caret onto line 1
	if !m.ShowDiagnostics() {
		t.Fatal("line 1 has diagnostics; the popup must open")
	}
	if !m.HoverOpen() {
		t.Fatal("popup must reuse the hover surface")
	}
	v := m.HoverView()
	for _, want := range []string{
		"error", "pyright · reportUndefinedVariable", "name is not defined",
		"warning", "unused variable",
	} {
		if !strings.Contains(v, want) {
			t.Fatalf("popup missing %q:\n%s", want, v)
		}
	}
	// Any key dismisses, like hover (the dismissing key is consumed).
	m = send(m, key('j'))
	if m.HoverOpen() {
		t.Fatal("popup must dismiss on the next key")
	}
	// A clean line opens nothing.
	m = typeKeys(m, "gg") // caret onto line 0, which has no diagnostics
	if m.ShowDiagnostics() {
		t.Fatal("line 0 has no diagnostics")
	}
	if m.HoverOpen() {
		t.Fatal("no popup on a clean line")
	}
}

// TestShowDiagnosticsMultilineMessage: message newlines become popup rows.
func TestShowDiagnosticsMultilineMessage(t *testing.T) {
	m, _ := loaded(t, "x\n")
	m, _ = m.Update(ilsp.DiagnosticsMsg{Path: m.path, Diagnostics: []ilsp.Diagnostic{
		{Range: buffer.Range{Start: buffer.Position{Line: 0, Col: 0}, End: buffer.Position{Line: 0, Col: 1}},
			Severity: 1, Message: "first line\nsecond line", Code: "E123"},
	}})
	if !m.ShowDiagnostics() {
		t.Fatal("popup must open")
	}
	v := m.HoverView()
	for _, want := range []string{"E123", "first line", "second line"} {
		if !strings.Contains(v, want) {
			t.Fatalf("popup missing %q:\n%s", want, v)
		}
	}
}
