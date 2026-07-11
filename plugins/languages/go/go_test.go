//go:build cgo

package langgo

import (
	"testing"

	"ike/internal/highlight"
)

// The blank init() registers Go, so highlight.Highlight resolves the grammar.
func TestGoHighlighting(t *testing.T) {
	lines := []string{
		"package main",
		"",
		`func main() { println("hi") }`,
	}
	spans := highlight.Highlight("main.go", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for Go source, got none")
	}
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 0); got != "keyword" { // "package"
		t.Errorf("package keyword: got capture %q", got)
	}
}

// TestHighlightFenced guards #379: a hover markdown fence tag ("```go")
// resolves to the registered grammar, both as a language id and an extension.
func TestHighlightFenced(t *testing.T) {
	lines := []string{"func Printf(format string, a ...any) (n int, err error)"}
	for _, tag := range []string{"go", "Go"} {
		if spans := highlight.HighlightFenced(tag, lines); len(spans) == 0 {
			t.Errorf("HighlightFenced(%q) returned no spans", tag)
		}
	}
	if spans := highlight.HighlightFenced("no-such-lang", lines); spans != nil {
		t.Errorf("unknown fence tag should yield nil spans, got %d", len(spans))
	}
}
