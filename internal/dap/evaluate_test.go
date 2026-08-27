package dap

import (
	"errors"
	"testing"
)

// TestEvaluateHoverCapability decodes supportsEvaluateForHovers, the flag the
// evaluate popup picks its context hint from (#2174).
func TestEvaluateHoverCapability(t *testing.T) {
	s, _ := startSession(t, map[string]any{"supportsEvaluateForHovers": true})
	if !s.SupportsEvaluateForHovers() {
		t.Fatal("supportsEvaluateForHovers not decoded")
	}
	plain, _ := startSession(t, nil)
	if plain.SupportsEvaluateForHovers() {
		t.Fatal("hover capability invented without the flag")
	}
}

// TestEvaluateSupportedByDefault: DAP has no initialize capability for
// evaluate, so support is optimistic until an adapter says otherwise (#2174).
func TestEvaluateSupportedByDefault(t *testing.T) {
	s, _ := startSession(t, nil)
	if !s.SupportsEvaluate() {
		t.Fatal("evaluate gated off before any adapter refusal")
	}
}

// TestEvaluateUnsupportedLatches: an adapter refusing evaluate as
// unimplemented turns the capability off for the rest of the session, and
// later calls answer from the verdict without another round trip (#2174).
func TestEvaluateUnsupportedLatches(t *testing.T) {
	s, fa := startSession(t, nil)
	fa.mu.Lock()
	fa.evalRefuse = "unsupported request: evaluate"
	fa.mu.Unlock()

	if _, err := s.Evaluate("x", 1, "watch"); !errors.Is(err, ErrEvaluateUnsupported) {
		t.Fatalf("first evaluate error = %v, want ErrEvaluateUnsupported", err)
	}
	if s.SupportsEvaluate() {
		t.Fatal("capability still on after the adapter refused evaluate")
	}
	if _, err := s.Evaluate("y", 1, "repl"); !errors.Is(err, ErrEvaluateUnsupported) {
		t.Fatalf("second evaluate error = %v, want ErrEvaluateUnsupported", err)
	}
	fa.mu.Lock()
	n := fa.evalCount
	fa.mu.Unlock()
	if n != 1 {
		t.Fatalf("adapter saw %d evaluate requests, want 1 — the verdict must be sticky", n)
	}
}

// TestEvaluateErrorDoesNotLatch: a bad expression is an ordinary failure —
// one mistyped watch must never disable watches (#2174).
func TestEvaluateErrorDoesNotLatch(t *testing.T) {
	s, fa := startSession(t, nil)
	fa.mu.Lock()
	fa.evalRefuse = "undefined variable nope"
	fa.mu.Unlock()

	_, err := s.Evaluate("nope", 1, "watch")
	if err == nil {
		t.Fatal("a failing expression must surface its error")
	}
	if errors.Is(err, ErrEvaluateUnsupported) {
		t.Fatalf("expression error mistaken for a missing capability: %v", err)
	}
	if !s.SupportsEvaluate() {
		t.Fatal("a bad expression disabled the whole feature")
	}
}

// TestUnsupportedRequestPhrasings pins the classifier the latch rides on.
func TestUnsupportedRequestPhrasings(t *testing.T) {
	for _, msg := range []string{
		"dap: evaluate: unsupported request: evaluate",
		"dap: evaluate: Unsupported command",
		"dap: evaluate: evaluate is not supported",
		"dap: evaluate: unimplemented",
		"dap: evaluate: not implemented",
		"dap: evaluate: unknown command evaluate",
	} {
		if !unsupportedRequest(msg) {
			t.Errorf("%q not classified as a missing request", msg)
		}
	}
	for _, msg := range []string{
		"dap: evaluate: could not find symbol value for x",
		"dap: evaluate: unknown variable nope",
		"dap: evaluate: timeout",
	} {
		if unsupportedRequest(msg) {
			t.Errorf("%q wrongly classified as a missing request", msg)
		}
	}
}
