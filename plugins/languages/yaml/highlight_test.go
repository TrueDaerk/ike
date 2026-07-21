//go:build cgo

package langyaml

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// TestYAMLGrammar guards the cgo wiring: the grammar is non-nil under cgo.
func TestYAMLGrammar(t *testing.T) {
	l, ok := lang.ByID("yaml")
	if !ok || l.Grammar == nil {
		t.Fatal("yaml grammar is nil under cgo")
	}
}

// TestYAMLHighlighting parses a small document end-to-end. The key assertions
// double as a guard for the query's capture order: mapping keys must resolve
// to property (the key pattern precedes the generic string capture — ike's
// span index is first-wins).
func TestYAMLHighlighting(t *testing.T) {
	lines := []string{
		`# deployment`,
		`name: ike`,
		`replicas: 3`,
		`active: true`,
		`command: "run"`,
	}
	spans := highlight.Highlight("deploy.yaml", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for YAML source, got none")
	}
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 0); got != "comment" {
		t.Errorf("comment: got capture %q", got)
	}
	if got := ix.CaptureAt(1, 0); got != "property" { // name key
		t.Errorf("key: got capture %q, want property", got)
	}
	if got := ix.CaptureAt(1, 6); got != "string" { // ike value
		t.Errorf("string value: got capture %q, want string", got)
	}
	if got := ix.CaptureAt(2, 10); got != "number" { // 3
		t.Errorf("number value: got capture %q, want number", got)
	}
	if got := ix.CaptureAt(3, 8); got != "boolean" { // true
		t.Errorf("boolean value: got capture %q, want boolean", got)
	}
	if got := ix.CaptureAt(4, 9); got != "string" { // "run"
		t.Errorf("quoted value: got capture %q, want string", got)
	}
}
