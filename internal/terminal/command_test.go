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

// TestModelStartCommandReplacesSession verifies the in-place launch path
// (#1905): StartCommand takes the model over with a fresh command session.
func TestModelStartCommandReplacesSession(t *testing.T) {
	c := &collector{}
	m := New("terminal", "/bin/sh", t.TempDir(), 80, 24, nil, c.send)
	t.Cleanup(m.Close)
	m.StartCommand("terminal", []string{"/bin/sh", "-c", "echo took-over"}, t.TempDir(), nil)
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
