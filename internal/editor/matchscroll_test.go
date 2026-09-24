package editor

// Tests for match-aware horizontal scrolling on search landings (#2732): a
// match right of the window is revealed whole, with matchScrollMargin columns
// of context, instead of parking its first character in the last column.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// longLine builds a line of filler with word placed at col, padded to n runes.
func longLine(word string, col, n int) string {
	return strings.Repeat("a", col) + word + strings.Repeat("b", n-col-len(word))
}

// assertRevealed checks the landing on [s, e): Left puts e+margin on the
// rightmost cell of the caret window.
func assertRevealed(t *testing.T, m Model, s, e int) {
	t.Helper()
	tw := m.scrollTextWidth()
	if m.cursor.Col != s {
		t.Fatalf("cursor col = %d want %d", m.cursor.Col, s)
	}
	if want := e + matchScrollMargin - tw; m.view.Left != want {
		t.Fatalf("Left = %d want %d (match [%d,%d), window %d)", m.view.Left, want, s, e, tw)
	}
}

func TestMatchScrollCommitRevealsWholeMatch(t *testing.T) {
	m, _ := loaded(t, longLine("foobar", 150, 300)+"\n")
	m = send(m, key('/'))
	m = typeKeys(m, "foobar")
	m = send(m, special(tea.KeyEnter))
	assertRevealed(t, m, 150, 156)
}

func TestMatchScrollLivePreviewRevealsWholeMatch(t *testing.T) {
	m, _ := loaded(t, longLine("foobar", 150, 300)+"\n")
	m = send(m, key('/'))
	m = typeKeys(m, "foobar")
	assertRevealed(t, m, 150, 156)
}

func TestMatchScrollStepPreviewRevealsWholeMatch(t *testing.T) {
	line := longLine("foobar", 10, 300)
	line = line[:200] + "foobar" + line[206:]
	m, _ := loaded(t, line+"\n")
	m = send(m, key('/'))
	m = typeKeys(m, "foobar")
	m.StepSearchPreview(false)
	assertRevealed(t, m, 200, 206)
}

func TestMatchScrollNextAndPrevRevealWholeMatch(t *testing.T) {
	line := longLine("foobar", 150, 400)
	line = line[:300] + "foobar" + line[306:]
	m, _ := loaded(t, line+"\n")
	m = send(m, key('/'))
	m = typeKeys(m, "foobar")
	m = send(m, special(tea.KeyEnter))
	m = send(m, key('n'))
	assertRevealed(t, m, 300, 306)

	// N back to the first match scrolls left: the start becomes visible.
	m = send(m, key('N'))
	if m.cursor.Col != 150 || m.view.Left != 150 {
		t.Fatalf("N: cursor col = %d Left = %d want 150/150", m.cursor.Col, m.view.Left)
	}
	// From the far left, N wraps to the right-hand match again.
	m.view.Left = 0
	m.cursor.Col = 0
	m = send(m, key('N'))
	assertRevealed(t, m, 300, 306)
}

func TestMatchScrollStarRevealsWholeMatch(t *testing.T) {
	line := "foobar " + strings.Repeat("a", 200) + " foobar " + strings.Repeat("b", 100)
	m, _ := loaded(t, line+"\n")
	m = send(m, key('*'))
	assertRevealed(t, m, 208, 214)
}

func TestMatchScrollWiderThanWindowStartsAtLeftEdge(t *testing.T) {
	m, _ := loaded(t, "x\n")
	tw := m.scrollTextWidth()
	word := strings.Repeat("q", tw+10)
	m, _ = loaded(t, longLine(word, 200, 400)+"\n")
	m = send(m, key('/'))
	m = typeKeys(m, word)
	m = send(m, special(tea.KeyEnter))
	if m.cursor.Col != 200 || m.view.Left != 200 {
		t.Fatalf("cursor col = %d Left = %d want 200/200", m.cursor.Col, m.view.Left)
	}
}

func TestMatchScrollAlreadyVisibleKeepsLeft(t *testing.T) {
	line := longLine("foobar", 150, 400)
	line = line[:170] + "foobar" + line[176:]
	m, _ := loaded(t, line+"\n")
	m = send(m, key('/'))
	m = typeKeys(m, "foobar")
	m = send(m, special(tea.KeyEnter))
	// Frame both matches, then step to the second: it is fully visible.
	m.view.Left = 140
	m = send(m, key('n'))
	if m.cursor.Col != 170 || m.view.Left != 140 {
		t.Fatalf("cursor col = %d Left = %d want 170/140", m.cursor.Col, m.view.Left)
	}
}

func TestMatchScrollMarginCutAtLineEnd(t *testing.T) {
	m, _ := loaded(t, longLine("foobar", 150, 158)+"\n")
	m = send(m, key('/'))
	m = typeKeys(m, "foobar")
	m = send(m, special(tea.KeyEnter))
	if want := 158 - m.scrollTextWidth(); m.view.Left != want {
		t.Fatalf("Left = %d want %d (margin cut at the line end)", m.view.Left, want)
	}
}

func TestMatchScrollKeepsMatchLeftOfScrollbar(t *testing.T) {
	var b strings.Builder
	b.WriteString(longLine("foobar", 150, 300) + "\n")
	for i := 0; i < 100; i++ {
		b.WriteString("filler\n")
	}
	m, _ := loaded(t, b.String())
	if m.scrollTextWidth() >= m.view.TextWidth(m.buf.LineCount()) {
		t.Fatal("test setup: the scrollbar must claim a column")
	}
	m = send(m, key('/'))
	m = typeKeys(m, "foobar")
	m = send(m, special(tea.KeyEnter))
	assertRevealed(t, m, 150, 156)
}

// Ordinary motion keeps the plain caret follow: no margin, no match reveal.
func TestMatchScrollOrdinaryMotionUnchanged(t *testing.T) {
	m, _ := loaded(t, longLine("foobar", 150, 300)+"\n")
	m = typeKeys(m, "150l")
	if want := 150 - m.scrollTextWidth() + 1; m.view.Left != want {
		t.Fatalf("Left = %d want %d (caret on the last column)", m.view.Left, want)
	}
}

func TestMatchLeft(t *testing.T) {
	cases := []struct {
		name                    string
		left, s, e, lineEnd, tw int
		want                    int
	}{
		{"visible", 0, 10, 16, 300, 50, 0},
		{"fits: margin", 0, 100, 106, 300, 50, 61},
		{"end off-screen", 0, 45, 51, 300, 50, 6},
		{"wider than window", 0, 100, 200, 300, 50, 100},
		{"margin cut at line end", 0, 100, 106, 108, 50, 58},
		{"margin pushes past window", 0, 100, 148, 300, 50, 100},
	}
	for _, c := range cases {
		if got := matchLeft(c.left, c.s, c.e, c.lineEnd, c.tw); got != c.want {
			t.Errorf("%s: matchLeft = %d want %d", c.name, got, c.want)
		}
	}
}
