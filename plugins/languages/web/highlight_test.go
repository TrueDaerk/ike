//go:build cgo

package langweb

import (
	"strings"
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// TestWebGrammars guards #925: all three web languages carry a grammar under
// cgo.
func TestWebGrammars(t *testing.T) {
	for _, id := range []string{"typescript", "html", "css"} {
		l, ok := lang.ByID(id)
		if !ok || l.Grammar == nil {
			t.Errorf("%s: grammar is nil under cgo", id)
		}
	}
}

// TestJavaScriptHighlighting: plain vanilla JS (the user-reported gap) under
// the shared TSX grammar.
func TestJavaScriptHighlighting(t *testing.T) {
	lines := []string{
		`// calendar helpers`,
		`const WEEKDAYS = 7;`,
		`function pad(n) {`,
		`  return String(n).padStart(2, "0");`,
		`}`,
		`document.addEventListener("click", (e) => pad(e.detail));`,
	}
	spans := highlight.Highlight("calendar_common.js", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for JS source, got none")
	}
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 0); got != "comment" {
		t.Errorf("comment: got %q", got)
	}
	if got := ix.CaptureAt(1, 0); got != "keyword" { // const
		t.Errorf("const: got %q, want keyword", got)
	}
	if got := ix.CaptureAt(1, 6); got != "constant" { // WEEKDAYS
		t.Errorf("SCREAMING_CASE: got %q, want constant", got)
	}
	if got := ix.CaptureAt(2, 9); got != "function" { // pad
		t.Errorf("function name: got %q, want function", got)
	}
	if got := ix.CaptureAt(3, 20); got != "function.method" { // padStart
		t.Errorf("method call: got %q, want function.method", got)
	}
	if got := ix.CaptureAt(3, 32); got != "string" { // "0"
		t.Errorf("string: got %q, want string", got)
	}
	if got := ix.CaptureAt(5, 0); got != "variable.builtin" { // document
		t.Errorf("document: got %q, want variable.builtin", got)
	}
}

// TestTypeScriptHighlighting: TS annotations under the same grammar.
func TestTypeScriptHighlighting(t *testing.T) {
	lines := []string{
		`interface User { name: string }`,
		`const u: User = { name: "x" };`,
	}
	spans := highlight.Highlight("model.ts", lines)
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 0); got != "keyword" { // interface
		t.Errorf("interface keyword: got %q", got)
	}
	if got := ix.CaptureAt(0, 10); got != "type" { // User
		t.Errorf("type name: got %q, want type", got)
	}
	if got := ix.CaptureAt(1, 9); got != "type" { // User annotation
		t.Errorf("type annotation: got %q, want type", got)
	}
}

// TestJSXHighlighting: lowercase JSX tags in a .jsx file.
func TestJSXHighlighting(t *testing.T) {
	lines := []string{
		`const el = <div className="box">hi</div>;`,
	}
	spans := highlight.Highlight("app.jsx", lines)
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 12); got != "tag" { // div
		t.Errorf("jsx tag: got %q, want tag", got)
	}
	if got := ix.CaptureAt(0, 16); got != "attribute" { // className
		t.Errorf("jsx attribute: got %q, want attribute", got)
	}
}

// TestCSSHighlighting: selectors, properties, values — including the
// pseudo-class reorder for the first-span-wins index.
func TestCSSHighlighting(t *testing.T) {
	lines := []string{
		`/* base */`,
		`.card:hover {`,
		`  color: #fff;`,
		`  margin: 4px;`,
		`}`,
	}
	spans := highlight.Highlight("style.css", lines)
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 0); got != "comment" {
		t.Errorf("comment: got %q", got)
	}
	if got := ix.CaptureAt(1, 1); got != "property" { // card (class name)
		t.Errorf("class selector: got %q, want property", got)
	}
	if got := ix.CaptureAt(1, 7); got != "attribute" { // hover
		t.Errorf("pseudo class: got %q, want attribute", got)
	}
	if got := ix.CaptureAt(2, 2); got != "property" { // color
		t.Errorf("property: got %q, want property", got)
	}
	if got := ix.CaptureAt(3, 10); got != "number" { // 4 of 4px
		t.Errorf("number: got %q, want number", got)
	}
	if got := ix.CaptureAt(3, 11); got != "number" { // px — uniform with its number
		t.Errorf("unit: got %q, want number", got)
	}
}

// TestHTMLHighlighting: tags/attributes plus the <script>/<style> injections
// (#925) — a template with Jinja braces still highlights the HTML around
// them (the grammar is error-tolerant).
func TestHTMLHighlighting(t *testing.T) {
	lines := []string{
		`<!doctype html>`,
		`<div class="{{ url_for('admin.index') }}">Text</div>`,
		`<script>const n = 1;</script>`,
		`<style>.a { color: red; }</style>`,
	}
	spans := highlight.Highlight("admin.html", lines)
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 0); got != "constant" { // doctype
		t.Errorf("doctype: got %q, want constant", got)
	}
	if got := ix.CaptureAt(1, 1); got != "tag" { // div
		t.Errorf("tag: got %q, want tag", got)
	}
	if got := ix.CaptureAt(1, 5); got != "attribute" { // class
		t.Errorf("attribute: got %q, want attribute", got)
	}
	if got := ix.CaptureAt(2, 8); got != "keyword" { // const, injected JS
		t.Errorf("injected script keyword: got %q, want keyword", got)
	}
	if got := ix.CaptureAt(3, 12); got != "property" { // color, injected CSS
		t.Errorf("injected style property: got %q, want property", got)
	}
}

