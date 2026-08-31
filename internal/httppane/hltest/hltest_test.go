// Package hltest verifies the response viewer's fenced body highlighting
// end to end (#1270). It lives in its own package on purpose: the check
// requires a grammar plugin to be linked (as cmd/ike/main.go does), and
// linking one into internal/httppane's own test binary would change the
// rendered output every other test in that package asserts on.
//
// The tests run in both build flavours: with CGo the JSON grammar highlights
// the body; without it (or without the plugin linked) the viewer must say so
// instead of silently rendering plain text — the actual root cause of #1270.
package hltest

import (
	"net/http"
	"strings"
	"testing"
	"time"

	// Linked like the real binary; xml is deliberately left out so the
	// missing-grammar notice can be asserted in a CGo build too.
	_ "ike/plugins/languages/json"

	"ike/internal/highlight"
	"ike/internal/httpclient"
	"ike/internal/httppane"
	"ike/internal/theme"
)

func resp(ct, body string) *httpclient.Response {
	return &httpclient.Response{
		Status: "200 OK", Proto: "HTTP/1.1",
		Headers:  http.Header{"Content-Type": {ct}},
		Body:     []byte(body),
		Duration: time.Millisecond,
	}
}

func viewer(t *testing.T, ct, body string) *httppane.Model {
	t.Helper()
	m := httppane.New(theme.DefaultPalette())
	m.SetSize(80, 20)
	m.Set("one", resp(ct, body))
	m.FinishHighlight() // the syntax pass runs off-loop now (#2353)
	return &m
}

// hasNotice reports whether the viewer shows the missing-grammar notice for
// tag.
func hasNotice(m *httppane.Model, tag string) bool {
	for _, w := range m.Warnings() {
		if strings.Contains(w, "no "+tag+" highlighter") {
			return true
		}
	}
	return false
}

// TestJSONBody is the regression test for #1270: with the grammar linked, a
// JSON response with a real-world Content-Type renders highlighted and
// without notices; in a CGo-free build the notice appears instead of silent
// plain text.
func TestJSONBody(t *testing.T) {
	supported := highlight.FencedSupported("json")
	for _, ct := range []string{
		"application/json",
		"application/json; charset=utf-8",
		"application/vnd.api+json",
	} {
		m := viewer(t, ct, `{"ok":true,"n":1}`)
		switch {
		case supported:
			if !m.Highlighted() {
				t.Errorf("%s: body must produce highlight spans", ct)
			}
			view := m.View()
			if i := strings.Index(view, "{"); i < 0 || !strings.Contains(view[i:], "\x1b[") {
				t.Errorf("%s: body must render styled", ct)
			}
			if w := m.Warnings(); len(w) > 0 {
				t.Errorf("%s: unexpected warnings %v", ct, w)
			}
		default:
			if !hasNotice(m, "json") {
				t.Errorf("%s: CGo-free build must show the missing-grammar notice, got %v", ct, m.Warnings())
			}
		}
	}
}

// TestFencedSupportedResolvesTags: the seam httppane consults reports linked
// grammars truthfully, including the extension-tag form and the empty tag.
func TestFencedSupportedResolvesTags(t *testing.T) {
	if highlight.FencedSupported("") {
		t.Error("an empty tag resolves to no language")
	}
	if highlight.FencedSupported("definitely-not-a-language") {
		t.Error("an unknown tag resolves to no language")
	}
}

// TestMissingGrammarNotice: a recognized Content-Type whose grammar is not in
// the build says so instead of rendering plain silently (#1270). The xml
// plugin is not linked into this binary.
func TestMissingGrammarNotice(t *testing.T) {
	if highlight.FencedSupported("xml") {
		t.Skip("xml grammar linked in this build")
	}
	m := viewer(t, "application/xml", `<a>x</a>`)
	if m.Highlighted() {
		t.Fatal("no xml grammar: no spans expected")
	}
	if !hasNotice(m, "xml") {
		t.Fatalf("missing-grammar notice expected, warnings: %v", m.Warnings())
	}
}

// TestUnknownContentTypeNoNotice: an unrecognized type renders plain without
// a notice — there is nothing to highlight by design.
func TestUnknownContentTypeNoNotice(t *testing.T) {
	m := viewer(t, "text/plain", "hello")
	if w := m.Warnings(); len(w) > 0 {
		t.Errorf("no notice expected for text/plain, got %v", w)
	}
}
