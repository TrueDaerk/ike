package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
	"ike/internal/terminal"
)

// openTestTerminal opens a terminal pane in a sized model and returns its key.
func openTestTerminal(t *testing.T) (Model, string) {
	t.Helper()
	m := sized(t, 100, 40)
	out, _ := m.Update(TerminalNewMsg{})
	m = out.(Model)
	key := m.panes.Focused()
	inst := m.panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatalf("terminal.new should focus a terminal pane, got %q", key)
	}
	t.Cleanup(func() { inst.Terminal().Close() })
	return m, key
}

func TestTerminalNewSplitsAndFocuses(t *testing.T) {
	m, key := openTestTerminal(t)
	if !strings.HasPrefix(key, "terminal") {
		t.Fatalf("key = %q", key)
	}
	if !m.terminalFocused() {
		t.Fatal("terminalFocused should report the live session")
	}
	// The view renders the pane; give the shell a moment to draw its prompt.
	time.Sleep(200 * time.Millisecond)
	if v := m.render(); !strings.Contains(v, "TERMINAL") {
		t.Fatal("pane chrome should title the terminal")
	}
}

func TestTerminalKeysBypassGlobalHandling(t *testing.T) {
	m, key := openTestTerminal(t)
	// 'q' must go to the shell, not quit the app.
	out, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = out.(Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if _, isQuit := msg.(tea.QuitMsg); isQuit {
				t.Fatal("q in a terminal must not quit")
			}
		}
	}
	if !m.panes.Has(key) {
		t.Fatal("terminal pane should survive q")
	}
	// ctrl+tab is the escape hatch: focus moves away.
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModCtrl})
	m = out.(Model)
	if m.panes.Focused() == key {
		t.Fatal("ctrl+tab should move focus away from the terminal")
	}
}

func TestTerminalExitClosesPane(t *testing.T) {
	m, key := openTestTerminal(t)
	out, _ := m.Update(terminal.ExitedMsg{Key: key})
	m = out.(Model)
	if m.panes.Has(key) {
		t.Fatal("an exited terminal's pane should close")
	}
	if m.panes.Focused() == key {
		t.Fatal("focus should land elsewhere")
	}
}