// TestHTMLAttributeInjections guards #2329: inline code inside attribute
// values highlights with its own grammar — event handlers as JavaScript,
// style as CSS — while the attribute names, the quotes and every non-code
// attribute keep their plain HTML colouring.
func TestHTMLAttributeInjections(t *testing.T) {
	lines := []string{
		`<button class="btn" onclick="alert(1)" style="color: red" data-id="7">Go</button>`,
	}
	spans := highlight.Highlight("page.html", lines)
	ix := highlight.NewIndex(spans)

	// Event handler: the call expression is JavaScript inside the quotes.
	col := strings.Index(lines[0], "alert(1)")
	if got := ix.CaptureAt(0, col); got != "function" {
		t.Errorf("onclick callee: got %q, want function", got)
	}
	if got := ix.CaptureAt(0, col+len("alert(")); got != "number" {
		t.Errorf("onclick argument: got %q, want number", got)
	}
	// The quotes around the handler stay HTML — the injection covers only the
	// inner attribute_value node.
	if got := ix.CaptureAt(0, col-1); got == "function" || got == "number" {
		t.Errorf("opening quote: got %q, want the host's own capture", got)
	}

	// style: a bare declaration list still reads as CSS (it parses inside the
	// synthetic rule the css wrapper supplies).
	col = strings.Index(lines[0], "color: red")
	if got := ix.CaptureAt(0, col); got != "property" {
		t.Errorf("style property: got %q, want property", got)
	}
	if got := ix.CaptureAt(0, col+len("color")); got != "punctuation.delimiter" {
		t.Errorf("style colon: got %q, want punctuation.delimiter", got)
	}

	// Non-code attributes are untouched: their values stay the host's string.
	for _, attr := range []string{"btn", "7"} {
		col = strings.Index(lines[0], `"`+attr+`"`) + 1
		if got := ix.CaptureAt(0, col); got != "string" {
			t.Errorf("%s value: got %q, want string", attr, got)
		}
	}
	// …and so do their names.
	if got := ix.CaptureAt(0, strings.Index(lines[0], "class")); got != "attribute" {
		t.Errorf("class name: got %q, want attribute", got)
	}
}

// TestHTMLAttributeInjectionScope: the on* rule matches handler names only.
// Attributes that merely begin with "on" (once, only) and hyphenated
// framework attributes stay plain, while an upper-case handler still injects
// — HTML attribute names are case-insensitive.
func TestHTMLAttributeInjectionScope(t *testing.T) {
	lines := []string{
		`<p once="x" only="y" data-on-tap="z" ONCLICK="f()">t</p>`,
	}
	frags := highlight.Fragments("html", lines)
	if len(frags) != 1 {
		t.Fatalf("Fragments = %+v, want only the ONCLICK handler", frags)
	}
	f := frags[0]
	if f.Lang != "typescript" || !f.Partial {
		t.Errorf("fragment = %+v, want a partial typescript fragment", f)
	}
	if got := lines[0][f.StartCol:f.EndCol]; got != "f()" {
		t.Errorf("fragment text = %q, want f()", got)
	}
}

// TestHTMLAttributeFragmentsArePartial: attribute values are snippets, not
// documents (#2329) — the flag is what routes the CSS one through its wrapper
// and what keeps the LSP virtual-document seam from mirroring either. The
// <script>/<style> element bodies stay whole documents.
func TestHTMLAttributeFragmentsArePartial(t *testing.T) {
	lines := []string{
		`<div style="color: red" onclick="f()"></div>`,
		`<style>.a { color: red; }</style>`,
	}
	frags := highlight.Fragments("html", lines)
	if len(frags) != 3 {
		t.Fatalf("Fragments = %d, want 3: %+v", len(frags), frags)
	}
	partial := map[string]bool{}
	for _, f := range frags {
		partial[strings.Join(f.Lines, "\n")] = f.Partial
	}
	want := map[string]bool{
		"color: red":         true,  // the style attribute
		"f()":                true,  // the onclick handler
		".a { color: red; }": false, // the <style> element body
	}
	for text, w := range want {
		got, ok := partial[text]
		if !ok {
			t.Errorf("no fragment for %q", text)
			continue
		}
		if got != w {
			t.Errorf("fragment %q: Partial = %v, want %v", text, got, w)
		}
	}
}

// TestHTMLEntitiesSurviveAttributeInjection: entity concealment (#1620) is a
// Go-side Spans hook layered over the grammar's spans, and it still wins
// inside an injected handler value.
func TestHTMLEntitiesSurviveAttributeInjection(t *testing.T) {
	lines := []string{`<p onclick="say(&amp;)">x</p>`}
	spans := highlight.Highlight("page.html", lines)
	col := strings.Index(lines[0], "&amp;")
	for _, s := range spans {
		if s.Line == 0 && s.StartCol == col && s.Replace == "&" {
			return
		}
	}
	t.Errorf("no entity replacement span at col %d: %+v", col, spans)
}
