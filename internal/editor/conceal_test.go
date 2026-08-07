package editor

import (
	"strings"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/editor/mode"
	"ike/internal/highlight"
)

// conceal_test.go covers the conceal-with-stand-in generalisation (#1585):
// a span carrying Replace renders its stand-in instead of the source runes on
// lines the caret is not on, while cursor line, selection and the toggle show
// raw source. The spans are synthetic — the mechanism is language-agnostic;
// the .http producer feeding it is tested in plugins/languages/http.

// replaceSpan is a "%20"-style conceal: cols [2,5) of line 0 stand in as " ".
func replaceSpan(line int) []highlight.Span {
	return []highlight.Span{
		{Line: line, StartCol: 2, EndCol: 5, Capture: "escape", Replace: " "},
	}
}

// TestConcealReplaceRendersStandIn: off the cursor line the encoded sequence
// displays as its decoded character; the span still styles the raw source.
func TestConcealReplaceRendersStandIn(t *testing.T) {
	m, path := mdLoaded(t, "a %20 b\nplain\n")
	m.cursor = buffer.Position{Line: 1}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: replaceSpan(0)})
	m = mm

	view := plainView(m)
	if strings.Contains(view, "%20") {
		t.Error("encoded sequence still visible off the cursor line")
	}
	if !strings.Contains(view, "a   b") {
		t.Errorf("stand-in not rendered, view:\n%s", view)
	}
	// The Replace span keeps styling the raw source too.
	if got := m.hlIndex.CaptureAt(0, 2); got != "escape" {
		t.Errorf("style capture at raw range = %q, want escape", got)
	}
}

// TestConcealReplaceCaretPositionRaw (#1594): the encoded source shows only
// while the caret sits inside the range — elsewhere on the same line the
// stand-in stays.
func TestConcealReplaceCaretPositionRaw(t *testing.T) {
	m, path := mdLoaded(t, "a %20 b\nplain\n")
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: replaceSpan(0)})
	m = mm
	// Caret at col 0 — outside the [2,5) range: still concealed.
	if view := plainView(m); strings.Contains(view, "%20") {
		t.Error("caret outside the range must keep the stand-in")
	}
	// Caret inside the range: raw encoded source.
	m.cursor = buffer.Position{Line: 0, Col: 3}
	if view := plainView(m); !strings.Contains(view, "%20") {
		t.Error("caret inside the range must show the raw encoded sequence")
	}
}

// TestConcealAdjacentCaretKeepsStandIn (#1686): the one-column widening is for
// the value families only — a generic stand-in conceal keeps the strict
// inside-only rule of #1594, so the caret next to it changes nothing.
func TestConcealAdjacentCaretKeepsStandIn(t *testing.T) {
	m, path := mdLoaded(t, "a %20 b\nplain\n")
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: replaceSpan(0)})
	m = mm
	for _, col := range []int{1, 5} {
		m.cursor = buffer.Position{Line: 0, Col: col}
		if view := plainView(m); strings.Contains(view, "%20") {
			t.Errorf("caret at col %d is outside the range — the stand-in must stay", col)
		}
	}
}

// TestConcealSelectionRevealsRaw: a visual selection touching the line shows
// raw source — the "selected for inspection" half of #1585.
func TestConcealSelectionRevealsRaw(t *testing.T) {
	m, path := mdLoaded(t, "a %20 b\nplain\n")
	m.cursor = buffer.Position{Line: 1}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: replaceSpan(0)})
	m = mm
	m.mode = mode.Visual
	m.anchor = buffer.Position{Line: 0, Col: 0}
	if view := plainView(m); !strings.Contains(view, "%20") {
		t.Error("selection over the line must reveal the raw source")
	}
}

// TestConcealReplaceMultiByte: a multi-sequence encoding ("%C3%A4" → "ä")
// conceals all six source columns behind one stand-in glyph, and the click
// map lands inside the range on its start.
func TestConcealReplaceMultiByte(t *testing.T) {
	m, path := mdLoaded(t, "x%C3%A4y\nplain\n")
	m.cursor = buffer.Position{Line: 1}
	spans := []highlight.Span{{Line: 0, StartCol: 1, EndCol: 7, Capture: "escape", Replace: "ä"}}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: spans})
	m = mm

	if view := plainView(m); !strings.Contains(view, "xäy") {
		t.Errorf("multi-byte stand-in not rendered, view:\n%s", view)
	}
	// Display "xäy": offset 0 → col 0, offset 1 (the stand-in) → range start
	// col 1, offset 2 → col 7 (the y).
	for _, tc := range []struct{ offset, want int }{
		{0, 0}, {1, 1}, {2, 7},
	} {
		if got := m.displayClickCol(0, 0, tc.offset); got != tc.want {
			t.Errorf("offset %d → col %d, want %d", tc.offset, got, tc.want)
		}
	}
}

// TestConcealReplaceDisplayOffset: overlay anchoring accounts for the
// stand-in's collapsed width (#1585 — DisplayOffset now walks conceal).
func TestConcealReplaceDisplayOffset(t *testing.T) {
	m, path := mdLoaded(t, "x%C3%A4y\nplain\n")
	m.cursor = buffer.Position{Line: 1}
	spans := []highlight.Span{{Line: 0, StartCol: 1, EndCol: 7, Capture: "escape", Replace: "ä"}}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: spans})
	m = mm

	if got := m.DisplayOffset(0, 7); got != 2 {
		t.Errorf("DisplayOffset(0, 7) = %d, want 2 (x + stand-in)", got)
	}
	// Plain conceal (#881) collapses to nothing.
	if got := m.DisplayOffset(0, 1); got != 1 {
		t.Errorf("DisplayOffset(0, 1) = %d, want 1", got)
	}
}
