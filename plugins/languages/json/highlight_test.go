//go:build cgo

package langjson

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"ike/internal/bracket"
	"ike/internal/highlight"
	"ike/internal/lang"
)

// TestJSONGrammar guards the cgo wiring: both ids share a non-nil grammar.
func TestJSONGrammar(t *testing.T) {
	for _, id := range []string{"json", "ndjson"} {
		l, ok := lang.ByID(id)
		if !ok || l.Grammar == nil {
			t.Errorf("%s: grammar is nil under cgo", id)
		}
	}
}

// TestJSONHighlighting parses a small document end-to-end.
func TestJSONHighlighting(t *testing.T) {
	lines := []string{
		`{`,
		`  "name": "ike",`,
		`  "count": 3,`,
		`  "ok": true`,
		`}`,
	}
	spans := highlight.Highlight("config.json", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for JSON source, got none")
	}
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(1, 3); got != "property" { // "name" key
		t.Errorf("key: got capture %q, want property", got)
	}
	if got := ix.CaptureAt(1, 11); got != "string" { // "ike" value
		t.Errorf("string value: got capture %q, want string", got)
	}
	if got := ix.CaptureAt(2, 11); got != "number" { // 3
		t.Errorf("number value: got capture %q, want number", got)
	}
	if got := ix.CaptureAt(3, 9); got != "constant.builtin" { // true
		t.Errorf("boolean value: got capture %q, want constant.builtin", got)
	}
}

// TestNDJSONHighlighting: the grammar's document rule is repeat(_value), so a
// multi-document stream parses line-per-line without error spans.
func TestNDJSONHighlighting(t *testing.T) {
	lines := []string{
		`{"a": 1}`,
		`{"b": 2}`,
	}
	spans := highlight.Highlight("events.ndjson", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for ndjson source, got none")
	}
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(1, 2); got != "property" { // "b" on the second document
		t.Errorf("second document key: got capture %q, want property", got)
	}
}

// TestRainbowLastBracketNoTrailingNewline (#1785): the closing bracket that is
// the file's very last byte must render in its opening partner's rainbow slot.
// The buffer strips a final newline at load (buffer.FromString), so a file
// with and without one parses identically — this test pins that equivalence at
// the highlight level and checks every pair of a large, deeply nested document
// (pretty-printed and minified single-line) against a pure-Go pairing oracle.
func TestRainbowLastBracketNoTrailingNewline(t *testing.T) {
	gen := func(depth, width int) string {
		var b strings.Builder
		var rec func(d int)
		rec = func(d int) {
			b.WriteString("{")
			for i := 0; i < width; i++ {
				if i > 0 {
					b.WriteString(",")
				}
				fmt.Fprintf(&b, `"k%d_%d":`, d, i)
				if d > 0 && i == width-1 {
					rec(d - 1)
				} else {
					fmt.Fprintf(&b, `["a {not} [a] bracket \" é %d",123456789,{"x":[%d]}]`, i, i)
				}
			}
			b.WriteString("}")
		}
		rec(depth)
		return b.String()
	}

	check := func(t *testing.T, lines []string) {
		t.Helper()
		spans := highlight.Highlight("big.json", lines)
		if len(spans) == 0 {
			t.Fatal("no spans")
		}
		ix := highlight.NewIndex(spans)
		// bracket.Scan with its line-local quote heuristics is an exact oracle
		// for valid JSON whose strings never span lines.
		marks := bracket.Scan(lines, bracket.Syntax{})
		for _, m := range marks {
			want := highlight.RainbowCapture(m.Depth)
			if got := ix.CaptureAt(m.Line, m.Col); got != want {
				t.Fatalf("bracket at (%d,%d) depth %d: capture %q, want %q", m.Line, m.Col, m.Depth, got, want)
			}
		}
		last := lines[len(lines)-1]
		if got := ix.CaptureAt(len(lines)-1, len([]rune(last))-1); got != highlight.RainbowCapture(0) {
			t.Fatalf("final byte of file: capture %q, want %q", got, highlight.RainbowCapture(0))
		}
		// Counter-check: a trailing empty line (a file ending in a newline,
		// had the buffer kept it) changes no span.
		withNL := highlight.Highlight("big.json", append(append([]string{}, lines...), ""))
		if !reflect.DeepEqual(spans, withNL) {
			t.Fatal("spans differ with a trailing empty line")
		}
	}

	doc := gen(30, 60) // ~110 KB, nesting depth well past one palette cycle
	t.Run("minified", func(t *testing.T) { check(t, []string{doc}) })
	t.Run("pretty", func(t *testing.T) { check(t, strings.Split(strings.ReplaceAll(doc, ",", ",\n"), "\n")) })
}
