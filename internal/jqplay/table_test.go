package jqplay

import (
	"strings"
	"testing"
)

// TestDelimitedObjects: the header is the union of the keys in order of first
// appearance (sorted within one object), a missing key is an empty cell, and
// nested values are compact JSON text, quoted per RFC 4180.
func TestDelimitedObjects(t *testing.T) {
	r := Evaluate(".", `[{"name":"a","id":1},{"id":2,"tags":["x","y"],"extra":null},{"name":"q\"uote, comma"}]`)
	got, err := r.Delimited(CSV)
	if err != nil {
		t.Fatal(err)
	}
	want := "id,name,extra,tags\n" +
		"1,a,,\n" +
		`2,,,"[""x"",""y""]"` + "\n" +
		`,"q""uote, comma",,` + "\n"
	if got != want {
		t.Fatalf("csv:\n%q\nwant\n%q", got, want)
	}
}

// TestDelimitedTSV: TSV quotes a cell holding a tab or a line break, and
// leaves a comma alone.
func TestDelimitedTSV(t *testing.T) {
	r := Evaluate(".", `[{"a":"x,y","b":"tab\there","c":"two\nlines","d":true}]`)
	got, err := r.Delimited(TSV)
	if err != nil {
		t.Fatal(err)
	}
	want := "a\tb\tc\td\nx,y\t\"tab\there\"\t\"two\nlines\"\ttrue\n"
	if got != want {
		t.Fatalf("tsv:\n%q\nwant\n%q", got, want)
	}
}

// TestDelimitedScalars: a list of scalars is one column without a header;
// a stream of outputs (`.[]`) tabulates like the array it came from.
func TestDelimitedScalars(t *testing.T) {
	for _, program := range []string{".", ".[]"} {
		r := Evaluate(program, `["a", 1.5, null, false, "b,c"]`)
		got, err := r.Delimited(CSV)
		if err != nil {
			t.Fatalf("%s: %v", program, err)
		}
		if want := "a\n1.5\n\nfalse\n\"b,c\"\n"; got != want {
			t.Fatalf("%s: got %q want %q", program, got, want)
		}
	}
}

// TestDelimitedYQ: the yq dialect exports its values the same way.
func TestDelimitedYQ(t *testing.T) {
	r := EvaluateWith(DialectYQ, ".items", "items:\n  - k: 1\n  - k: 2\n")
	got, err := r.Delimited(CSV)
	if err != nil {
		t.Fatal(err)
	}
	if want := "k\n1\n2\n"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// TestDelimitedRawOutput: the export reads the values, so -r does not change it.
func TestDelimitedRawOutput(t *testing.T) {
	in, err := DialectJQ.Parse(`[{"s":"x"}]`)
	if err != nil {
		t.Fatal(err)
	}
	r := RunWith(t.Context(), ".[] | .s", in, Options{Raw: true})
	if got, err := r.Delimited(CSV); err != nil || got != "x\n" {
		t.Fatalf("got %q, %v", got, err)
	}
}

// TestTableReason: results that are no table say why.
func TestTableReason(t *testing.T) {
	cases := []struct{ program, input, want string }{
		{".", `[[1,2],[3]]`, "row 1 is an array"},
		{".", `[{"a":1}, 2]`, "row 2 is a number"},
		{".", `[1, {"a":1}]`, "row 2 is an object"},
		{".", `[]`, "empty array"},
		{"empty", `{}`, "the result is empty"},
	}
	for _, tc := range cases {
		r := Evaluate(tc.program, tc.input)
		if got := r.TableReason(); !strings.Contains(got, tc.want) {
			t.Errorf("%s over %s: reason %q, want it to mention %q", tc.program, tc.input, got, tc.want)
		}
	}
	if got := Evaluate(".", `{"a":1}`).TableReason(); got != "" {
		t.Fatalf("a single object is a one-row table, got reason %q", got)
	}
}

// TestXMQValues: an xmq result has values only when its command wrote JSON.
func TestXMQValues(t *testing.T) {
	r := Result{dialect: DialectXMQ, ext: "json", Outputs: []string{`[{"a":1},{"a":2}]`}}
	if got, err := r.Delimited(CSV); err != nil || got != "a\n1\n2\n" {
		t.Fatalf("to-json: got %q, %v", got, err)
	}
	r = Result{dialect: DialectXMQ, ext: "html", Outputs: []string{"<p/>"}}
	if got := r.TableReason(); !strings.Contains(got, "not JSON") {
		t.Fatalf("html: reason %q", got)
	}
}
