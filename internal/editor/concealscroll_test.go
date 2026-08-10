package editor

import (
	"strings"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	"ike/internal/secret"
)

// concealscroll_test.go covers horizontal cursor-following on lines whose
// conceal stand-ins render at a width of their own (#1752): view.Left is a
// rune column, the window it opens is display cells, so the follow decision
// has to be made in the expansion.

// standInScroll loads a long single line with cols [6,12) replaced by repl
// (the secret-masking family, #1623 — masking is on by default) and parks the
// caret off that line so the stand-in applies.
func standInScroll(t *testing.T, repl string) Model {
	t.Helper()
	line := "TOKEN=abc123" + strings.Repeat("z", 200)
	m, path := mdLoaded(t, line+"\nx\n")
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: []highlight.Span{
		{Line: 0, StartCol: 6, EndCol: 12, Capture: secret.Capture, Replace: repl},
	}})
	return mm
}

// displayCol is the assertion-side mirror of the expansion: the display cell
// of a buffer column on the caret's line.
func displayCol(m Model, col int) int {
	return concealDisplayColAt(m.concealPrefix(m.cursor.Line), col)
}

// TestConcealScrollWideStandInFollowsRight: a mask wider than the value pushes
// the columns behind it right, so the caret leaves the window at a rune column
// the raw comparison still calls visible — the viewport must scroll.
func TestConcealScrollWideStandInFollowsRight(t *testing.T) {
	const wide = 40 // vs. 6 columns of source
	m := standInScroll(t, strings.Repeat("#", wide))
	tw := m.view.TextWidth(m.buf.LineCount())

	// A rune column the unconverted check would keep on screen (col < tw),
	// but whose display cell sits past the right edge.
	col := tw - 5
	if displayCol(m, col) <= tw-1 {
		t.Fatalf("test setup: display col %d of col %d must exceed the window (%d cells)", displayCol(m, col), col, tw)
	}
	m.cursor = buffer.Position{Line: 0, Col: col}
	m.scroll()

	if m.view.Left == 0 {
		t.Fatalf("no horizontal scroll: caret display col %d does not fit %d cells", displayCol(m, col), tw)
	}
	// The offset lands on or before the right edge — an offset falling inside
	// the stand-in snaps past it (none of it renders from there), so the exact
	// edge is only guaranteed when the window start clears the range.
	if got := displayCol(m, col) - displayCol(m, m.view.Left); got > tw-1 {
		t.Errorf("caret display offset = %d, still past the right edge %d (Left=%d)", got, tw-1, m.view.Left)
	}
}

// TestConcealScrollWideStandInRightEdgeExact: with the window start well past
// the stand-in the offset is the plain minimum — the caret sits on the last
// display cell.
func TestConcealScrollWideStandInRightEdgeExact(t *testing.T) {
	m := standInScroll(t, strings.Repeat("#", 40))
	tw := m.view.TextWidth(m.buf.LineCount())

	col := 150
	m.cursor = buffer.Position{Line: 0, Col: col}
	m.scroll()

	if m.view.Left <= 12 {
		t.Fatalf("Left = %d, want the window start past the stand-in", m.view.Left)
	}
	if got := displayCol(m, col) - displayCol(m, m.view.Left); got != tw-1 {
		t.Errorf("caret display offset = %d, want the right edge %d (Left=%d)", got, tw-1, m.view.Left)
	}
}

// TestConcealScrollWideStandInFollowsBackLeft: moving back onto the key
// scrolls the window home again.
func TestConcealScrollWideStandInFollowsBackLeft(t *testing.T) {
	m := standInScroll(t, strings.Repeat("#", 40))
	tw := m.view.TextWidth(m.buf.LineCount())

	m.cursor = buffer.Position{Line: 0, Col: tw - 5}
	m.scroll()
	if m.view.Left == 0 {
		t.Fatal("test setup: the caret must have scrolled the window first")
	}

	m.cursor = buffer.Position{Line: 0, Col: 0}
	m.scroll()
	if m.view.Left != 0 {
		t.Errorf("Left = %d, want 0 after moving back to the line start", m.view.Left)
	}
}

// TestConcealScrollNarrowStandInNoOverScroll: a mask narrower than the value
// pulls the columns behind it left, so a caret past the raw text width is
// still on screen — no scroll.
func TestConcealScrollNarrowStandInNoOverScroll(t *testing.T) {
	m := standInScroll(t, "*") // 1 cell for 6 columns of source
	tw := m.view.TextWidth(m.buf.LineCount())

	col := tw + 3 // raw check would scroll; the expansion keeps it visible
	if displayCol(m, col) > tw-1 {
		t.Fatalf("test setup: display col %d of col %d must fit the window (%d cells)", displayCol(m, col), col, tw)
	}
	m.cursor = buffer.Position{Line: 0, Col: col}
	m.scroll()

	if m.view.Left != 0 {
		t.Errorf("Left = %d, want 0 — the caret is inside the window in display cells", m.view.Left)
	}
}

// TestConcealScrollWithoutRangesUnchanged: a line with no conceal ranges keeps
// the raw-column result view.Scroll derived.
func TestConcealScrollWithoutRangesUnchanged(t *testing.T) {
	m, _ := mdLoaded(t, strings.Repeat("z", 200)+"\n")
	tw := m.view.TextWidth(m.buf.LineCount())

	col := tw + 3
	m.cursor = buffer.Position{Line: 0, Col: col}
	m.scroll()

	if want := col - tw + 1; m.view.Left != want {
		t.Errorf("Left = %d, want %d (raw rune columns)", m.view.Left, want)
	}
}
