package diff

import (
	"reflect"
	"strings"
	"testing"
)

// applyEdits replays an edit script and returns the two sides it encodes, to
// verify the script is a valid transformation of a into b.
func applyEdits(edits []Edit) (a, b []string) {
	for _, e := range edits {
		switch e.Op {
		case OpEqual:
			a = append(a, e.Text)
			b = append(b, e.Text)
		case OpDelete:
			a = append(a, e.Text)
		case OpInsert:
			b = append(b, e.Text)
		}
	}
	return a, b
}

func TestLinesRoundTrip(t *testing.T) {
	cases := []struct{ name, a, b string }{
		{"equal", "a\nb\nc", "a\nb\nc"},
		{"insert", "a\nc", "a\nb\nc"},
		{"delete", "a\nb\nc", "a\nc"},
		{"change", "a\nx\nc", "a\ny\nc"},
		{"empty left", "", "a\nb"},
		{"empty right", "a\nb", ""},
		{"both empty", "", ""},
		{"disjoint", "a\nb", "x\ny\nz"},
		{"tail change", "a\nb\nc", "a\nb\nd"},
		{"head change", "a\nb\nc", "z\nb\nc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			edits := Lines(splitLines(tc.a), splitLines(tc.b))
			gotA, gotB := applyEdits(edits)
			if !reflect.DeepEqual(gotA, splitLines(tc.a)) {
				t.Fatalf("left side mismatch: got %q want %q", gotA, splitLines(tc.a))
			}
			if !reflect.DeepEqual(gotB, splitLines(tc.b)) {
				t.Fatalf("right side mismatch: got %q want %q", gotB, splitLines(tc.b))
			}
		})
	}
}

func TestLinesMinimalForSingleChange(t *testing.T) {
	edits := Lines([]string{"a", "x", "c"}, []string{"a", "y", "c"})
	want := []Edit{
		{Op: OpEqual, Text: "a"},
		{Op: OpDelete, Text: "x"},
		{Op: OpInsert, Text: "y"},
		{Op: OpEqual, Text: "c"},
	}
	if !reflect.DeepEqual(edits, want) {
		t.Fatalf("got %v want %v", edits, want)
	}
}

func TestComputePairsChangedLines(t *testing.T) {
	res := Compute("a\nold line\nc", "a\nnew line\nc")
	if len(res.Rows) != 3 {
		t.Fatalf("want 3 rows, got %d: %+v", len(res.Rows), res.Rows)
	}
	row := res.Rows[1]
	if row.Kind != RowChanged {
		t.Fatalf("middle row should be RowChanged, got %v", row.Kind)
	}
	if row.LeftNo != 2 || row.RightNo != 2 {
		t.Fatalf("changed row numbers: got %d/%d want 2/2", row.LeftNo, row.RightNo)
	}
	// "old" vs "new": the differing prefix is runes [0,3) on both sides.
	if len(row.LeftSpans) == 0 || len(row.RightSpans) == 0 {
		t.Fatalf("changed row should carry intra-line spans: %+v", row)
	}
	if row.LeftSpans[0].Start != 0 || row.LeftSpans[0].End != 3 {
		t.Fatalf("left span: got %+v want [0,3)", row.LeftSpans[0])
	}
	if row.RightSpans[0].Start != 0 || row.RightSpans[0].End != 3 {
		t.Fatalf("right span: got %+v want [0,3)", row.RightSpans[0])
	}
}

func TestComputeOneSidedRows(t *testing.T) {
	res := Compute("a\nb", "a\nb\nc\nd")
	kinds := make([]Kind, len(res.Rows))
	for i, r := range res.Rows {
		kinds[i] = r.Kind
	}
	want := []Kind{RowSame, RowSame, RowAdded, RowAdded}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("kinds: got %v want %v", kinds, want)
	}
	if res.Rows[2].LeftNo != 0 || res.Rows[2].RightNo != 3 {
		t.Fatalf("added row numbers: got %d/%d want 0/3", res.Rows[2].LeftNo, res.Rows[2].RightNo)
	}
}

func TestComputeUnevenChangeRun(t *testing.T) {
	// Two deletes vs one insert: one changed pair plus one pure removal.
	res := Compute("a\nx\ny\nc", "a\nz\nc")
	kinds := make([]Kind, len(res.Rows))
	for i, r := range res.Rows {
		kinds[i] = r.Kind
	}
	want := []Kind{RowSame, RowChanged, RowRemoved, RowSame}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("kinds: got %v want %v", kinds, want)
	}
}

func TestHunks(t *testing.T) {
	res := Compute("a\nx\nc\nd\ny\nf", "a\nX\nc\nd\nY\nf")
	if len(res.Hunks) != 2 {
		t.Fatalf("want 2 hunks, got %d: %+v", len(res.Hunks), res.Hunks)
	}
	if res.Hunks[0] != (Hunk{Start: 1, End: 2}) {
		t.Fatalf("hunk 0: got %+v", res.Hunks[0])
	}
	if res.Hunks[1] != (Hunk{Start: 4, End: 5}) {
		t.Fatalf("hunk 1: got %+v", res.Hunks[1])
	}
}

func TestHunkAtEnd(t *testing.T) {
	res := Compute("a\nb", "a\nb\nc")
	if len(res.Hunks) != 1 {
		t.Fatalf("want 1 hunk, got %d", len(res.Hunks))
	}
	if res.Hunks[0].End != len(res.Rows) {
		t.Fatalf("trailing hunk should end at row count: %+v", res.Hunks[0])
	}
}

func TestRefineSkipsOversizedLines(t *testing.T) {
	long := strings.Repeat("x", maxRefineRunes+1)
	ls, rs := refine(long+"a", long+"b")
	if ls != nil || rs != nil {
		t.Fatalf("oversized lines should skip refinement, got %v / %v", ls, rs)
	}
}

func TestRefineMergesAdjacentSpans(t *testing.T) {
	// "abcdef" -> "aXYdef": runes 1-2 replaced; the delete+insert runs touch,
	// so each side carries one merged span.
	ls, rs := refine("abcdef", "aXYdef")
	if len(ls) != 1 || ls[0] != (Span{Start: 1, End: 3}) {
		t.Fatalf("left spans: got %+v want [{1 3}]", ls)
	}
	if len(rs) != 1 || rs[0] != (Span{Start: 1, End: 3}) {
		t.Fatalf("right spans: got %+v want [{1 3}]", rs)
	}
}

func TestSplitLines(t *testing.T) {
	if got := splitLines(""); got != nil {
		t.Fatalf("empty text should split to nil, got %q", got)
	}
	if got := splitLines("a\nb\n"); !reflect.DeepEqual(got, []string{"a", "b", ""}) {
		t.Fatalf("trailing newline: got %q", got)
	}
}
