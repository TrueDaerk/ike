package terminal

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// collector gathers async session msgs.
type collector struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (c *collector) send(msg tea.Msg) {
	c.mu.Lock()
	c.msgs = append(c.msgs, msg)
	c.mu.Unlock()
}

func (c *collector) has(pred func(tea.Msg) bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.msgs {
		if pred(m) {
			return true
		}
	}
	return false
}

// waitFor polls until cond holds or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// startSh spawns a plain /bin/sh session for grid assertions.
func startSh(t *testing.T, c *collector) *Session {
	t.Helper()
	s, err := StartSession("terminal", "/bin/sh", t.TempDir(), 80, 24, c.send)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func plainView(s *Session) string { return ansi.Strip(s.View()) }

func TestSessionEchoRendersOnGrid(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)

	for _, r := range "echo hello-grid\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "echo output", func() bool {
		return strings.Count(plainView(s), "hello-grid") >= 2 // echoed input + output
	})
	if !c.has(func(m tea.Msg) bool { _, ok := m.(OutputMsg); return ok }) {
		t.Fatal("output should raise coalesced OutputMsg notifications")
	}
}

func keyFor(r rune) vt.KeyPressEvent {
	if r == '\r' {
		return vt.KeyPressEvent{Code: vt.KeyEnter}
	}
	return vt.KeyPressEvent{Code: r, Text: string(r)}
}

func TestSessionResizePropagates(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)

	s.Resize(100, 30)
	for _, r := range "stty size\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "stty size output", func() bool {
		return strings.Contains(plainView(s), "30 100")
	})
}

func TestSessionExitLifecycle(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)

	for _, r := range "exit\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "exit msg", func() bool {
		return c.has(func(m tea.Msg) bool { _, ok := m.(ExitedMsg); return ok })
	})
	if s.Running() {
		t.Fatal("session should report not running after exit")
	}
	s.Close() // double close is safe
}

func TestSessionAltScreenAndColors(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)

	// Drive raw escape sequences through printf: color + cursor addressing.
	cmd := `printf '\033[2J\033[3;5H\033[31mRED-MARK\033[0m'` + "\r"
	for _, r := range cmd {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "styled output", func() bool {
		return strings.Contains(plainView(s), "RED-MARK")
	})
	// The styled view keeps the SGR color around the mark.
	if !strings.Contains(s.View(), "RED-MARK") {
		t.Fatal("styled view should contain the mark")
	}
	lines := strings.Split(plainView(s), "\n")
	if len(lines) < 3 || !strings.Contains(lines[2], "RED-MARK") {
		t.Fatalf("cursor addressing should place the mark on row 3, got %q", lines[2])
	}
}

func TestShellResolution(t *testing.T) {
	if got := Shell("/bin/zsh"); got != "/bin/zsh" {
		t.Fatalf("override should win, got %q", got)
	}
	t.Setenv("SHELL", "/bin/fish")
	if got := Shell(""); got != "/bin/fish" {
		t.Fatalf("$SHELL should apply, got %q", got)
	}
	t.Setenv("SHELL", "")
	if got := Shell(""); got != "/bin/sh" {
		t.Fatalf("fallback should be /bin/sh, got %q", got)
	}
}
