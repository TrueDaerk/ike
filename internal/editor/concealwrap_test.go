package editor

import (
	"reflect"
	"strings"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/editor/viewport"
	"ike/internal/highlight"
	"ike/internal/secret"
)

// concealwrap_test.go covers soft wrap on lines whose conceal stand-ins render
// at a width of their own (#1756): wrap segments must break on the display
// cells of the concealed expansion, not the raw rune columns.

// standInWrapped loads a long single line with cols [6,12) replaced by repl
// (the secret-masking family, #1623 — masking is on by default), turns soft
// wrap on and parks the caret at the line start so the stand-in applies.
func standInWrapped(t *testing.T, repl string) Model {
	t.Helper()
	line := "TOKEN=abc123" + strings.Repeat("z", 200)
	m, path := mdLoaded(t, line+"\nx\n")
	m.softWrap = true
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: []highlight.Span{
		{Line: 0, StartCol: 6, EndCol: 12, Capture: secret.Capture, Replace: repl},
	}})
	return mm
}

// segDisplayWidth measures segment si of line in display cells through the
// conceal expansion — the assertion-side mirror of what the row renders.
func segDisplayWidth(m Model, line int, segs []int, si int) int {
	prefix := m.concealPrefix(line)
	end := viewport.SegmentEnd(segs, si, len([]rune(m.buf.Line(line))))
	return concealDisplayColAt(prefix, end) - concealDisplayColAt(prefix, segs[si])
}

// TestConcealWrapWideStandInRowsFitWidth: a mask wider than the value pushes
// display cells past the raw column count — every visual row must still fit
// the text width.
func TestConcealWrapWideStandInRowsFitWidth(t *testing.T) {
	const wide = 40 // vs. 6 columns of source
	m := standInWrapped(t, strings.Repeat("#", wide))
	tw := m.view.TextWidth(m.buf.LineCount())

	segs := m.wrapSegs(0)
	raw := viewport.WrapSegments([]rune(m.buf.Line(0)), tw, m.tabWidth)
	if reflect.DeepEqual(segs, raw) {
		t.Fatalf("segments %v ignore the stand-in (raw wrap %v)", segs, raw)
	}
	for si := range segs {
		if w := segDisplayWidth(m, 0, segs, si); w > tw {
			t.Errorf("segment %d spans %d display cells, exceeds text width %d (segs=%v)", si, w, tw, segs)
		}
	}
}

// TestConcealWrapNarrowStandInNoEarlyBreak: a mask narrower than the value
// pulls the cells behind it left — rows must fill up to the text width instead
// of breaking at the raw column count.
func TestConcealWrapNarrowStandInNoEarlyBreak(t *testing.T) {
	m := standInWrapped(t, "*") // 1 cell for 6 columns of source
	tw := m.view.TextWidth(m.buf.LineCount())

	segs := m.wrapSegs(0)
	// Every row but the last must use the full budget: breaking earlier than
	// the display width allows is exactly the raw-column distortion.
	for si := 0; si < len(segs)-1; si++ {
		if w := segDisplayWidth(m, 0, segs, si); w != tw {
			t.Errorf("segment %d spans %d display cells, want the full width %d (segs=%v)", si, w, tw, segs)
		}
	}
	// The first break lands past the raw one: the 5 cells the mask saves buy
	// the first row 5 more columns.
	raw := viewport.WrapSegments([]rune(m.buf.Line(0)), tw, m.tabWidth)
	if segs[1] <= raw[1] {
		t.Errorf("first break at col %d, want past the raw break %d (early break)", segs[1], raw[1])
	}
}

// TestConcealWrapVerticalFollowsSegments: gj steps one visual row per press
// through the conceal-aware segments, and the wrapped scroll keeps the caret's
// segment inside the window.
func TestConcealWrapVerticalFollowsSegments(t *testing.T) {
	m := standInWrapped(t, strings.Repeat("#", 40))

	segs := m.wrapSegs(0)
	if len(segs) < 3 {
		t.Fatalf("test setup: line must wrap to at least 3 rows, got %v", segs)
	}
	r := m.wrapVertical(1, 1)
	if r.Pos.Line != 0 || viewport.SegmentIndex(segs, r.Pos.Col) != 1 {
		t.Errorf("gj landed at %v, want segment 1 of line 0 (segs=%v)", r.Pos, segs)
	}
	m.cursor = buffer.Position{Line: 0, Col: r.Pos.Col}
	r = m.wrapVertical(1, -1)
	if r.Pos.Line != 0 || viewport.SegmentIndex(segs, r.Pos.Col) != 0 {
		t.Errorf("gk landed at %v, want segment 0 of line 0 (segs=%v)", r.Pos, segs)
	}

	// The wrapped scroll follows the caret's visual row through the tall line:
	// on the last segment of a line taller than the window, Top pins to it.
	m.view.SetSize(60, 3)
	last := len(segs) - 1
	m.cursor = buffer.Position{Line: 0, Col: segs[last]}
	m.scroll()
	if m.view.Top != 0 {
		t.Errorf("Top = %d, want 0 (tall wrapped line pins the viewport)", m.view.Top)
	}
	if m.view.Left != 0 {
		t.Errorf("Left = %d; wrap must pin horizontal scroll at 0", m.view.Left)
	}
}

// TestConcealWrapWithoutRangesUnchanged: a line with no conceal ranges wraps
// on raw rune columns exactly as before.
func TestConcealWrapWithoutRangesUnchanged(t *testing.T) {
	m, _ := mdLoaded(t, strings.Repeat("z", 200)+"\n")
	m.softWrap = true
	tw := m.view.TextWidth(m.buf.LineCount())

	want := viewport.WrapSegments([]rune(m.buf.Line(0)), tw, m.tabWidth)
	if got := m.wrapSegs(0); !reflect.DeepEqual(got, want) {
		t.Errorf("segments = %v, want the raw wrap %v", got, want)
	}
}
