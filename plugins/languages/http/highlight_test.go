//go:build cgo

package langhttp

import (
	"fmt"
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
	// The target's prefix reads as three segments since #1740: the scheme
	// dims, "://" is punctuation, the authority keeps the url string colour
	// through string.special.
	if got := ix.CaptureAt(1, 5); got != "comment" { // https
		t.Errorf("scheme: got capture %q, want comment", got)
	}
	if got := ix.CaptureAt(1, 10); got != "punctuation" { // ://
		t.Errorf("separator: got capture %q, want punctuation", got)
	}
	if got := ix.CaptureAt(1, 13); got != "string.special" { // api.test
		t.Errorf("authority: got capture %q, want string.special", got)
	}
	if got := ix.CaptureAt(2, 0); got != "constant" { // header name
		t.Errorf("header name: got capture %q, want constant", got)
	}
}

// TestErrorRecoveryMidFile (#2226): a request the grammar cannot parse
// cleanly — a `{{host}}` placeholder target, plain garbage, an unclosed
// placeholder, an unbalanced body — must not take the rest of the file's
// highlighting with it. The broken request sits ~200 lines in (the report was
// about a long file, so this guards the far-from-the-error case too), and the
// section after it must highlight again at its `###` separator at the latest:
// separator, method, header name and JSON body all carry their captures.
func TestErrorRecoveryMidFile(t *testing.T) {
	brokenVariants := map[string][]string{
		"placeholder-target": {
			"### Check scrapeless",
			"GET {{host}}/ai_tracking_read/_search",
			"Content-Type: application/json",
			"",
			"{",
			"    \"size\": 0,",
			"    \"query\": {",
			"    }",
			"}",
			"",
		},
		"garbage-line":    {"### broken", "THISISNOTAMETHOD !!! ???", ""},
		"unclosed-var":    {"### broken", "GET {{host/ai/_search", ""},
		"unbalanced-body": {"### broken", "POST https://x.test/a", "Content-Type: application/json", "", "{", "  \"unclosed\": {", ""},
	}
	for name, broken := range brokenVariants {
		// ~200 lines of healthy requests, the broken section, one trailing
		// healthy section whose highlighting is what the test guards.
		var lines []string
		for i := 0; i < 20; i++ {
			lines = append(lines,
				fmt.Sprintf("### Request %d", i),
				fmt.Sprintf("POST https://seo01.example.net:9210/idx%d/_search", i),
				"Content-Type: application/json",
				"",
				"{",
				fmt.Sprintf("    \"size\": %d,", i),
				"    \"query\": {",
				"    }",
				"}",
				"",
			)
		}
		lines = append(lines, broken...)
		after := len(lines)
		lines = append(lines,
			"### After the broken request",
			"POST https://example.com/ok",
			"Content-Type: application/json",
			"",
			"{",
			"    \"tail\": true",
			"}",
		)
		ix := highlight.NewIndex(highlight.Highlight("req.http", lines))
		if got := ix.CaptureAt(after, 0); got != "comment" {
			t.Errorf("%s: separator after broken request: got capture %q, want comment", name, got)
		}
		if got := ix.CaptureAt(after+1, 0); got != "function" {
			t.Errorf("%s: method after broken request: got capture %q, want function", name, got)
		}
		if got := ix.CaptureAt(after+2, 0); got != "constant" {
			t.Errorf("%s: header after broken request: got capture %q, want constant", name, got)
		}
		if got := ix.CaptureAt(after+5, 5); got == "" {
			t.Errorf("%s: JSON body key after broken request carries no capture", name)
		}
	}
}

// TestPlaceholderRequestHighlighted (#2226, the repro from the report): the
// `{{host}}` request itself, its header and its JSON body all highlight — the
// parse error the placeholder used to cause must not eat its own section
// either.
func TestPlaceholderRequestHighlighted(t *testing.T) {
	lines := []string{
		"### Check scrapeless",
		"GET {{host}}/ai_tracking_read/_search",
		"Content-Type: application/json",
		"",
		"{",
		"    \"size\": 0",
		"}",
	}
	ix := highlight.NewIndex(highlight.Highlight("req.http", lines))
	if got := ix.CaptureAt(1, 0); got != "function" {
		t.Errorf("method: got capture %q, want function", got)
	}
	if got := ix.CaptureAt(1, 6); got != "variable" {
		t.Errorf("placeholder name: got capture %q, want variable", got)
	}
	if got := ix.CaptureAt(1, 12); got != "label" {
		t.Errorf("path after placeholder: got capture %q, want label", got)
	}
	if got := ix.CaptureAt(2, 0); got != "constant" {
		t.Errorf("header name: got capture %q, want constant", got)
	}
	if got := ix.CaptureAt(5, 5); got == "" {
		t.Error("JSON body key carries no capture")
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
