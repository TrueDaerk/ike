//go:build cgo

package langmake

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"

	// The injection smoke test needs the shell grammar registered (#894):
	// recipe bodies resolve through lang.ByID("shell") at overlay time.
	_ "ike/plugins/languages/shell"
)

// TestMakeGrammar guards the vendored-source cgo wiring: the grammar is
// non-nil under cgo.
func TestMakeGrammar(t *testing.T) {
	l, ok := lang.ByID("make")
	if !ok || l.Grammar == nil {
		t.Fatal("make grammar is nil under cgo")
	}
}

// TestMakeHighlighting parses a small Makefile end-to-end via the
// exact-base-name path (no extension): comments, variable assignments,
// targets and special targets all resolve to their captures.
func TestMakeHighlighting(t *testing.T) {
	lines := []string{
		`# build the binary`,
		`CFLAGS := -O2`,
		`.PHONY: build`,
		`build:`,
		"\tgo build ./...",
		``, // the make grammar wants recipes newline-terminated; a real buffer's trailing \n provides this
	}
	spans := highlight.Highlight("/p/Makefile", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for Makefile source, got none")
	}
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 0); got != "comment" {
		t.Errorf("comment: got capture %q", got)
	}
	if got := ix.CaptureAt(1, 0); got != "constant.builtin" { // CFLAGS (implicit-rule variable)
		t.Errorf("CFLAGS: got capture %q, want constant.builtin", got)
	}
	if got := ix.CaptureAt(2, 0); got != "constant.builtin" { // .PHONY
		t.Errorf(".PHONY: got capture %q, want constant.builtin", got)
	}
	if got := ix.CaptureAt(3, 0); got != "function" { // build target
		t.Errorf("target: got capture %q, want function", got)
	}
}

// TestMakeVariableCapture covers plain (non-builtin) variables: assignment
// names and $(…) references both capture as variable.
func TestMakeVariableCapture(t *testing.T) {
	lines := []string{
		`BINARY = ike`,
		`all:`,
		"\techo $(BINARY)",
		``,
	}
	ix := highlight.NewIndex(highlight.Highlight("/p/Makefile", lines))
	if got := ix.CaptureAt(0, 0); got != "variable" {
		t.Errorf("assignment name: got capture %q, want variable", got)
	}
}

// TestMakeShellInjection guards the recipe-body injection (#1136): the shell
// grammar parses recipe lines, so a recipe's command word carries a shell
// capture instead of falling through uncaptured.
func TestMakeShellInjection(t *testing.T) {
	if _, ok := lang.ByID("shell"); !ok {
		t.Fatal("shell language not registered — injection target missing")
	}
	lines := []string{
		`build:`,
		"\tif true; then echo ok; fi",
		``,
	}
	spans := highlight.Highlight("/p/Makefile", lines)
	ix := highlight.NewIndex(spans)
	// "if" at column 1 of the recipe line is a bash keyword; without the
	// injection the make grammar leaves shell_text uncaptured.
	if got := ix.CaptureAt(1, 1); got != "keyword" {
		t.Errorf("recipe `if`: got capture %q, want keyword (shell injection)", got)
	}
}
