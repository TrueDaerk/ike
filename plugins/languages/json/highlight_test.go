//go:build cgo

package langjson

import (
	"testing"

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
