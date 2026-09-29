package editor

import "testing"

// TestPasteIntoPromptOnlyTakesPrompts: the prompt-only entry point (#2772)
// fills an open search line like PasteText does, and with no prompt open it
// declines and leaves the buffer alone — the host places the block instead.
func TestPasteIntoPromptOnlyTakesPrompts(t *testing.T) {
	m, _ := loaded(t, "alpha beta\n")
	if m.PasteIntoPrompt("beta") {
		t.Fatal("no prompt open: PasteIntoPrompt must decline")
	}
	if line(m, 0) != "alpha beta" {
		t.Fatalf("a declined paste touched the buffer: %q", line(m, 0))
	}
	m = send(m, key('/'))
	if !m.PasteIntoPrompt("be\nta") {
		t.Fatal("open search line: PasteIntoPrompt must take the paste")
	}
	if m.cmdline != "be ta" || line(m, 0) != "alpha beta" {
		t.Fatalf("cmdline=%q buffer=%q, want the flattened block on the search line only", m.cmdline, line(m, 0))
	}
	if m.PasteIntoPrompt("") {
		t.Error("an empty paste must not count as taken")
	}
}
