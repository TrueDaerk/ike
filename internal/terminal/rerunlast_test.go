package terminal

import (
	"strings"
	"testing"
)

// rerunlast_test.go covers #2543's delivery half: RerunLast is the seam
// terminal.rerunLast writes through — Up + Enter into a shell at its prompt,
// nothing anywhere else.

// TestRerunLastRepeatsThePreviousCommand: after one command ran, RerunLast
// runs it again — the marker shows up a second time in the command's own
// output (echoed line + output per run, so four occurrences in total).
func TestRerunLastRepeatsThePreviousCommand(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	m := Model{sess: s, h: 24, w: 80}
	waitPrompt(t, s)

	if !m.PasteToShell("echo rerun-marker", true) {
		t.Fatal("setup: PasteToShell should succeed on a live session")
	}
	waitFor(t, "first run", func() bool {
		return strings.Count(plainView(s), "rerun-marker") >= 2
	})
	if !m.RerunLast() {
		t.Fatal("RerunLast should report success for a shell at its prompt")
	}
	waitFor(t, "second run", func() bool {
		return strings.Count(plainView(s), "rerun-marker") >= 4
	})
	if !m.occupied {
		t.Fatal("a re-run is input to the shell and must mark the session occupied")
	}
}

// TestRerunLastRefusesWithoutPrompt: no session, or a command session (0350)
// that never has a prompt — nothing is sent and the caller is told so, which
// is what lets the app toast instead of typing into a running program.
func TestRerunLastRefusesWithoutPrompt(t *testing.T) {
	var none Model
	if none.RerunLast() {
		t.Fatal("RerunLast must report failure without a session")
	}
	if none.AtPrompt() {
		t.Fatal("a model without a session is never at a prompt")
	}

	c := &collector{}
	s, err := StartCommandSession("terminal", []string{"/bin/sh", "-c", "sleep 5"}, t.TempDir(), 80, 24, nil, c.send)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	m := Model{sess: s, h: 24, w: 80}
	if m.AtPrompt() {
		t.Fatal("a command session is never at a prompt")
	}
	if m.RerunLast() {
		t.Fatal("RerunLast must refuse a command session")
	}
	if m.occupied {
		t.Fatal("a refused re-run must not mark the session occupied")
	}
}
