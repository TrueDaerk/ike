package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// mdhang_test.go covers HangingIndent (#2105). The inputs mimic glamour
// output, which pads every line out to the width it wrapped at — the padding
// is how HangingIndent recovers that width.

// padded joins lines the way glamour emits them: right-padded to w cells.
func padded(w int, lines ...string) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l + strings.Repeat(" ", max(0, w-ansi.StringWidth(l)))
	}
	return strings.Join(out, "\n")
}

// plainLines strips styling and the trailing padding off a rendered block.
func plainLines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		out = append(out, strings.TrimRight(ansi.Strip(l), " "))
	}
	return out
}

func checkLines(t *testing.T, got string, want ...string) {
	t.Helper()
	if g := plainLines(got); strings.Join(g, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %q,\nwant %q", g, want)
	}
}

func TestHangingIndentBullet(t *testing.T) {
	// A document margin of two, the bullet, and a continuation line glamour
	// dropped back to the margin.
	in := padded(20, "  • abc def ghi jkl", "  mno pqr")
	checkLines(t, HangingIndent(in, 20), "  • abc def ghi jkl", "    mno pqr")
}

func TestHangingIndentOrdered(t *testing.T) {
	// The continuation aligns under the text, not under the number.
	in := padded(26, "  1. one long ordered item", "  that wraps")
	checkLines(t, HangingIndent(in, 26), "  1. one long ordered item", "     that wraps")
}

func TestHangingIndentWideEnumeration(t *testing.T) {
	// A two-digit number hangs one column further.
	in := padded(26, "  10. one long ordered item", "  that wraps")
	checkLines(t, HangingIndent(in, 26), "  10. one long ordered", "      item that wraps")
}

func TestHangingIndentNested(t *testing.T) {
	// A nested item hangs off its own level, and the parent above it is not
	// folded into it.
	in := padded(28, "  • parent", "    • nested item that wraps", "    over here")
	checkLines(t, HangingIndent(in, 28),
		"  • parent", "    • nested item that wraps", "      over here")
}

func TestHangingIndentTask(t *testing.T) {
	in := padded(28, "  [ ] a task item that wraps", "  around")
	checkLines(t, HangingIndent(in, 28), "  [ ] a task item that wraps", "      around")
}

func TestHangingIndentLeavesNonListLines(t *testing.T) {
	in := padded(26, "  a paragraph that wraps", "  onto a second line")
	if got := HangingIndent(in, 26); got != in {
		t.Errorf("got %q, want it unchanged", got)
	}
}

func TestHangingIndentKeepsSeparateBlock(t *testing.T) {
	// "ok" would have fit on the item's line, so glamour did not wrap it off
	// that line: it is a block of its own (a code line at the same indent,
	// say) and must not be folded in.
	in := padded(40, "  1. short", "  ok")
	if got := HangingIndent(in, 40); got != in {
		t.Errorf("got %q, want it unchanged", got)
	}
}

func TestHangingIndentStopsAtBlankLine(t *testing.T) {
	in := padded(20, "  • abc def ghi jkl", "", "  mno pqr")
	if got := HangingIndent(in, 20); got != in {
		t.Errorf("got %q, want it unchanged", got)
	}
}

func TestHangingIndentNarrowDoesNotLoseText(t *testing.T) {
	for _, wrap := range []int{1, 2, 3, 4, 6, 10} {
		in := padded(wrap, "  • alpha beta gamma delta", "  epsilon")
		got := HangingIndent(in, wrap)
		// Words may be broken at the wrap floor, but no character is dropped
		// and no line grows past the width glamour used.
		if letters := strings.Join(strings.Fields(ansi.Strip(got)), ""); letters != "•alphabetagammadeltaepsilon" {
			t.Errorf("wrap %d: text lost: %q", wrap, got)
		}
		for _, l := range strings.Split(got, "\n") {
			if w := ansi.StringWidth(strings.TrimRight(l, " ")); w > max(wrap, 26) {
				t.Errorf("wrap %d: line %q is %d cells", wrap, l, w)
			}
		}
	}
}

func TestHangingIndentPreservesStyling(t *testing.T) {
	in := padded(20, "  \x1b[38;5;252m• \x1b[m\x1b[1mabc def ghi jkl\x1b[m", "  \x1b[1mmno pqr\x1b[m")
	got := HangingIndent(in, 20)
	if !strings.Contains(got, "\x1b[1m") {
		t.Errorf("styling dropped: %q", got)
	}
	checkLines(t, got, "  • abc def ghi jkl", "    mno pqr")
}
