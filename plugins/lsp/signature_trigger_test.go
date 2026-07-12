package lsp

import (
	"testing"

	"ike/internal/host"
)

func TestTypedChar(t *testing.T) {
	ev := host.EditorEvent{Text: "ab(\nsecond", Line: 0, Col: 3}
	if got := typedChar(ev); got != "(" {
		t.Fatalf("typedChar = %q", got)
	}
	if got := typedChar(host.EditorEvent{Text: "ab", Line: 0, Col: 0}); got != "" {
		t.Fatalf("col 0 should yield empty, got %q", got)
	}
	if got := typedChar(host.EditorEvent{Text: "ab", Line: 5, Col: 1}); got != "" {
		t.Fatalf("out-of-range line should yield empty, got %q", got)
	}
	// Unicode before the cursor.
	if got := typedChar(host.EditorEvent{Text: "π(", Line: 0, Col: 2}); got != "(" {
		t.Fatalf("unicode line typedChar = %q", got)
	}
}

// TestSignatureAutoAndInlayToggles covers the config gates (#523): the
// signature auto-popup defaults on (absent key), inlay hints default off.
func TestSignatureAutoAndInlayToggles(t *testing.T) {
	mk := func(cfg host.MapConfig) *bridge {
		return &bridge{h: host.New(cfg)}
	}
	if b := mk(host.MapConfig{}); !b.signatureAutoEnabled() {
		t.Fatal("absent lsp.signature_auto must mean enabled")
	}
	if b := mk(host.MapConfig{"lsp.signature_auto": "false"}); b.signatureAutoEnabled() {
		t.Fatal("lsp.signature_auto=false must disable the auto popup")
	}
	if b := mk(host.MapConfig{}); b.inlayHintsEnabled() {
		t.Fatal("absent lsp.inlay_hints must mean disabled (#523)")
	}
	if b := mk(host.MapConfig{"lsp.inlay_hints": "true"}); !b.inlayHintsEnabled() {
		t.Fatal("lsp.inlay_hints=true must enable hints")
	}
	if (&bridge{}).signatureAutoEnabled() != true {
		t.Fatal("no host attached must mean auto enabled")
	}
}

func TestIsSignatureTrigger(t *testing.T) {
	trig := []string{"(", ","}
	if !isSignatureTrigger("(", trig) || !isSignatureTrigger(",", trig) {
		t.Fatal("advertised chars should trigger")
	}
	if isSignatureTrigger(")", trig) || isSignatureTrigger("", trig) {
		t.Fatal("other chars must not trigger")
	}
}
