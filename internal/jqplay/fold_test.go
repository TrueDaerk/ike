package jqplay

import (
	"reflect"
	"testing"
)

// fold_test.go covers the result's fold ranges (#2029): which nodes fold, in
// which order, and how many members the placeholder credits them with.

// TestFoldsNested is the issue's core case: an object holding an array folds
// at both levels, outer before inner, each with its own member count.
func TestFoldsNested(t *testing.T) {
	text := Evaluate(".", `{"name":"ike","tags":["tui","go"]}`).Text()
	got := Folds(text)
	want := []Fold{
		{HeaderLine: 0, EndLine: 6, Items: 2, Unit: UnitKeys, Closer: "}"},
		{HeaderLine: 2, EndLine: 5, Items: 2, Unit: UnitItems, Closer: "]"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Folds(%q) = %+v, want %+v", text, got, want)
	}
}

// TestFoldsSkipSingleLineNodes: a node that already fits on one line hides
// nothing, so it is not foldable — a placeholder over `[]` would be longer
// than the value.
func TestFoldsSkipSingleLineNodes(t *testing.T) {
	text := Evaluate(".", `{"empty":{},"list":[],"n":1}`).Text()
	folds := Folds(text)
	if len(folds) != 1 || folds[0].HeaderLine != 0 {
		t.Fatalf("only the multi-line outer object folds, got %+v for %q", folds, text)
	}
	if folds[0].Items != 3 {
		t.Errorf("outer object holds %d keys, want 3", folds[0].Items)
	}
}

// TestFoldsIgnoreDelimitersInStrings: braces inside a string value are text,
// not structure, and must not open a fold that never closes.
func TestFoldsIgnoreDelimitersInStrings(t *testing.T) {
	text := Evaluate(".", `{"tpl":"{{ .Values }}","q":"a \" [b"}`).Text()
	folds := Folds(text)
	if len(folds) != 1 || folds[0].Items != 2 || folds[0].Unit != UnitKeys {
		t.Fatalf("Folds = %+v, want only the 2-key outer object for %q", folds, text)
	}
}

// TestFoldsSpanSeveralOutputs: the result window holds every output value
// joined into one buffer, so each value's own tree folds at its own lines.
func TestFoldsSpanSeveralOutputs(t *testing.T) {
	text := Evaluate(".[]", `[[1,2],[3,4,5]]`).Text()
	got := Folds(text)
	want := []Fold{
		{HeaderLine: 0, EndLine: 3, Items: 2, Unit: UnitItems, Closer: "]"},
		{HeaderLine: 4, EndLine: 8, Items: 3, Unit: UnitItems, Closer: "]"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Folds(%q) = %+v, want %+v", text, got, want)
	}
}

// TestFoldsOnUnbalancedTail: a truncated document yields the folds it can and
// no error — the scan is a lexer, not a validator.
func TestFoldsOnUnbalancedTail(t *testing.T) {
	if folds := Folds("{\n  \"a\": [\n    1\n  ]\n"); len(folds) != 1 || folds[0].HeaderLine != 1 {
		t.Fatalf("Folds over an unclosed object = %+v, want only the closed array", folds)
	}
	if folds := Folds("}\n]\n"); len(folds) != 0 {
		t.Fatalf("stray closers must fold nothing, got %+v", folds)
	}
}

// TestFoldLabel: the placeholder names the node's size in its own unit, in
// the singular where that reads right, and closes the value.
func TestFoldLabel(t *testing.T) {
	cases := []struct {
		fold Fold
		want string
	}{
		{Fold{Items: 3, Unit: UnitKeys, Closer: "}"}, "⋯ 3 keys }"},
		{Fold{Items: 1, Unit: UnitKeys, Closer: "}"}, "⋯ 1 key }"},
		{Fold{Items: 12, Unit: UnitItems, Closer: "]"}, "⋯ 12 items ]"},
		{Fold{Items: 1, Unit: UnitItems, Closer: "]"}, "⋯ 1 item ]"},
	}
	for _, c := range cases {
		if got := c.fold.Label(); got != c.want {
			t.Errorf("Label(%+v) = %q, want %q", c.fold, got, c.want)
		}
	}
}
