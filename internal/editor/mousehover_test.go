package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	ilsp "ike/internal/lsp"
)

// mousehover_test.go covers the editor side of the mouse-idle hover (#1129):
// the HoverTarget hit-test, the diagnostic-only popup, the positioned
// HoverMsg validation, and the anchor/dismissal rules.

// hoverModel is a sized model with a known two-line buffer.
func hoverModel(t *testing.T) Model {
	t.Helper()
	m, _ := loaded(t, "package main\n\nfunc target() {}\n")
	m.SetSize(60, 10)
	return m
}

func TestHoverTargetMapsContentCell(t *testing.T) {
	m := hoverModel(t)
	gw := m.GutterWidth()
	pos, ok := m.HoverTarget(gw+2, 0)
	if !ok || pos != (buffer.Position{Line: 0, Col: 2}) {
		t.Fatalf("HoverTarget(gw+2, 0) = %v (%v), want (0,2)", pos, ok)
	}
}

func TestHoverTargetRejectsGutter(t *testing.T) {
	m := hoverModel(t)
	for x := 0; x < m.GutterWidth(); x++ {
		if _, ok := m.HoverTarget(x, 0); ok {
			t.Fatalf("x=%d lies in the gutter and must not be a hover target", x)
		}
	}
}

func TestHoverTargetRejectsPastEndOfLineAndBuffer(t *testing.T) {
	m := hoverModel(t)
	gw := m.GutterWidth()
	if _, ok := m.HoverTarget(gw+len("package main"), 0); ok {
		t.Fatal("a cell past the end of the line must not be a hover target")
	}
	if _, ok := m.HoverTarget(gw, 1); ok {
		t.Fatal("an empty line has no hoverable cell")
	}
	if _, ok := m.HoverTarget(gw, 9); ok {
		t.Fatal("a row past the last buffer line must not be a hover target")
	}
}

func TestHoverTargetRejectsScrollbar(t *testing.T) {
	m, _ := loaded(t, strings.Repeat("some line of text here\n", 50))
	m.SetSize(30, 5)
	if !m.ScrollbarHit(29, 0) {
		t.Fatal("setup: expected a visible scrollbar at the right edge")
	}
	if _, ok := m.HoverTarget(29, 0); ok {
		t.Fatal("the scrollbar column must not be a hover target")
	}
}

// diagAt installs one error diagnostic covering (line, colStart..colEnd).
func diagAt(t *testing.T, m Model, line, colStart, colEnd int, msg string) Model {
	t.Helper()
	m, _ = m.Update(ilsp.DiagnosticsMsg{Path: m.path, Diagnostics: []ilsp.Diagnostic{{
		Range: buffer.Range{
			Start: buffer.Position{Line: line, Col: colStart},
			End:   buffer.Position{Line: line, Col: colEnd},
		},
		Severity: 1, Message: msg, Source: "vet",
	}}})
	return m
}

func TestShowMouseHoverDiagnosticOnlyWithoutLSP(t *testing.T) {
	m := hoverModel(t)
	m = diagAt(t, m, 0, 0, 7, "bad package")
	pos := buffer.Position{Line: 0, Col: 3}
	if !m.ShowMouseHover(pos) {
		t.Fatal("a diagnostic covers the cell: the popup must open without any LSP hover")
	}
	if !m.HoverOpen() {
		t.Fatal("hover popup must be open")
	}
	if col, line := m.HoverAnchor(); col != 3 || line != 0 {
		t.Fatalf("popup anchors at (%d,%d), want the hovered cell (3,0)", col, line)
	}
	if !strings.Contains(m.HoverView(), "bad package") {
		t.Fatal("popup must show the diagnostic message")
	}
}

func TestShowMouseHoverOutsideDiagnosticRange(t *testing.T) {
	m := hoverModel(t)
	m = diagAt(t, m, 0, 0, 3, "bad")
	if m.ShowMouseHover(buffer.Position{Line: 0, Col: 8}) {
		t.Fatal("no diagnostic covers col 8: no diagnostic-only popup")
	}
	if m.HoverOpen() {
		t.Fatal("no popup must be open while the LSP reply is outstanding")
	}
}

func TestMouseHoverMsgAnchorsAtPointerAndPrependsDiagnostic(t *testing.T) {
	m := hoverModel(t)
	m = diagAt(t, m, 0, 0, 7, "bad package")
	pos := buffer.Position{Line: 0, Col: 3}
	m.ShowMouseHover(pos)
	m, _ = m.Update(ilsp.HoverMsg{Path: m.path, Contents: "the docs", Mouse: true, Line: 0, Col: 3})
	view := m.HoverView()
	di, hi := strings.Index(view, "bad package"), strings.Index(view, "the docs")
	if di < 0 || hi < 0 {
		t.Fatalf("popup must show diagnostic and hover content, got %q", view)
	}
	if di > hi {
		t.Fatal("the diagnostic must precede the LSP hover content")
	}
	if col, line := m.HoverAnchor(); col != 3 || line != 0 {
		t.Fatalf("popup anchors at (%d,%d), want the hovered cell (3,0)", col, line)
	}
}

func TestMouseHoverMsgStaleReplyDropped(t *testing.T) {
	m := hoverModel(t)
	m.ShowMouseHover(buffer.Position{Line: 0, Col: 3})
	// Reply for a different cell: the pointer moved before the server answered.
	m, _ = m.Update(ilsp.HoverMsg{Path: m.path, Contents: "stale", Mouse: true, Line: 2, Col: 1})
	if m.HoverOpen() {
		t.Fatal("a reply for another cell must not open a popup")
	}
	// Reply after dismissal (any key) is dropped too.
	m.ShowMouseHover(buffer.Position{Line: 2, Col: 1})
	m, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m, _ = m.Update(ilsp.HoverMsg{Path: m.path, Contents: "late", Mouse: true, Line: 2, Col: 1})
	if m.HoverOpen() {
		t.Fatal("a reply landing after a key dismissed the hover must be dropped")
	}
}

func TestKeyTriggeredHoverStillAnchorsAtCursor(t *testing.T) {
	m := hoverModel(t)
	m.SetCursor(2, 5)
	m, _ = m.Update(ilsp.HoverMsg{Path: m.path, Contents: "cursor docs"})
	if !m.HoverOpen() {
		t.Fatal("the key-triggered hover flow must still open")
	}
	if col, line := m.HoverAnchor(); col != 5 || line != 2 {
		t.Fatalf("key-triggered popup anchors at (%d,%d), want the cursor (5,2)", col, line)
	}
}

func TestDismissMouseHoverLeavesCursorPopupAlone(t *testing.T) {
	m := hoverModel(t)
	// A key-triggered (cursor-anchored) popup survives pointer motion.
	m, _ = m.Update(ilsp.HoverMsg{Path: m.path, Contents: "cursor docs"})
	m.DismissMouseHover()
	if !m.HoverOpen() {
		t.Fatal("motion must not dismiss a key-triggered hover popup")
	}
	// A mouse-anchored popup goes when the pointer leaves the cell.
	m = diagAt(t, m, 0, 0, 7, "bad")
	m.ShowMouseHover(buffer.Position{Line: 0, Col: 1})
	m.DismissMouseHover()
	if m.HoverOpen() {
		t.Fatal("motion off the cell must dismiss the mouse-anchored popup")
	}
	if m.mouseHover != nil {
		t.Fatal("the pending mouse-hover position must be cleared")
	}
}
