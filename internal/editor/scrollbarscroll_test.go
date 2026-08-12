package editor

import (
	"strings"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	"ike/internal/secret"
)

// scrollbarscroll_test.go covers the column the overlaid vertical scrollbar
// claims (#1827): horizontal cursor-following must stop one column short of it
// while the bar is visible, or the caret hides behind the bar.

// tallLine loads a long first line followed by enough short lines to overflow
// the viewport, so the scrollbar renders over the pane's last column.
func tallLine(t *testing.T, first string) Model {
	t.Helper()
	m, _ := mdLoaded(t, first+"\n"+strings.Repeat("x\n", 40))
	if _, _, _, _, ok := m.scrollbarGeometry(); !ok {
		t.Fatal("test setup: the scrollbar must be visible")
	}
	return m
}

// TestScrollReservesScrollbarColumn: with the bar visible the caret stops one
// column left of the text edge, the viewport scrolling one column further.
func TestScrollReservesScrollbarColumn(t *testing.T) {
	m := tallLine(t, strings.Repeat("z", 200))
	tw := m.view.TextWidth(m.buf.LineCount())

	col := tw + 3
	m.cursor = buffer.Position{Line: 0, Col: col}
	m.scroll()

	if want := col - tw + 2; m.view.Left != want {
		t.Errorf("Left = %d, want %d (one column more than without a bar)", m.view.Left, want)
	}
	if got := col - m.view.Left; got != tw-2 {
		t.Errorf("caret column offset = %d, want %d — left of the scrollbar column", got, tw-2)
	}
}

// TestScrollWithoutScrollbarUsesLastColumn: a buffer that fits vertically draws
// no bar, so the full last column stays usable.
func TestScrollWithoutScrollbarUsesLastColumn(t *testing.T) {
	m, _ := mdLoaded(t, strings.Repeat("z", 200)+"\n")
	if _, _, _, _, ok := m.scrollbarGeometry(); ok {
		t.Fatal("test setup: no scrollbar expected on a buffer that fits")
	}
	tw := m.view.TextWidth(m.buf.LineCount())

	col := tw + 3
	m.cursor = buffer.Position{Line: 0, Col: col}
	m.scroll()

	if want := col - tw + 1; m.view.Left != want {
		t.Errorf("Left = %d, want %d (full text width)", m.view.Left, want)
	}
}

// standInTall is standInScroll (#1752) on a buffer tall enough to show the
// scrollbar: cols [6,12) of the long line carry the stand-in repl.
func standInTall(t *testing.T, repl string) Model {
	t.Helper()
	m := tallLine(t, "TOKEN=abc123"+strings.Repeat("z", 200))
	mm, _ := m.Update(highlight.SpansMsg{Path: m.path, Version: m.docVersion, Spans: []highlight.Span{
		{Line: 0, StartCol: 6, EndCol: 12, Capture: secret.Capture, Replace: repl},
	}})
	return mm
}

// TestConcealScrollReservesScrollbarColumn: the conceal path (#1752) measures
// against the same reduced width — the caret's display cell stays left of the
// bar, for masks wider and narrower than their source.
func TestConcealScrollReservesScrollbarColumn(t *testing.T) {
	for _, tc := range []struct {
		name string
		repl string
	}{
		{"wide mask", strings.Repeat("#", 40)},
		{"narrow mask", "*"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := standInTall(t, tc.repl)
			tw := m.view.TextWidth(m.buf.LineCount())

			col := 150
			m.cursor = buffer.Position{Line: 0, Col: col}
			m.scroll()

			if m.view.Left <= 12 {
				t.Fatalf("Left = %d, want the window start past the stand-in", m.view.Left)
			}
			if got := displayCol(m, col) - displayCol(m, m.view.Left); got != tw-2 {
				t.Errorf("caret display offset = %d, want %d — left of the scrollbar column (Left=%d)",
					got, tw-2, m.view.Left)
			}
		})
	}
}

// TestWrapSegsReservesScrollbarColumn: soft-wrapped rows break one column
// earlier while the bar is visible, so no wrapped segment — and no caret on its
// last cell — lands under it.
func TestWrapSegsReservesScrollbarColumn(t *testing.T) {
	m := tallLine(t, strings.Repeat("z", 200))
	tw := m.view.TextWidth(m.buf.LineCount())
	m.softWrap = true

	segs := m.wrapSegs(0)
	if len(segs) < 2 {
		t.Fatalf("test setup: expected a wrapped line, got %d segments", len(segs))
	}
	if segs[1] != tw-1 {
		t.Errorf("second segment starts at %d, want %d (wrap width minus the scrollbar column)", segs[1], tw-1)
	}
}
