//go:build cgo

package langyaml

import (
	"strings"
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"

	// The injection test needs the shell grammar registered (#894): run:
	// scripts resolve through lang.ByID("shell") at overlay time.
	_ "ike/plugins/languages/shell"
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

// TestYAMLShellInjection (#1625): a CI workflow's run: script highlights with
// the shell grammar, composing with host YAML highlighting and the Spans
// hook (cron hints #1624) — the three layers stack without conflicts.
func TestYAMLShellInjection(t *testing.T) {
	lines := []string{
		`on:`,
		`  schedule:`,
		`    - cron: '0 3 * * *'`,
		`jobs:`,
		`  build:`,
		`    steps:`,
		`      - run: |`,
		`          echo building`,
		`      - run: make test`,
	}
	spans := highlight.Highlight("ci.yaml", lines)
	ix := highlight.NewIndex(spans)
	// Host YAML still highlights outside the fragments.
	if got := ix.CaptureAt(3, 0); got != "property" { // jobs key
		t.Errorf("yaml key: got capture %q, want property", got)
	}
	// Inside the block scalar the shell grammar takes over: echo is a command.
	col := strings.Index(lines[7], "echo")
	if got := ix.CaptureAt(7, col); got != "function" {
		t.Errorf("block-scalar command: got capture %q, want function", got)
	}
	// The inline run: value injects too.
	col = strings.Index(lines[8], "make")
	if got := ix.CaptureAt(8, col); got != "function" {
		t.Errorf("inline command: got capture %q, want function", got)
	}
	// The Spans hook still layers on top: the cron expression carries its
	// human-readable hint (#1624) alongside the injection.
	hinted := false
	for _, s := range spans {
		if s.Line == 2 && s.Replace != "" {
			hinted = true
			break
		}
	}
	if !hinted {
		t.Error("cron hint span missing when shell injection is active")
	}
}
