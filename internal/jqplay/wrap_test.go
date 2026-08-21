package jqplay

import (
	"strings"
	"testing"
)

// rows renders the wrapped program as the strings the view would show, which
// is what the assertions below are really about.
func rows(program string, width int) []string {
	r := []rune(program)
	lines := Wrap(program, width)
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, string(r[l.Start:l.End]))
	}
	return out
}

// TestWrapBreaksAtPipes (#2032): a pipeline too wide for one row breaks at its
// stage boundaries — every row ends on a pipe and starts on the next stage,
// with the separating blank dropped.
func TestWrapBreaksAtPipes(t *testing.T) {
	program := `.hits.hits[]._source | .keyword as $keyword | .ser[] | select(.domain == "x.com")`
	got := rows(program, 40)
	want := []string{
		".hits.hits[]._source |",
		".keyword as $keyword | .ser[] |",
		`select(.domain == "x.com")`,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rows %q, want %d %q", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestWrapCoversTheWholeProgram: every rune except the dropped row-leading
// blanks survives the wrap, in order — the expanded view must show the program
// it was given, not a summary of it.
func TestWrapCoversTheWholeProgram(t *testing.T) {
	program := `.a | map(select(.n > 3)) | .[] | {x: .n, y: "a b c"} | add`
	for _, width := range []int{1, 5, 12, 40, 500} {
		// The rows join back to the program up to the blanks a stage break
		// drops, so comparing without spaces is the exact statement of
		// "nothing was lost and nothing was reordered".
		joined := strings.ReplaceAll(strings.Join(rows(program, width), ""), " ", "")
		if want := strings.ReplaceAll(program, " ", ""); joined != want {
			t.Errorf("width %d: rows join to %q, want %q", width, joined, want)
		}
		for i, l := range Wrap(program, width) {
			if l.End-l.Start > width {
				t.Errorf("width %d: row %d is %d runes wide", width, i, l.End-l.Start)
			}
		}
	}
}

// TestWrapHardSplitsALongStage: a stage wider than the row has no pipe to
// break on and is cut at the width instead of overflowing it.
func TestWrapHardSplitsALongStage(t *testing.T) {
	got := rows(strings.Repeat("a", 25), 10)
	want := []string{"aaaaaaaaaa", "aaaaaaaaaa", "aaaaa"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestWrapIgnoresPipesInLiterals: a `|` inside a string or a comment is text,
// and `||` is the or-operator — neither is a stage boundary.
func TestWrapIgnoresPipesInLiterals(t *testing.T) {
	if got := rows(`select(.a == "x|y" or .b) # a|b`, 80); len(got) != 1 {
		t.Errorf("a pipe in a literal must not break the row: %q", got)
	}
	if got := rows(`.a || .b`, 80); len(got) != 1 {
		t.Errorf("`||` must not break the row: %q", got)
	}
}

// TestWrapEmptyProgram: the empty program is one empty row, so the caller may
// index the first row unconditionally.
func TestWrapEmptyProgram(t *testing.T) {
	if got := Wrap("", 20); len(got) != 1 || got[0] != (Line{0, 0}) {
		t.Fatalf("Wrap(\"\") = %+v, want one empty line", got)
	}
}

// TestLineAt resolves the cursor's row: inside a row, on a row boundary (the
// position after a pipe belongs to the row that follows) and past the end.
func TestLineAt(t *testing.T) {
	program := ".aaaa | .bbbb | .cccc"
	lines := Wrap(program, 8)
	if len(lines) != 3 {
		t.Fatalf("setup: got %d rows, want 3: %+v", len(lines), lines)
	}
	cases := map[int]int{0: 0, 6: 0, 7: 1, 14: 1, 15: 2, len([]rune(program)): 2, 999: 2}
	for pos, want := range cases {
		if got := LineAt(lines, pos); got != want {
			t.Errorf("LineAt(%d) = %d, want %d", pos, got, want)
		}
	}
}
