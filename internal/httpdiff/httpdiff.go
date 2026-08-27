// Package httpdiff renders stored HTTP responses (internal/httphistory) as
// the plain text the diff viewer compares (#1992). Two runs of the same
// request differ in a hundred irrelevant ways when their bodies are compared
// byte for byte — key order, indentation, a minified answer against a
// pretty-printed one — so a JSON body is normalized first: decoded and
// re-encoded with stable key order and one fixed indentation, which makes a
// key-order-only difference diff as nothing at all. Non-JSON bodies are
// compared as they arrived.
package httpdiff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"ike/internal/httphistory"
)

// Indent is the fixed indentation normalized JSON is re-encoded with, so two
// responses formatted differently on the wire still line up.
const Indent = "  "

// Text renders one stored response as the diff text: status line, headers,
// blank line, body. Both halves are comparable in one view (#1992) with the
// body — the part that matters — last, where a growing diff does not push the
// headers out of sight.
func Text(e httphistory.Entry) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(e.Proto + " " + e.Status))
	b.WriteString("\n\n")
	b.WriteString(HeadersText(e.Headers))
	b.WriteString("\n")
	// FullBody, not Body (#2157): a spooled entry keeps only its head in
	// memory, and a diff of two heads would report a difference at the point
	// where both were cut off.
	b.WriteString(NormalizeBody(e.Headers.Get("Content-Type"), e.FullBody()))
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// HeadersText renders the headers one "Name: value" per line, sorted by name
// and then in their sent order per name, so header diffs are order-stable
// too — Go's http.Header is a map and would otherwise shuffle every render.
func HeadersText(h http.Header) string {
	names := make([]string, 0, len(h))
	for n := range h {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		for _, v := range h[n] {
			b.WriteString(n + ": " + v + "\n")
		}
	}
	return b.String()
}

// NormalizeBody returns the diff-ready form of one response body: JSON
// re-encoded with sorted keys and Indent indentation, anything else as it
// arrived. A binary body collapses to a notice — comparing raw bytes as text
// produces garbage, exactly as the viewer refuses to render them.
//
// A body counts as JSON when the Content-Type says so, or when it looks like
// an object/array and parses; that keeps an API answering without a
// Content-Type normalized while leaving a text/plain "123" — valid JSON, but
// not meant as such — untouched.
func NormalizeBody(contentType string, body []byte) string {
	if len(body) == 0 {
		return ""
	}
	if isBinary(body) {
		return fmt.Sprintf("(binary body, %d bytes)", len(body))
	}
	text := string(body)
	if !looksJSON(contentType, body) {
		return text
	}
	norm, ok := normalizeJSON(body)
	if !ok {
		// Malformed JSON is still the response the server sent: comparing it
		// verbatim beats hiding it behind a parse error.
		return text
	}
	return norm
}

// looksJSON reports whether body should be normalized as JSON.
func looksJSON(contentType string, body []byte) bool {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	if strings.HasSuffix(ct, "/json") || strings.HasSuffix(ct, "+json") {
		return true
	}
	// Line-delimited streams (Elasticsearch bulk, log APIs) name themselves
	// without the "/json" suffix but carry the same values.
	switch {
	case strings.HasSuffix(ct, "ndjson"), strings.HasSuffix(ct, "jsonl"),
		strings.HasSuffix(ct, "json-seq"):
		return true
	}
	if ct != "" {
		return false
	}
	t := bytes.TrimSpace(body)
	return len(t) > 0 && (t[0] == '{' || t[0] == '[')
}

// normalizeJSON re-encodes every JSON value in body with sorted keys (Go's
// encoder sorts map keys) and fixed indentation. Several values in a row —
// an NDJSON stream — normalize one after another, so those compare cleanly
// too. ok is false when the input is not JSON after all; the caller then
// keeps the raw text.
func normalizeJSON(body []byte) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber() // 1.0 must not become 1, and huge ints must not lose digits
	var out strings.Builder
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false) // a URL in a body stays readable
	enc.SetIndent("", Indent)
	for {
		var v any
		err := dec.Decode(&v)
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", false
		}
		if err := enc.Encode(v); err != nil {
			return "", false
		}
	}
	if out.Len() == 0 {
		return "", false
	}
	return strings.TrimRight(out.String(), "\n") + "\n", true
}

// isBinary mirrors the viewer's rule (internal/httppane): invalid UTF-8 or a
// NUL byte means the body is not text.
func isBinary(b []byte) bool {
	return bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b)
}
