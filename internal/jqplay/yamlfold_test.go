package jqplay

import (
	"reflect"
	"testing"
)

// yamlfold_test.go covers the YAML result's fold ranges (#2039): which blocks
// fold, in which order, and how many members the placeholder credits them
// with. The inputs are the encoder's own output, which is the only text the
// scan ever sees.

// TestYAMLFoldsNested is the JSON scan's core case in YAML: a mapping holding
// a sequence folds at both levels, outer before inner, each with its own
// member count and unit.
func TestYAMLFoldsNested(t *testing.T) {
	text := EvaluateWith(DialectYQ, ".", "name: ike\ntags:\n  - tui\n  - go\n").Text()
	got := DialectYQ.Folds(text)
	want := []Fold{
		{HeaderLine: 1, EndLine: 3, Items: 2, Unit: UnitItems},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Folds(%q) = %+v, want %+v", text, got, want)
	}
}

// TestYAMLFoldsSequenceOfMappings: each entry of a sequence of mappings folds
// on its own dash row, counting the key that shares the row with the dash.
func TestYAMLFoldsSequenceOfMappings(t *testing.T) {
	text := EvaluateWith(DialectYQ, ".people", "people:\n  - name: ada\n    age: 36\n  - name: bob\n    age: 41\n").Text()
	got := DialectYQ.Folds(text)
	want := []Fold{
		{HeaderLine: 0, EndLine: 1, Items: 2, Unit: UnitKeys},
		{HeaderLine: 2, EndLine: 3, Items: 2, Unit: UnitKeys},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Folds(%q) = %+v, want %+v", text, got, want)
	}
}

// TestYAMLFoldsBlockScalar: a literal block's members are its lines — "3
// keys" over a shell script would be nonsense.
func TestYAMLFoldsBlockScalar(t *testing.T) {
	text := EvaluateWith(DialectYQ, ".", "script: |\n  one\n  two\n  three\n").Text()
	got := DialectYQ.Folds(text)
	want := []Fold{{HeaderLine: 0, EndLine: 3, Items: 3, Unit: UnitLines}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Folds(%q) = %+v, want %+v", text, got, want)
	}
}

// TestYAMLFoldsNestedKeyBeforeSibling: a key whose own block sits above its
// siblings must not make that block's indent the parent's child indent —
// counting from the first child line would credit the mapping with one key.
func TestYAMLFoldsNestedKeyBeforeSibling(t *testing.T) {
	text := "root:\n  a:\n    x: 1\n  b: 2\n  c: 3\n"
	got := yamlFolds(text)
	if len(got) != 2 {
		t.Fatalf("Folds(%q) = %+v, want the root and the nested `a`", text, got)
	}
	if got[0].Items != 3 || got[0].Unit != UnitKeys {
		t.Errorf("root fold = %+v, want 3 keys", got[0])
	}
}

// TestYAMLFoldsSkipWrappedScalars: a long scalar the emitter broke across
// rows is one value, not a block — a continuation row is not a member.
func TestYAMLFoldsSkipWrappedScalars(t *testing.T) {
	text := "note: a long sentence that the emitter\n  wrapped onto a second row\n"
	if got := yamlFolds(text); len(got) != 0 {
		t.Errorf("Folds(%q) = %+v, want none", text, got)
	}
	quoted := "- \"a long quoted sentence that the emitter\n  wrapped onto a second row\"\n"
	if got := yamlFolds(quoted); len(got) != 0 {
		t.Errorf("Folds(%q) = %+v, want none", quoted, got)
	}
}

// TestYAMLFoldsSpanSeveralDocuments: the result buffer holds every output
// joined by the document marker, so each document's blocks fold at their own
// lines and the `---` at column 0 closes the block above it.
func TestYAMLFoldsSpanSeveralDocuments(t *testing.T) {
	text := EvaluateWith(DialectYQ, ".[]", "- {a: {x: 1, y: 2}}\n- {b: {z: 3}}\n").Text()
	got := DialectYQ.Folds(text)
	want := []Fold{
		{HeaderLine: 0, EndLine: 2, Items: 2, Unit: UnitKeys},
		{HeaderLine: 4, EndLine: 5, Items: 1, Unit: UnitKeys},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Folds(%q) = %+v, want %+v", text, got, want)
	}
}

// TestYAMLFoldsTopLevelHasNoHeader: a whole document is not foldable, because
// YAML gives it no row of its own to fold at — the JSON result's outermost
// `{` has no counterpart. Everything below the top level still folds.
func TestYAMLFoldsTopLevelHasNoHeader(t *testing.T) {
	text := EvaluateWith(DialectYQ, ".", "a: 1\nb:\n  c: 2\n").Text()
	got := DialectYQ.Folds(text)
	want := []Fold{{HeaderLine: 1, EndLine: 2, Items: 1, Unit: UnitKeys}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Folds(%q) = %+v, want %+v", text, got, want)
	}
}

// TestYAMLFoldLabel: the YAML placeholder names the block's size without a
// closing delimiter — YAML has none to restore.
func TestYAMLFoldLabel(t *testing.T) {
	cases := []struct {
		fold Fold
		want string
	}{
		{Fold{Items: 3, Unit: UnitKeys}, "⋯ 3 keys"},
		{Fold{Items: 1, Unit: UnitItems}, "⋯ 1 item"},
		{Fold{Items: 7, Unit: UnitLines}, "⋯ 7 lines"},
	}
	for _, c := range cases {
		if got := c.fold.Label(); got != c.want {
			t.Errorf("Label(%+v) = %q, want %q", c.fold, got, c.want)
		}
	}
}
