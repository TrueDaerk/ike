package lsp

import (
	"testing"

	"ike/internal/host"
)

// TestCompletionWarranted covers the #527 trigger decision: server trigger
// characters always fire (with "." as the fallback while capabilities are
// unknown); identifier runes fire only with the as-you-type toggle on.
func TestCompletionWarranted(t *testing.T) {
	cases := []struct {
		name      string
		ch        string
		triggers  []string
		autoIdent bool
		want      bool
	}{
		{"server trigger char", ">", []string{">", ":"}, false, true},
		{"non-trigger punctuation", ";", []string{">", ":"}, true, false},
		{"fallback dot without caps", ".", nil, false, true},
		{"non-dot without caps", ",", nil, false, false},
		{"identifier rune, auto on", "p", nil, true, true},
		{"identifier rune, auto off", "p", nil, false, false},
		{"underscore, auto on", "_", nil, true, true},
		{"unicode letter, auto on", "ä", nil, true, true},
		{"digit never auto-triggers", "1", nil, true, false},
		{"space never triggers", " ", nil, true, false},
		{"multi-rune never ident-triggers", "ab", nil, true, false},
	}
	for _, c := range cases {
		if got := completionWarranted(c.ch, c.triggers, c.autoIdent); got != c.want {
			t.Errorf("%s: completionWarranted(%q, %v, %v) = %v, want %v",
				c.name, c.ch, c.triggers, c.autoIdent, got, c.want)
		}
	}
}

// TestCompletionAutoToggle covers the lsp.completion_auto config gate (#527):
// absent means enabled, matching the config default.
func TestCompletionAutoToggle(t *testing.T) {
	mk := func(cfg host.MapConfig) *bridge {
		return &bridge{h: host.New(cfg)}
	}
	if b := mk(host.MapConfig{}); !b.completionAutoEnabled() {
		t.Fatal("absent lsp.completion_auto must mean enabled")
	}
	if b := mk(host.MapConfig{"lsp.completion_auto": "false"}); b.completionAutoEnabled() {
		t.Fatal("lsp.completion_auto=false must disable the ident auto-trigger")
	}
	if !(&bridge{}).completionAutoEnabled() {
		t.Fatal("no host attached must mean enabled")
	}
}

// TestShouldCompleteManual: a char-less trigger (ctrl+space) is honoured even
// before any manager exists; a typed char without a manager is dropped.
func TestShouldCompleteManual(t *testing.T) {
	b := &bridge{}
	if !b.shouldComplete(host.EditorEvent{}) {
		t.Fatal("manual trigger must always request")
	}
	if b.shouldComplete(host.EditorEvent{Char: "."}) {
		t.Fatal("typed char without a manager must not request")
	}
}
