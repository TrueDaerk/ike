//go:build cgo

package highlight

import "testing"

// captureOnLine returns the capture covering (line, col) from a freshly parsed
// span set, or "" — a small helper to assert real Tree-sitter output.
func captureAt(t *testing.T, spans []Span, line, col int) string {
	t.Helper()
	return NewIndex(spans).CaptureAt(line, col)
}

func TestHighlightGo(t *testing.T) {
	lines := []string{
		"package main",
		"",
		`func main() { println("hi") }`,
	}
	spans := Highlight("main.go", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for Go source, got none")
	}
	// "package" is a keyword.
	if got := captureAt(t, spans, 0, 0); got != "keyword" {
		t.Errorf("package keyword: got capture %q", got)
	}
	// The string literal "hi" should be a string capture (col of the quote).
	q := len(`func main() { println(`)
	if got := captureAt(t, spans, 2, q); got == "" {
		t.Errorf("expected a capture on the string literal at col %d", q)
	}
}

func TestHighlightPython(t *testing.T) {
	lines := []string{"def f():", "    return 1"}
	spans := Highlight("a.py", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for Python source, got none")
	}
	if got := captureAt(t, spans, 0, 0); got == "" { // "def"
		t.Error("expected a capture on the def keyword")
	}
}

func TestHighlightPHP(t *testing.T) {
	lines := []string{"<?php", "function f() { return 1; }"}
	spans := Highlight("a.php", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for PHP source, got none")
	}
}

func TestHighlightUnsupported(t *testing.T) {
	if spans := Highlight("a.md", []string{"# hi"}); spans != nil {
		t.Errorf("unsupported extension should yield nil spans, got %v", spans)
	}
}
