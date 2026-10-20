//go:build windows

package terminal

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The ConPTY tests (#3005) drive cmd.exe on a real pseudo console and are
// what the Windows CI job runs: the interactive shell echoes typed input,
// a resize reaches the console, a command session shows its output and
// exit code, and Close ends a running child. They are self-contained — the
// unix helpers (requirePTY, waitFor) probe /bin/sh, which does not exist
// here.

// conptyWait polls cond for up to ten seconds; the hosted Windows runner
// starts conhost and cmd.exe noticeably slower than a unix pty spawns sh.
func conptyWait(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// conptyText is the screen plus scrollback as plain text, so output that
// has already scrolled off the grid still counts.
func conptyText(s *Session) string {
	var b strings.Builder
	for y := 0; y < s.ScrollbackLen(); y++ {
		b.WriteString(ansi.Strip(s.HistoryLine(y)))
		b.WriteByte('\n')
	}
	b.WriteString(ansi.Strip(s.View()))
	return b.String()
}

func conptyShell(t *testing.T) string {
	t.Helper()
	if s := os.Getenv("COMSPEC"); s != "" {
		return s
	}
	return "cmd.exe"
}

func TestConPTYShellEchoesInputAndResizes(t *testing.T) {
	c := &collector{}
	s, err := StartSession("terminal", conptyShell(t), t.TempDir(), 80, 24, nil, c.send)
	if err != nil {
		t.Fatalf("spawn on ConPTY: %v", err)
	}
	t.Cleanup(s.Close)
	if s.Pid() == 0 {
		t.Fatal("the adopted process has no pid")
	}
	conptyWait(t, "the cmd.exe prompt", func() bool { return strings.Contains(plainView(s), ">") })

	for _, r := range "echo hello-conpty\r" {
		s.SendKey(keyFor(r))
	}
	conptyWait(t, "typed input echoed and the command's output", func() bool {
		return strings.Count(conptyText(s), "hello-conpty") >= 2
	})

	// A width change is applied from the trailing debounce timer; the console
	// must report the new size to the child afterwards.
	s.Resize(100, 30)
	time.Sleep(2 * resizeQuiet)
	for _, r := range "mode con\r" {
		s.SendKey(keyFor(r))
	}
	conptyWait(t, "mode con reporting the resized console", func() bool {
		text := conptyText(s)
		return strings.Contains(text, "Columns:") && strings.Contains(strings.ReplaceAll(text, " ", ""), "Columns:100")
	})
}

func TestConPTYCommandSessionShowsOutputAndExitCode(t *testing.T) {
	c := &collector{}
	s, err := StartCommandSession("run", []string{conptyShell(t), "/c", "echo run-output-ok & exit 3"}, t.TempDir(), 80, 24, nil, c.send)
	if err != nil {
		t.Fatalf("command spawn on ConPTY: %v", err)
	}
	t.Cleanup(s.Close)
	conptyWait(t, "the command's output", func() bool {
		return strings.Contains(conptyText(s), "run-output-ok")
	})
	conptyWait(t, "the exit code", func() bool {
		code, ok := s.ExitCode()
		return ok && code == 3
	})
	conptyWait(t, "ExitedMsg", func() bool {
		return c.has(func(m tea.Msg) bool { _, ok := m.(ExitedMsg); return ok })
	})
}

func TestConPTYCloseEndsARunningChild(t *testing.T) {
	c := &collector{}
	s, err := StartSession("terminal", conptyShell(t), t.TempDir(), 80, 24, nil, c.send)
	if err != nil {
		t.Fatalf("spawn on ConPTY: %v", err)
	}
	conptyWait(t, "the cmd.exe prompt", func() bool { return strings.Contains(plainView(s), ">") })
	proc := s.cmd.Process
	start := time.Now()
	s.Close()
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Close took %s; release must not block on the console", d)
	}
	conptyWait(t, "the killed shell to be reaped", func() bool {
		_, exited := s.ExitCode()
		return exited
	})
	if err := proc.Signal(os.Kill); err == nil {
		t.Fatal("the shell still accepts signals after Close killed it")
	}
}
