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
