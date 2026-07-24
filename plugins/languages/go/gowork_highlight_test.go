//go:build cgo

package langgo

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// TestGoWorkGrammar guards the dedicated go.work grammar wiring (#1119):
// go.work carries its own non-nil grammar under cgo, distinct from go.mod's.
func TestGoWorkGrammar(t *testing.T) {
	l, ok := lang.ByID("go.work")
	if !ok || l.Grammar == nil {
		t.Fatal("go.work grammar is nil under cgo")
	}
}

// TestGoWorkHighlighting parses a small go.work end-to-end via the
// exact-base-name path: the `use` directive — single and block form, the very
// gap #1119 closes (the gomod grammar error-recovered on it) — highlights,
// alongside go/replace/comments.
func TestGoWorkHighlighting(t *testing.T) {
	lines := []string{
		`// workspace`,
		`go 1.26`,
		``,
		`use ./tools`,
		``,
		`use (`,
		`	./app`,
		`	./lib`,
		`)`,
		``,
		`replace example.com/a => ../a`,
	}
	spans := highlight.Highlight("/p/go.work", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for go.work source, got none")
	}
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 0); got != "comment" {
		t.Errorf("comment: got capture %q", got)
	}
	if got := ix.CaptureAt(1, 0); got != "keyword" { // go
		t.Errorf("go: got capture %q, want keyword", got)
	}
	if got := ix.CaptureAt(3, 0); got != "keyword" { // use (single)
		t.Errorf("use: got capture %q, want keyword", got)
	}
	if got := ix.CaptureAt(3, 4); got != "type" { // ./tools
		t.Errorf("use path: got capture %q, want type", got)
	}
	if got := ix.CaptureAt(5, 0); got != "keyword" { // use (block)
		t.Errorf("use block: got capture %q, want keyword", got)
	}
	if got := ix.CaptureAt(6, 1); got != "type" { // ./app inside the block
		t.Errorf("block path: got capture %q, want type", got)
	}
	if got := ix.CaptureAt(10, 0); got != "keyword" { // replace
		t.Errorf("replace: got capture %q, want keyword", got)
	}
	if got := ix.CaptureAt(10, 22); got != "operator" { // =>
		t.Errorf("=>: got capture %q, want operator", got)
	}
}
