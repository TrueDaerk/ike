//go:build cgo

package langgo

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// TestGoModGrammar guards the vendored-source cgo wiring (#1078): go.mod
// carries a non-nil grammar under cgo (go.work has its own grammar since
// #1119, see gowork_highlight_test.go); go.sum stays plain (no grammar
// exists, content is hashes).
func TestGoModGrammar(t *testing.T) {
	l, ok := lang.ByID("go.mod")
	if !ok || l.Grammar == nil {
		t.Error("go.mod grammar is nil under cgo")
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

// go.work highlighting moved to gowork_highlight_test.go: since #1119 it has
// a dedicated grammar whose `use` directive the gomod grammar lacks.
