package jqplay

import (
	"reflect"
	"testing"
)

// outline_test.go covers the structure strip's depth-1 scan (#2793).

func outlineOf(t *testing.T, d Dialect, program, input string, opts Options) []OutlineItem {
	t.Helper()
	return runOpts(t, d, program, input, opts).Outline()
}

func TestOutlineJSONObjectKeys(t *testing.T) {
	got := outlineOf(t, DialectJQ, ".", `{"id":1,"user":{"name":"a","tags":["x"]},"a\"b":[1,2],"z":null}`, Options{})
	want := []OutlineItem{{`a"b`, 1}, {"id", 5}, {"user", 6}, {"z", 12}} // gojq sorts the keys
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("outline = %+v, want %+v", got, want)
	}
}

func TestOutlineJSONArrayIndices(t *testing.T) {
	got := outlineOf(t, DialectJQ, ".", `[{"a":1},2,[3]]`, Options{})
	want := []OutlineItem{{"[0]", 1}, {"[1]", 4}, {"[2]", 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("outline = %+v, want %+v", got, want)
	}
}

func TestOutlineStreamListsValues(t *testing.T) {
	got := outlineOf(t, DialectJQ, ".[]", `[{"a":1},2]`, Options{})
	want := []OutlineItem{{"#1", 0}, {"#2", 3}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("outline = %+v, want %+v", got, want)
	}
}

func TestOutlineNothingToList(t *testing.T) {
	for name, c := range map[string]struct {
		program, input string
		opts           Options
	}{
		"scalar":       {".a", `{"a":1}`, Options{}},
		"string":       {".a", `{"a":"x: y"}`, Options{}},
		"empty object": {".", `{}`, Options{}},
		"empty array":  {".", `[]`, Options{}},
		"compact":      {".", `{"a":1,"b":2}`, Options{Compact: true}},
		"raw":          {".", `{"a":1,"b":2}`, Options{Raw: true}},
	} {
		if got := outlineOf(t, DialectJQ, c.program, c.input, c.opts); len(got) != 0 {
			t.Errorf("%s: outline = %+v, want none", name, got)
		}
	}
}

func TestOutlineYAML(t *testing.T) {
	got := outlineOf(t, DialectYQ, ".", "name: web\nspec:\n  replicas: 2\n  ports:\n    - 80\n\"a b\": 1\n", Options{})
	want := []OutlineItem{{"a b", 0}, {"name", 1}, {"spec", 2}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mapping outline = %+v, want %+v", got, want)
	}
	got = outlineOf(t, DialectYQ, ".", "- a: 1\n  b: 2\n- x\n", Options{})
	want = []OutlineItem{{"[0]", 0}, {"[1]", 2}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sequence outline = %+v, want %+v", got, want)
	}
	if got := outlineOf(t, DialectYQ, ".a", "a: 'x: y'\n", Options{}); len(got) != 0 {
		t.Errorf("a scalar string must list nothing: %+v", got)
	}
}
