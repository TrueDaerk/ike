//go:build cgo

package langtoml

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// TestTOMLGrammar guards the cgo wiring: the grammar is non-nil under cgo.
func TestTOMLGrammar(t *testing.T) {
	l, ok := lang.ByID("toml")
	if !ok || l.Grammar == nil {
		t.Fatal("toml grammar is nil under cgo")
	}
}

// TestTOMLHighlighting parses a small document end-to-end.
func TestTOMLHighlighting(t *testing.T) {
	lines := []string{
		`# ike config`,
		`[editor]`,
		`tab_width = 4`,
		`theme = "default"`,
	}
	spans := highlight.Highlight("config.toml", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for TOML source, got none")
	}
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 0); got != "comment" {
		t.Errorf("comment: got capture %q", got)
	}
	if got := ix.CaptureAt(1, 1); got != "type" { // editor (table header)
		t.Errorf("table header: got capture %q, want type", got)
	}
	if got := ix.CaptureAt(2, 0); got != "property" { // tab_width
		t.Errorf("key: got capture %q, want property", got)
	}
	if got := ix.CaptureAt(2, 12); got != "number" { // 4
		t.Errorf("number: got capture %q, want number", got)
	}
	if got := ix.CaptureAt(3, 9); got != "string" { // "default"
		t.Errorf("string: got capture %q, want string", got)
	}
}
