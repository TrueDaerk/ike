package terminal

import (
	"strings"
	"testing"
	"time"
)

// pastetoshell_test.go covers #2542's delivery half: PasteToShell is the seam
// the editor's "send selection to the terminal" writes through.

// waitPrompt blocks until the spawned shell has drawn its prompt. Pasting
// before it does races the shell's own startup echo, which would show the
// payload twice for reasons that have nothing to do with the send.
func waitPrompt(t *testing.T, s *Session) {
	t.Helper()
	waitFor(t, "shell prompt", func() bool { return strings.Contains(plainView(s), "$") })
}

// TestPasteToShellSubmits: PasteToShell hands the payload to the child and,
// with submit set, follows it with Enter — so the pasted command runs (the
// text shows twice: the echoed input line and the command's output).
func TestPasteToShellSubmits(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	m := Model{sess: s, h: 24, w: 80}
	waitPrompt(t, s)

	if !m.PasteToShell("echo alpha-beta", true) {
		t.Fatal("PasteToShell should report success for a live session")
	}
	waitFor(t, "pasted command ran", func() bool {
		return strings.Count(plainView(s), "alpha-beta") >= 2
	})
}

// TestPasteToShellWithoutSubmitStaysOnTheLine: the plain flavour leaves the
// payload sitting on the prompt line — it arrives, but nothing runs it. That
// is the whole difference between terminal.sendSelection and its Run twin.
func TestPasteToShellWithoutSubmitStaysOnTheLine(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	m := Model{sess: s, h: 24, w: 80}
	waitPrompt(t, s)

	if !m.PasteToShell("echo gamma-delta", false) {
		t.Fatal("PasteToShell should report success for a live session")
	}
	waitFor(t, "payload on the prompt line", func() bool {
		return strings.Contains(plainView(s), "gamma-delta")
	})
	// Give a would-be execution ample time to produce a second occurrence.
	time.Sleep(300 * time.Millisecond)
	if n := strings.Count(plainView(s), "gamma-delta"); n != 1 {
		t.Fatalf("payload occurrences = %d, want 1 (pasted, not run):\n%s", n, plainView(s))
	}
}

// TestPasteToShellMarksOccupied: a send counts as work in the terminal, like
// any other input, so the run-reuse bookkeeping (0350) does not hand the
// session out as idle right after one.
func TestPasteToShellMarksOccupied(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	m := Model{sess: s, h: 24, w: 80}

	if m.occupied {
		t.Fatal("a fresh session should not be occupied")
	}
	m.PasteToShell("echo occupied", true)
	if !m.occupied {
		t.Fatal("a send should mark the session occupied")
	}
}

// TestPasteToShellWithoutSessionReports guards the caller's fallback: a model
// with no live child says so instead of swallowing the payload.
func TestPasteToShellWithoutSessionReports(t *testing.T) {
	var m Model
	if m.PasteToShell("echo nothing", true) {
		t.Fatal("PasteToShell must report failure without a session")
	}
}
