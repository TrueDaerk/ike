package terminal

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestCommandSessionRunsAndReportsExitCode verifies a command session (0350,
// #574) runs a program directly (no shell), streams its output, and keeps
// the exit code for the completion line.
func TestCommandSessionRunsAndReportsExitCode(t *testing.T) {
	c := &collector{}
	s, err := StartCommandSession("terminal", []string{"/bin/sh", "-c", "echo run-output; exit 3"}, t.TempDir(), 80, 24, nil, c.send)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	waitFor(t, "exit msg", func() bool {
		return c.has(func(m tea.Msg) bool { _, ok := m.(ExitedMsg); return ok })
	})
	if !strings.Contains(plainView(s), "run-output") {
		t.Fatal("command output must land on the grid")
	}
	code, ok := s.ExitCode()
	if !ok || code != 3 {
		t.Fatalf("ExitCode = %d/%v, want 3/true", code, ok)
	}
	if !s.IsCommand() {
		t.Fatal("IsCommand must report true for a command session")
	}
}

// TestShellSessionIsNotCommand confirms plain shells keep the old semantics.
func TestShellSessionIsNotCommand(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	if s.IsCommand() || s.Argv() != nil {
		t.Fatal("a shell session must not report as a command session")
	}
}

// TestStartCommandSessionEmptyArgv rejects an empty command line.
func TestStartCommandSessionEmptyArgv(t *testing.T) {
	if _, err := StartCommandSession("terminal", nil, ".", 80, 24, nil, nil); err == nil {
		t.Fatal("empty argv must fail")
	}
}

// TestModelOccupiedTracking verifies the reuse predicate (#574): fresh
// terminals are unoccupied; a forwarded key or paste occupies them; the
// scrollback paging keys do not.
func TestModelOccupiedTracking(t *testing.T) {
	c := &collector{}
	m := New("terminal", "/bin/sh", t.TempDir(), 80, 24, nil, c.send)
	t.Cleanup(m.Close)
	if m.Occupied() {
		t.Fatal("a fresh terminal must be unoccupied")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp, Mod: tea.ModShift})
	if m.Occupied() {
		t.Fatal("scrollback paging must not occupy the terminal")
	}
	m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if !m.Occupied() {
		t.Fatal("a forwarded key must occupy the terminal")
	}
}

// TestModelPasteOccupies verifies pasted text counts as input too.
func TestModelPasteOccupies(t *testing.T) {
	c := &collector{}
	m := New("terminal", "/bin/sh", t.TempDir(), 80, 24, nil, c.send)
	t.Cleanup(m.Close)
	m.PasteText("ls\n")
	if !m.Occupied() {
		t.Fatal("a paste must occupy the terminal")
	}
}

// TestModelStartCommandReplacesSession verifies the reuse path: StartCommand
// takes over the model with a fresh command session and resets occupancy.
func TestModelStartCommandReplacesSession(t *testing.T) {
	c := &collector{}
	m := New("terminal", "/bin/sh", t.TempDir(), 80, 24, nil, c.send)
	t.Cleanup(m.Close)
	m.StartCommand("terminal", []string{"/bin/sh", "-c", "echo took-over"}, t.TempDir(), nil)
	if m.Occupied() {
		t.Fatal("a replaced session starts unoccupied")
	}
	if !m.IsCommand() {
		t.Fatal("the replacement must be a command session")
	}
	waitFor(t, "replacement output", func() bool {
		return strings.Contains(plainView(m.sess), "took-over")
	})
	waitFor(t, "replacement exit", func() bool { _, ok := m.ExitCode(); return ok })
	if code, _ := m.ExitCode(); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}
