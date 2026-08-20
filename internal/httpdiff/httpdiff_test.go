package httpdiff

import (
	"net/http"
	"strings"
	"testing"

	"ike/internal/httphistory"
)

// TestNormalizeKeyOrderOnly is the point of the whole package (#1992): two
// JSON bodies that differ only in key order must normalize to the same text,
// so the diff comes out empty.
func TestNormalizeKeyOrderOnly(t *testing.T) {
	a := NormalizeBody("application/json", []byte(`{"b":2,"a":1,"c":{"y":true,"x":null}}`))
	b := NormalizeBody("application/json", []byte(`{"c":{"x":null,"y":true},"a":1,"b":2}`))
	if a != b {
		t.Fatalf("key order must not survive normalization:\n%q\n%q", a, b)
	}
	if !strings.Contains(a, "\n"+Indent+`"a": 1`) {
		t.Fatalf("expected indented, sorted output, got:\n%s", a)
	}
}

// TestNormalizeFormatting folds a minified body onto its pretty-printed twin:
// serialization noise must not show up as a difference either.
func TestNormalizeFormatting(t *testing.T) {
	min := NormalizeBody("application/json", []byte(`{"a":[1,2],"b":"x"}`))
	pretty := NormalizeBody("application/json", []byte("{\n\t\"a\": [ 1, 2 ],\n\t\"b\": \"x\"\n}\n"))
	if min != pretty {
		t.Fatalf("formatting must not survive normalization:\n%q\n%q", min, pretty)
	}
}

// TestNormalizeRealDifference guards the other side: a changed value still
// differs after normalization.
func TestNormalizeRealDifference(t *testing.T) {
	a := NormalizeBody("application/json", []byte(`{"a":1}`))
	b := NormalizeBody("application/json", []byte(`{"a":2}`))
	if a == b {
		t.Fatal("a changed value must survive normalization")
	}
}

// TestNormalizeNumbers keeps number literals verbatim: 1.0 must not collapse
// to 1, and a large integer must not go through float64.
func TestNormalizeNumbers(t *testing.T) {
	got := NormalizeBody("application/json", []byte(`{"f":1.0,"big":12345678901234567890}`))
	for _, want := range []string{`"f": 1.0`, `"big": 12345678901234567890`} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %s in:\n%s", want, got)
		}
	}
}

// TestNormalizeNoEscapeHTML keeps a URL readable rather than <-escaped.
func TestNormalizeNoEscapeHTML(t *testing.T) {
	got := NormalizeBody("application/json", []byte(`{"u":"a?b=1&c=2","t":"<b>"}`))
	if !strings.Contains(got, `"a?b=1&c=2"`) || !strings.Contains(got, `"<b>"`) {
		t.Fatalf("HTML escaping must stay off, got:\n%s", got)
	}
}

// TestNormalizeContentTypeVariants covers the JSON detection: a +json suffix
// and a charset parameter count, a body without a Content-Type counts when it
// looks like JSON, and a non-JSON type is left alone.
func TestNormalizeContentTypeVariants(t *testing.T) {
	body := []byte(`{"b":2,"a":1}`)
	want := NormalizeBody("application/json", body)
	for _, ct := range []string{"application/problem+json", "APPLICATION/JSON; charset=utf-8", ""} {
		if got := NormalizeBody(ct, body); got != want {
			t.Fatalf("content type %q: got %q, want %q", ct, got, want)
		}
	}
	// text/plain that happens to parse as JSON is not a JSON response.
	if got := NormalizeBody("text/plain", []byte("123")); got != "123" {
		t.Fatalf("text/plain must diff as-is, got %q", got)
	}
}

// TestNormalizeNonJSON leaves other bodies byte-identical — including a
// malformed JSON body, which is still what the server answered.
func TestNormalizeNonJSON(t *testing.T) {
	xml := "<a>\n  <b/>\n</a>"
	if got := NormalizeBody("application/xml", []byte(xml)); got != xml {
		t.Fatalf("XML must diff as-is, got %q", got)
	}
	broken := `{"a":1,`
	if got := NormalizeBody("application/json", []byte(broken)); got != broken {
		t.Fatalf("malformed JSON must diff as-is, got %q", got)
	}
	if got := NormalizeBody("application/json", nil); got != "" {
		t.Fatalf("empty body must stay empty, got %q", got)
	}
}

// TestNormalizeNDJSON normalizes a stream of values one after another.
func TestNormalizeNDJSON(t *testing.T) {
	a := NormalizeBody("application/x-ndjson", []byte(`{"b":1,"a":2}`+"\n"+`{"d":3,"c":4}`))
	b := NormalizeBody("application/x-ndjson", []byte(`{"a":2,"b":1}`+"\n"+`{"c":4,"d":3}`))
	if a != b {
		t.Fatalf("NDJSON key order must not survive:\n%q\n%q", a, b)
	}
}

// TestNormalizeBinary refuses to compare raw bytes as text.
func TestNormalizeBinary(t *testing.T) {
	got := NormalizeBody("application/octet-stream", []byte{0x00, 0x01, 0x02})
	if !strings.HasPrefix(got, "(binary body, 3 bytes") {
		t.Fatalf("expected a binary notice, got %q", got)
	}
}

// TestHeadersTextSorted renders headers in a stable order, values included.
func TestHeadersTextSorted(t *testing.T) {
	h := http.Header{
		"Server":       {"nginx"},
		"Content-Type": {"application/json"},
		"Set-Cookie":   {"a=1", "b=2"},
	}
	want := "Content-Type: application/json\nServer: nginx\nSet-Cookie: a=1\nSet-Cookie: b=2\n"
	if got := HeadersText(h); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestTextCombinesStatusHeadersBody checks the combined diff view: status
// line, headers, blank line, normalized body — and that the duration, which
// differs on every run, stays out of it.
func TestTextCombinesStatusHeadersBody(t *testing.T) {
	e := httphistory.Entry{
		Proto:      "HTTP/1.1",
		Status:     "200 OK",
		StatusCode: 200,
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       []byte(`{"b":2,"a":1}`),
	}
	got := Text(e)
	want := "HTTP/1.1 200 OK\n\nContent-Type: application/json\n\n{\n  \"a\": 1,\n  \"b\": 2\n}\n"
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

// TestTextKeyOrderOnlyEntries is the acceptance criterion end to end: two
// stored entries differing only in key order render identically.
func TestTextKeyOrderOnlyEntries(t *testing.T) {
	mk := func(body string) httphistory.Entry {
		return httphistory.Entry{
			Proto: "HTTP/1.1", Status: "200 OK", StatusCode: 200,
			Headers: http.Header{"Content-Type": {"application/json"}},
			Body:    []byte(body),
		}
	}
	if a, b := Text(mk(`{"x":1,"y":2}`)), Text(mk(`{"y":2,"x":1}`)); a != b {
		t.Fatalf("entries must render identically:\n%q\n%q", a, b)
	}
}
