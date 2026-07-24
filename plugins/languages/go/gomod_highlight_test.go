//go:build cgo

package langgo

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// TestGoModGrammar guards the vendored-source cgo wiring (#1078): go.mod and
// go.work carry a non-nil grammar under cgo; go.sum stays plain (no grammar
// exists, content is hashes).
func TestGoModGrammar(t *testing.T) {
	for _, id := range []string{"go.mod", "go.work"} {
		l, ok := lang.ByID(id)
		if !ok || l.Grammar == nil {
			t.Errorf("%s grammar is nil under cgo", id)
		}
	}
	if l, ok := lang.ByID("go.sum"); !ok || l.Grammar != nil {
		t.Error("go.sum should stay plain (nil grammar)")
	}
}

// TestGoModHighlighting parses a small go.mod end-to-end via the
// exact-base-name path (no extension).
func TestGoModHighlighting(t *testing.T) {
	lines := []string{
		`// module metadata`,
		`module example.com/demo`,
		``,
		`go 1.26`,
		``,
		`require github.com/tree-sitter/go-tree-sitter v0.25.0`,
	}
	spans := highlight.Highlight("/p/go.mod", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for go.mod source, got none")
	}
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 0); got != "comment" {
		t.Errorf("comment: got capture %q", got)
	}
	if got := ix.CaptureAt(1, 0); got != "keyword" { // module
		t.Errorf("module keyword: got capture %q, want keyword", got)
	}
	if got := ix.CaptureAt(1, 7); got != "type" { // example.com/demo
		t.Errorf("module path: got capture %q, want type", got)
	}
	if got := ix.CaptureAt(3, 3); got != "string" { // 1.26
		t.Errorf("go version: got capture %q, want string", got)
	}
	if got := ix.CaptureAt(5, 0); got != "keyword" { // require
		t.Errorf("require keyword: got capture %q, want keyword", got)
	}
	if got := ix.CaptureAt(5, 8); got != "type" { // module path
		t.Errorf("require path: got capture %q, want type", got)
	}
	if got := ix.CaptureAt(5, 51); got != "string" { // v0.25.0
		t.Errorf("require version: got capture %q, want string", got)
	}
}

// TestGoWorkHighlighting: go.work shares the gomod grammar. The grammar has no
// `use` directive (use lines fall into error recovery), but go/toolchain
// directives and comments still highlight.
func TestGoWorkHighlighting(t *testing.T) {
	lines := []string{
		`go 1.26`,
		``,
		`// local modules`,
		`use ./demo`,
	}
	spans := highlight.Highlight("/p/go.work", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for go.work source, got none")
	}
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 0); got != "keyword" { // go
		t.Errorf("go keyword: got capture %q, want keyword", got)
	}
	if got := ix.CaptureAt(0, 3); got != "string" { // 1.26
		t.Errorf("go version: got capture %q, want string", got)
	}
	if got := ix.CaptureAt(2, 0); got != "comment" {
		t.Errorf("comment: got capture %q", got)
	}
}
