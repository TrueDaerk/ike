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

func TestIsSignatureTrigger(t *testing.T) {
	trig := []string{"(", ","}
	if !isSignatureTrigger("(", trig) || !isSignatureTrigger(",", trig) {
		t.Fatal("advertised chars should trigger")
	}
	if isSignatureTrigger(")", trig) || isSignatureTrigger("", trig) {
		t.Fatal("other chars must not trigger")
	}
}
