package jqplay

import (
	"sort"
	"strings"
	"testing"
)

// TestGridObjects: one column per key in the export's order, a missing key
// marked, null spelled out, nested values as compact JSON and control
// characters escaped so a cell stays one line.
func TestGridObjects(t *testing.T) {
	g, err := Evaluate(".", `[{"name":"a","id":1},{"id":2,"tags":["x","y"],"extra":null},{"name":"two\nlines\u001b"}]`).Grid()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(g.Columns, ","); got != "id,name,extra,tags" {
		t.Fatalf("columns = %q", got)
	}
	if g.Stream {
		t.Fatal("one array output is not a stream")
	}
	row := func(i int) string {
		var out []string
		for _, c := range g.Rows[i] {
			if c.Missing {
				out = append(out, "∅")
				continue
			}
			out = append(out, c.Text)
		}
		return strings.Join(out, "|")
	}
	for i, want := range []string{"1|a|∅|∅", `2|∅|null|["x","y"]`, `∅|two\nlines\u001b|∅|∅`} {
		if got := row(i); got != want {
			t.Errorf("row %d = %q, want %q", i, got, want)
		}
	}
}

// TestGridScalarsAndStreams: a list of scalars is one unnamed column, and a
// stream of several values is a table too, flagged as a stream.
func TestGridScalarsAndStreams(t *testing.T) {
	g, err := Evaluate(".", `[3, "x", null, true]`).Grid()
	if err != nil {
		t.Fatal(err)
	}
	if g.Columns != nil || len(g.Rows) != 4 || g.Rows[1][0].Text != "x" || g.Rows[2][0].Text != "null" {
		t.Fatalf("scalar grid = %+v", g)
	}
	g, err = Evaluate(".[]", `[{"a":1},{"a":2}]`).Grid()
	if err != nil {
		t.Fatal(err)
	}
	if !g.Stream || len(g.Rows) != 2 || g.Rows[1][0].Text != "2" {
		t.Fatalf("stream grid = %+v", g)
	}
}

// TestGridRejectsShapes: a lone document, an empty array and a mixed list
// stay in the text view, each with a reason that says why.
func TestGridRejectsShapes(t *testing.T) {
	for _, tc := range []struct{ program, input, want string }{
		{".", `{"a":1}`, "an object, not a list"},
		{".", `3`, "a number, not a list"},
		{".", `[]`, "empty array"},
		{".", `[{"a":1},2]`, "row 2 is a number"},
		{".", `[1,[2]]`, "row 2 is an array"},
		{".", `[[1],[2]]`, "row 1 is an array"},
		{"empty", `{}`, "empty"},
	} {
		_, err := Evaluate(tc.program, tc.input).Grid()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s over %s: err = %v, want %q", tc.program, tc.input, err, tc.want)
		}
	}
}

// TestGridYAML: a yq result tabulates from the same values.
func TestGridYAML(t *testing.T) {
	g, err := EvaluateWith(DialectYQ, ".", "- name: a\n  n: 2\n- name: b\n").Grid()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(g.Columns, ",") != "n,name" || !g.Rows[1][0].Missing {
		t.Fatalf("yaml grid = %+v", g)
	}
}

// TestCompareCells: missing first, then jq's type order, numbers by value
// across representations, strings by bytes.
func TestCompareCells(t *testing.T) {
	g, err := Evaluate(".", `[{"v":"b"},{"v":10},{"v":null},{},{"v":2.5},{"v":true},{"v":false},{"v":"a"},{"v":{"k":1}},{"v":[1]},{"v":100000000000000000000}]`).Grid()
	if err != nil {
		t.Fatal(err)
	}
	cells := make([]GridCell, len(g.Rows))
	for i, r := range g.Rows {
		cells[i] = r[0]
	}
	sort.SliceStable(cells, func(i, j int) bool { return CompareCells(cells[i], cells[j]) < 0 })
	var got []string
	for _, c := range cells {
		if c.Missing {
			got = append(got, "∅")
			continue
		}
		got = append(got, c.Text)
	}
	want := `∅ null false true 2.5 10 100000000000000000000 a b [1] {"k":1}`
	if strings.Join(got, " ") != want {
		t.Fatalf("order = %q\nwant    %q", strings.Join(got, " "), want)
	}
}
