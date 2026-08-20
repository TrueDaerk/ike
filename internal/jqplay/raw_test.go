package jqplay

import (
	"errors"
	"strings"
	"testing"
)

// raw_test.go covers EvaluateRaw, the `jq -r`-shaped single-value form the
// .http capture directive (#1993) runs on.

func TestEvaluateRawValues(t *testing.T) {
	cases := []struct {
		program, input, want string
	}{
		// A string comes back unquoted — it is going into a request target or
		// a header, not into a JSON document.
		{".access_token", `{"access_token":"ya29.abc"}`, "ya29.abc"},
		// Everything else keeps its JSON spelling, on one line.
		{".count", `{"count":42}`, "42"},
		{".big", `{"big":9007199254740993}`, "9007199254740993"}, // no float rounding
		{".ok", `{"ok":true}`, "true"},
		{".ids", `{"ids":[1,2]}`, "[1,2]"},
		{".meta", `{"meta":{"a":1}}`, `{"a":1}`},
		// Real jq, not a path syntax: pipes, filters and string building work.
		{`.task | "\(.node):\(.id)"`, `{"task":{"node":"n1","id":7}}`, "n1:7"},
		{".items[] | select(.state==\"done\") | .id", `{"items":[{"state":"run","id":1},{"state":"done","id":2}]}`, "2"},
		// Several outputs: the first one is the captured value.
		{".ids[]", `{"ids":["a","b"]}`, "a"},
		// A JSON stream (an NDJSON body): nulls are skipped, so the first
		// line that actually holds the value wins.
		{".id", "{\"other\":1}\n{\"id\":\"x\"}\n", "x"},
	}
	for _, c := range cases {
		got, err := EvaluateRaw(c.program, c.input)
		if err != nil {
			t.Errorf("EvaluateRaw(%q, %q): %v", c.program, c.input, err)
			continue
		}
		if got != c.want {
			t.Errorf("EvaluateRaw(%q, %q) = %q, want %q", c.program, c.input, got, c.want)
		}
	}
}

// TestEvaluateRawNoValue: a path that matches nothing, an empty iterator and a
// null are one failure — there is no value to capture. An empty string is a
// value, though: the expression did match.
func TestEvaluateRawNoValue(t *testing.T) {
	for _, program := range []string{".missing", ".a.b.c", `.items[] | select(false)`, "empty", "null"} {
		if got, err := EvaluateRaw(program, `{"items":[1,2],"a":{}}`); !errors.Is(err, ErrNoValue) {
			t.Errorf("EvaluateRaw(%q) = %q, %v; want ErrNoValue", program, got, err)
		}
	}
	if got, err := EvaluateRaw(".s", `{"s":""}`); err != nil || got != "" {
		t.Errorf(`EvaluateRaw(".s") on an empty string = %q, %v; want "", nil`, got, err)
	}
}

// TestEvaluateRawNotJSON: a body that is not JSON comes back as an
// *InputError, whose Detail the caller can phrase in its own words.
func TestEvaluateRawNotJSON(t *testing.T) {
	_, err := EvaluateRaw(".a", "<html><body>nope</body></html>")
	var input *InputError
	if !errors.As(err, &input) {
		t.Fatalf("error is %v (%T), want *InputError", err, err)
	}
	if input.Detail == "" {
		t.Error("InputError carries no detail")
	}
}

// TestEvaluateRawBadProgram: a program that does not parse or compile names
// itself as such rather than blaming the body.
func TestEvaluateRawBadProgram(t *testing.T) {
	for _, program := range []string{".a |", "notafunction"} {
		_, err := EvaluateRaw(program, `{"a":1}`)
		if err == nil || !strings.Contains(err.Error(), "invalid jq expression") {
			t.Errorf("EvaluateRaw(%q): %v, want an invalid-expression error", program, err)
		}
	}
}

// TestEvaluateRawRuntimeError: a program that raises reports the raise, not a
// missing value.
func TestEvaluateRawRuntimeError(t *testing.T) {
	_, err := EvaluateRaw(".a | keys", `{"a":1}`)
	if err == nil || errors.Is(err, ErrNoValue) {
		t.Fatalf("EvaluateRaw over a type error: %v", err)
	}
}
