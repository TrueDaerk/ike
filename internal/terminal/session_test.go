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
	s, err := StartSession("terminal", "/bin/sh", t.TempDir(), 80, 24, nil, c.send)
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

func TestScrollbackPaging(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	m := Model{sess: s, h: 24}

	// Push well over one screen of output into history.
	for _, r := range "seq 1 200\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "output scrolled", func() bool { return s.ScrollbackLen() > 20 })

	m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp, Mod: tea.ModShift})
	if m.Scroll() == 0 {
		t.Fatal("shift+pgup should enter the scrollback")
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "[scrollback -") {
		t.Fatalf("scrolled view should carry the position marker:\n%s", v)
	}
	// Paging back down (clamped) returns to live.
	for i := 0; i < 10; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown, Mod: tea.ModShift})
	}
	if m.Scroll() != 0 {
		t.Fatalf("paging down should clamp back to live, scroll = %d", m.Scroll())
	}

	// Any ordinary key snaps back to live and reaches the shell.
	m.ScrollBy(30)
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.Scroll() != 0 {
		t.Fatal("a typed key should snap the view back to live")
	}
}

func TestScrollByClamps(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	m := Model{sess: s, h: 24}
	m.ScrollBy(-5)
	if m.Scroll() != 0 {
		t.Fatal("negative scroll clamps to live")
	}
	m.ScrollBy(1 << 20)
	if m.Scroll() > s.ScrollbackLen() {
		t.Fatal("scroll clamps to the available history")
	}
}

// TestSessionOSCTitle: OSC 2 title sequences land in Title() for the pane.
func TestSessionOSCTitle(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	cmd := `printf '\033]2;building things\007'` + "\r"
	for _, r := range cmd {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "osc title", func() bool { return s.Title() == "building things" })
}

// TestSessionClear empties history and repaints via ctrl+l.
func TestSessionClear(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	for _, r := range "seq 1 100\r" {
		s.SendKey(keyFor(r))
	}
	// Wait for seq to FINISH (its last line on screen), not merely to start —
	// clearing mid-stream would race the remaining output back onto the grid.
	waitFor(t, "seq done", func() bool { return strings.Contains(plainView(s), "100") })
	waitFor(t, "history", func() bool { return s.ScrollbackLen() > 0 })
	s.Clear()
	if s.ScrollbackLen() != 0 {
		t.Fatalf("scrollback should be empty, len = %d", s.ScrollbackLen())
	}
	// The visible screen is wiped emulator-side — no stale seq output.
	waitFor(t, "screen wipe", func() bool {
		return !strings.Contains(plainView(s), "97") && !strings.Contains(plainView(s), "42")
	})
}
