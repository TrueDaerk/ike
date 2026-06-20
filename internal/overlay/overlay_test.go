package overlay

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// baseCanvas builds a w×h canvas filled with the rune r.
func baseCanvas(r byte, w, h int) string {
	row := strings.Repeat(string(r), w)
	rows := make([]string, h)
	for i := range rows {
		rows[i] = row
	}
	return strings.Join(rows, "\n")
}

func TestCenterPlacesBoxInTheMiddle(t *testing.T) {
	base := baseCanvas('.', 20, 10)
	top := "AAA\nAAA"
	out := Center(base, top, 20, 10)
	lines := strings.Split(out, "\n")

	if len(lines) != 10 {
		t.Fatalf("height = %d, want 10", len(lines))
	}
	// Every row keeps the full canvas width — the box is spliced, not appended.
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 20 {
			t.Fatalf("row %d width = %d, want 20", i, w)
		}
	}
	// topW=3, topH=2 -> x=(20-3)/2=8, y=(10-2)/2=4. Rows 4 and 5 carry "AAA"
	// starting at column 8; the base dots survive on both sides.
	for _, row := range []int{4, 5} {
		if !strings.Contains(lines[row], "AAA") {
			t.Fatalf("row %d missing box content: %q", row, lines[row])
		}
		if !strings.HasPrefix(lines[row], "........") {
			t.Fatalf("row %d should keep left base content: %q", row, lines[row])
		}
	}
	// Rows outside the box are untouched base rows.
	if lines[0] != "...................." {
		t.Fatalf("row 0 should be pristine base: %q", lines[0])
	}
}

func TestCenterReturnsBaseWhenBoxTooLarge(t *testing.T) {
	base := baseCanvas('.', 10, 4)
	if got := Center(base, "WWWWWWWWWWWW", 10, 4); got != base {
		t.Fatal("box wider than canvas should leave base untouched")
	}
	tall := strings.Repeat("x\n", 6)
	if got := Center(base, tall, 10, 4); got != base {
		t.Fatal("box taller than canvas should leave base untouched")
	}
}

func TestCenterEmptyTopIsNoOp(t *testing.T) {
	base := baseCanvas('.', 8, 3)
	if got := Center(base, "", 8, 3); got != base {
		t.Fatal("empty top should return base unchanged")
	}
}

func TestCenterPreservesStyledBaseAroundBox(t *testing.T) {
	// A styled base row: the colour on both sides of the box must survive the
	// splice (the reset sequences isolate the box from the base styling).
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(strings.Repeat("x", 20))
	base := strings.Join([]string{styled, styled, styled, styled, styled}, "\n")
	out := Center(base, "BB", 20, 5)
	if !strings.Contains(out, "\x1b[") {
		t.Fatal("styled base should keep ANSI sequences after splice")
	}
	if !strings.Contains(out, "BB") {
		t.Fatal("box content should be present")
	}
	// Visual width is preserved per row despite the embedded escapes.
	for _, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w != 20 {
			t.Fatalf("row visual width = %d, want 20", w)
		}
	}
}
