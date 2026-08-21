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

// TestRowColAndPosAt (#2038): the caret's coordinates in the wrapped program
// and the position they resolve back to — the pair a vertical motion is made
// of. The caret on a row's end stays on that row (the blank the pipe break
// dropped separates it from the next one), and a column past a short row's end
// clamps to it without being forgotten by the caller.
func TestRowColAndPosAt(t *testing.T) {
	program := ".aaaa | .bb | .cccccc"
	lines := Wrap(program, 8)
	if len(lines) != 3 {
		t.Fatalf("setup: got %d rows, want 3: %+v", len(lines), lines)
	}
	cases := []struct{ pos, row, col int }{
		{0, 0, 0},
		{7, 0, 7},                    // the caret past the first row's `|`
		{8, 1, 0},                    // the next row's first cell
		{13, 1, 5},                   // that row's own end
		{14, 2, 0},                   // and on to the last
		{len([]rune(program)), 2, 7}, // the end of the program
	}
	for _, c := range cases {
		row, col := RowCol(lines, c.pos)
		if row != c.row || col != c.col {
			t.Errorf("RowCol(%d) = (%d,%d), want (%d,%d)", c.pos, row, col, c.row, c.col)
		}
		if got := PosAt(lines, row, col); got != c.pos {
			t.Errorf("PosAt(%d,%d) = %d, want %d", row, col, got, c.pos)
		}
	}
	// A goal column past a row's end clamps into the row, and the column
	// itself is the caller's to keep — stepping on lands back where it was.
	if got, want := PosAt(lines, 1, 99), lines[1].End; got != want {
		t.Errorf("PosAt past the row's end = %d, want %d", got, want)
	}
	// Out-of-range rows resolve into the program rather than panicking.
	if got := PosAt(lines, -3, 2); got != lines[0].Start+2 {
		t.Errorf("PosAt on a negative row = %d, want the first row's cell", got)
	}
	if got := PosAt(lines, 99, 0); got != lines[2].Start {
		t.Errorf("PosAt past the last row = %d, want the last row's start", got)
	}
	if got := PosAt(nil, 0, 0); got != 0 {
		t.Errorf("PosAt with no rows = %d, want 0", got)
	}
}

// TestRowColHardSplitTouchesTheNextRow (#2038): a row cut at the width has no
// blank between it and the next, so the position where they meet is the next
// row's first cell — the caret can be there, and a motion off it is not
// ambiguous.
func TestRowColHardSplitTouchesTheNextRow(t *testing.T) {
	lines := Wrap(strings.Repeat("a", 25), 10)
	if len(lines) != 3 {
		t.Fatalf("setup: got %d rows, want 3: %+v", len(lines), lines)
	}
	if row, col := RowCol(lines, 10); row != 1 || col != 0 {
		t.Errorf("RowCol at the cut = (%d,%d), want (1,0)", row, col)
	}
	if row, col := RowCol(lines, 25); row != 2 || col != 5 {
		t.Errorf("RowCol at the program's end = (%d,%d), want (2,5)", row, col)
	}
}
