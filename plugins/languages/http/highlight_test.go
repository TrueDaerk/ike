//go:build cgo

package langhttp

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
)

func TestLanguageRegistered(t *testing.T) {
	l, ok := lang.ByExt("http")
	if !ok || l.ID != "http" {
		t.Fatalf("http language not registered: ok=%v %+v", ok, l)
	}
	if l.LineComment != "#" {
		t.Errorf("line comment: %q", l.LineComment)
	}
	if _, ok := lang.ByExt("rest"); !ok {
		t.Error(".rest extension not registered")
	}
}

func TestHighlighting(t *testing.T) {
	if !highlight.Supported("req.http") {
		t.Fatal("highlighting must be supported for .http")
	}
	lines := []string{
		"### create thing",
		"POST https://api.test/things HTTP/1.1",
		"Content-Type: application/json",
	}
	spans := highlight.Highlight("req.http", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for .http source, got none")
	}
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 0); got != "comment" { // ### separator
		t.Errorf("separator: got capture %q, want comment", got)
	}
	if got := ix.CaptureAt(1, 0); got != "function" { // POST
		t.Errorf("method: got capture %q, want function", got)
	}
	if got := ix.CaptureAt(1, 5); got != "string" { // url
		t.Errorf("url: got capture %q, want string", got)
	}
	if got := ix.CaptureAt(2, 0); got != "constant" { // header name
		t.Errorf("header name: got capture %q, want constant", got)
	}
}

// TestFoldedQueryHighlighting: indented "?"/"&" continuation lines (#1269)
// are part of the request target — since #1585 they carry the query-param
// captures (separators as punctuation, keys as property), not a broken
// header.
func TestFoldedQueryHighlighting(t *testing.T) {
	lines := []string{
		"GET https://example.net:9210/_cat/indices",
		"    ? v =",
		"    & s = i",
		"Accept: application/json",
	}
	ix := highlight.NewIndex(highlight.Highlight("req.http", lines))
	for _, ln := range []int{1, 2} {
		if got := ix.CaptureAt(ln, 4); got != "punctuation" {
			t.Errorf("folded query line %d separator: got capture %q, want punctuation", ln, got)
		}
		if got := ix.CaptureAt(ln, 6); got != "property" {
			t.Errorf("folded query line %d key: got capture %q, want property", ln, got)
		}
	}
	if got := ix.CaptureAt(3, 0); got != "constant" {
		t.Errorf("header after the fold: got capture %q, want constant", got)
	}
}
