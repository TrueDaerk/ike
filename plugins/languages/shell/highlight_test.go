//go:build cgo

package langshell

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// TestShellGrammar guards the cgo wiring: the grammar is non-nil under cgo.
func TestShellGrammar(t *testing.T) {
	l, ok := lang.ByID("shell")
	if !ok || l.Grammar == nil {
		t.Fatal("shell grammar is nil under cgo")
	}
}

// TestShellHighlighting parses a small script end-to-end.
func TestShellHighlighting(t *testing.T) {
	lines := []string{
		`# greet`,
		`greet() {`,
		`  echo "hello"`,
		`}`,
		`if true; then`,
		`  greet`,
		`fi`,
	}
	spans := highlight.Highlight("run.sh", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for shell source, got none")
	}
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 0); got != "comment" {
		t.Errorf("comment: got capture %q", got)
	}
	if got := ix.CaptureAt(1, 0); got != "function" { // greet definition
		t.Errorf("function name: got capture %q, want function", got)
	}
	if got := ix.CaptureAt(2, 2); got != "function" { // echo (command)
		t.Errorf("command: got capture %q, want function", got)
	}
	if got := ix.CaptureAt(2, 8); got != "string" { // "hello"
		t.Errorf("string: got capture %q, want string", got)
	}
	if got := ix.CaptureAt(4, 0); got != "keyword" { // if
		t.Errorf("if keyword: got capture %q, want keyword", got)
	}
	if got := ix.CaptureAt(4, 9); got != "keyword" { // then
		t.Errorf("then keyword: got capture %q, want keyword", got)
	}
}
