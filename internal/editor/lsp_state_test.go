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
