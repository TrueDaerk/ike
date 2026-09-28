package editor

import (
	"strings"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
)

// foldScrollModel builds a 60-line editor with a collapsed fold 5-30 (header
// 5, body 6-30) and the cursor on its header, Top 0. The rendered rows then
// read 0,1,2,3,4,5(fold),31,32,33,… — the fold's 25 hidden lines make buffer
// distances far larger than row distances below it (#2768).
func foldScrollModel(t *testing.T, scrollOff int) Model {
	t.Helper()
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = "line" + itoa(i)
	}
	m := New()
	m.buf = buffer.FromString(strings.Join(lines, "\n"))
	m.path = "main.go"
	m.SetSize(40, 10)
	m.SetFocused(true)
	m.view.ScrollOff = scrollOff
	m.foldLines = m.buf.LineCount()
	m = feedSpans(t, m, highlight.SpansMsg{
		Path:  "main.go",
		Folds: []highlight.Fold{{HeaderLine: 5, EndLine: 30}},
	})
	m = send(m, keys("5jzc")...)
	if e, ok := m.folded[5]; !ok || e != 30 || m.cursor.Line != 5 || m.view.Top != 0 {
		t.Fatalf("setup: folded=%v cursor=%d top=%d", m.folded, m.cursor.Line, m.view.Top)
	}
	return m
}

// rowOf is the rendered row (0-based below Top) of buffer line l.
func foldRowOf(t *testing.T, m Model, l int) int {
	t.Helper()
	for r := 0; r < m.view.Height(); r++ {
		if m.displayLineAt(r) == l {
			return r
		}
	}
	t.Fatalf("line %d is not on screen (top=%d)", l, m.view.Top)
	return -1
}

func TestFoldClickBelowFoldDoesNotScroll(t *testing.T) {
	base := foldScrollModel(t, 0)
	h := base.view.Height()
	cases := []struct {
		name      string
		scrollOff int
		row       int
	}{
		{"mid-viewport", 2, 6},
		{"at the bottom margin", 2, h - 3},
		{"last row without margin", 0, h - 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := foldScrollModel(t, c.scrollOff)
			want := m.displayLineAt(c.row)
			m.MouseClick(0, c.row)
			if m.cursor.Line != want {
				t.Fatalf("click on row %d: cursor line %d, want %d", c.row, m.cursor.Line, want)
			}
			if m.view.Top != 0 {
				t.Errorf("click on visible row %d below a fold scrolled: Top=%d, want 0", c.row, m.view.Top)
			}
			if got := foldRowOf(t, m, want); got != c.row {
				t.Errorf("clicked line moved from row %d to row %d", c.row, got)
			}
		})
	}
}

func TestFoldJOffHeaderDoesNotScroll(t *testing.T) {
	m := foldScrollModel(t, 3)
	m = send(m, key('j'))
	if m.cursor.Line != 31 {
		t.Fatalf("j from the closed header: line %d, want 31", m.cursor.Line)
	}
	if m.view.Top != 0 {
		t.Errorf("j onto a visible line below the fold scrolled: Top=%d, want 0", m.view.Top)
	}
}

func TestFoldFollowScrollCountsVisibleRows(t *testing.T) {
	m := foldScrollModel(t, 2)
	h := m.view.Height()
	// The last row outside the bottom margin, then one further: exactly one
	// visible row of scrolling, keeping ScrollOff rows below the cursor.
	m.MouseClick(0, h-3)
	top := m.view.Top
	m = send(m, key('j'))
	if m.view.Top != 1 {
		t.Fatalf("j into the bottom margin: Top=%d, want 1 (from %d)", m.view.Top, top)
	}
	if r := foldRowOf(t, m, m.cursor.Line); r != h-3 {
		t.Errorf("cursor row %d, want %d (ScrollOff 2 below)", r, h-3)
	}
	// Scrolling further down counts the fold as one row: after five more
	// rows the fold header is the top row.
	m = send(m, keys("jjjj")...)
	if m.view.Top != 5 {
		t.Errorf("Top=%d, want the fold header 5", m.view.Top)
	}
	m = send(m, key('j'))
	if m.view.Top != 31 {
		t.Errorf("one row past the header: Top=%d, want 31", m.view.Top)
	}
	// Up: ScrollOff visible rows above the cursor; the fold is one of them.
	for m.cursor.Line > 32 {
		m = send(m, key('k'))
	}
	if m.view.Top != 5 {
		t.Errorf("k to line 32: Top=%d, want 5 (2 rows above: 5(fold), 31)", m.view.Top)
	}
	m = send(m, key('k'))
	if m.cursor.Line != 31 || m.view.Top != 4 {
		t.Errorf("k to line 31: cursor %d Top %d, want 31 / 4", m.cursor.Line, m.view.Top)
	}
}

func TestFoldTopNeverRestsInsideCollapsedBody(t *testing.T) {
	m := foldScrollModel(t, 0)
	m.view.Top = 15 // inside the hidden body 6-30
	m.cursor = buffer.Position{Line: 31}
	m.scroll()
	if m.view.Top != 5 {
		t.Errorf("Top=%d, want lifted onto the header 5", m.view.Top)
	}
}
