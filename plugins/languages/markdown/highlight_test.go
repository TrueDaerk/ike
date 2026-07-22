//go:build cgo

package langmarkdown

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"

	// Registers Go, the fence language of the injection test below.
	_ "ike/plugins/languages/go"
)

// TestMarkdownGrammars guards the vendored-source cgo wiring: both grammars
// are non-nil under cgo.
func TestMarkdownGrammars(t *testing.T) {
	for _, id := range []string{"markdown", "markdown_inline"} {
		l, ok := lang.ByID(id)
		if !ok || l.Grammar == nil {
			t.Errorf("%s: grammar is nil under cgo", id)
		}
	}
}

// TestMarkdownBlockHighlighting: block-level structure — headings and fence
// delimiters — resolves from the block grammar alone.
func TestMarkdownBlockHighlighting(t *testing.T) {
	lines := []string{
		`# Title`,
		``,
		`Some prose here.`,
	}
	spans := highlight.Highlight("README.md", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for markdown source, got none")
	}
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 0); got != "punctuation" { // the # marker
		t.Errorf("heading marker: got capture %q, want punctuation", got)
	}
	if got := ix.CaptureAt(0, 2); got != "function" { // Title
		t.Errorf("heading text: got capture %q, want function", got)
	}
}

// TestMarkdownInlineInjection is the block→inline seam (#880): inline styles
// (a code span, link text) appear inside block nodes via the
// @fragment.markdown_inline injection.
func TestMarkdownInlineInjection(t *testing.T) {
	lines := []string{
		"Use `go build` to compile, see [docs](https://go.dev).",
	}
	spans := highlight.Highlight("README.md", lines)
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 5); got != "string" { // go build code span
		t.Errorf("code span: got capture %q, want string", got)
	}
	if got := ix.CaptureAt(0, 32); got != "label" { // docs link text
		t.Errorf("link text: got capture %q, want label", got)
	}
	if got := ix.CaptureAt(0, 38); got != "attribute" { // the URL
		t.Errorf("link destination: got capture %q, want attribute", got)
	}
}

// TestMarkdownConcealAndMarkupCaptures guards the #881 query side: the inline
// grammar emits @conceal spans on the marker chrome and markup.* spans on the
// emphasis nodes the editor turns into text attributes.
func TestMarkdownConcealAndMarkupCaptures(t *testing.T) {
	line := "**bold** and `code` and [docs](https://go.dev)"
	spans := highlight.Highlight("README.md", []string{line})
	has := func(capture string, col int) bool {
		for _, s := range spans {
			if s.Capture == capture && s.Line == 0 && col >= s.StartCol && col < s.EndCol {
				return true
			}
		}
		return false
	}
	if !has("conceal", 0) || !has("conceal", 6) { // the ** pairs
		t.Error("emphasis delimiters not captured as conceal")
	}
	if !has("markup.bold", 2) { // bold
		t.Error("strong emphasis not captured as markup.bold")
	}
	if !has("conceal", 13) { // opening backtick
		t.Error("code span delimiter not captured as conceal")
	}
	if !has("conceal", 24) || !has("conceal", 30) { // [ and ] + ( of the link
		t.Error("link brackets not captured as conceal")
	}
	if !has("conceal", 32) { // the URL
		t.Error("link destination not captured as conceal")
	}
	if !has("label", 25) { // docs
		t.Error("link text lost its label capture")
	}
}

// TestMarkdownFencedCodeInjection is the dynamic fence injection (#880): a
// ```go block is parsed with the registered Go grammar — the fence tag names
// the language at query-match time, not in the capture name.
func TestMarkdownFencedCodeInjection(t *testing.T) {
	lines := []string{
		"# Example",
		"",
		"```go",
		`func main() {`,
		`	println("hi")`,
		"}",
		"```",
	}
	spans := highlight.Highlight("README.md", lines)
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(3, 0); got != "keyword" { // func
		t.Errorf("fenced go func: got capture %q, want keyword", got)
	}
	if got := ix.CaptureAt(3, 5); got != "function" { // main
		t.Errorf("fenced go func name: got capture %q, want function", got)
	}
	if got := ix.CaptureAt(4, 10); got != "string" { // "hi"
		t.Errorf("fenced go string: got capture %q, want string", got)
	}
	// The fence delimiter stays block-grammar territory.
	if got := ix.CaptureAt(2, 0); got != "punctuation" {
		t.Errorf("fence delimiter: got capture %q, want punctuation", got)
	}
	// An unknown fence language leaves the host's own styling.
	unknown := highlight.Highlight("README.md", []string{"```nosuchlang", "xyz", "```"})
	uix := highlight.NewIndex(unknown)
	if got := uix.CaptureAt(1, 0); got != "embedded" {
		t.Errorf("unknown fence content: got capture %q, want embedded", got)
	}
}
