package app

// playpasteroute_test.go guards the #2236 paste routing: an open jq playground
// is not a modal — it may only consume a paste while its own pane holds the
// focus. With the popup terminal layer up (#1398, floating panels #1793) that
// layer owns the keyboard, and with the focus on another pane the paste
// belongs to that pane's editor or text input.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/layout"
)

// openJQOverFloatingTerminal opens the playground over a JSON buffer, then
// tears the popup terminal's only tab out into a focused floating panel — the
// issue's repro state: the playground's pane still holds the pane focus while
// the panel owns the keyboard.
func openJQOverFloatingTerminal(t *testing.T) Model {
	t.Helper()
	m := openJQ(t, playApp(t, `{"foo":[1,2,3]}`))
	m = setProgram(m, ".foo[]")
	m = openTestPopupWith(t, m)
	inst := m.popup.inst
	m.tearOutPopupTab(inst, 0, 30, 20)
	if len(m.floatTerms) != 1 || m.floatFocused() == nil {
		t.Fatal("test setup: the torn-out panel must own the popup layer's keyboard")
	}
	if !m.playFocused() {
		t.Fatal("test setup: the playground's pane still holds the pane focus")
	}
	return m
}

// TestPasteWithFloatingTerminalFocusedSkipsPlayground is the #2236 regression
// guard: the playground must not swallow a paste that belongs to the focused
// floating terminal panel.
func TestPasteWithFloatingTerminalFocusedSkipsPlayground(t *testing.T) {
	m := openJQOverFloatingTerminal(t)
	program := m.play.program.Text

	tm, cmd := m.Update(tea.PasteMsg{Content: "LEAK"})
	m = drainCmd(tm.(Model), cmd)

	if got := m.play.program.Text; got != program {
		t.Fatalf("query line = %q, the paste belongs to the focused panel", got)
	}
	if got := m.play.result.Text(); got != "1\n2\n3" {
		t.Errorf("result = %q, the paste must not re-run the query", got)
	}
}

// TestPasteWithPopupBoxOpenSkipsPlayground covers the popup box half of the
// layer, and that closing it hands the paste straight back to the playground.
func TestPasteWithPopupBoxOpenSkipsPlayground(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":[1,2,3]}`))
	m.play.program.Clear()
	m = openTestPopupWith(t, m)

	tm, cmd := m.Update(tea.PasteMsg{Content: ".foo"})
	m = drainCmd(tm.(Model), cmd)
	if got := m.play.program.Text; got != "" {
		t.Fatalf("query line = %q, the paste belongs to the popup shell", got)
	}

	// Hiding the layer restores the playground's claim on the keyboard.
	tm, cmd = m.Update(TerminalPopupMsg{})
	m = drainCmd(tm.(Model), cmd)
	tm, cmd = m.Update(tea.PasteMsg{Content: ".foo\n| length"})
	m = drainCmd(tm.(Model), cmd)
	if got := m.play.program.Text; got != ".foo | length" {
		t.Fatalf("query line = %q, want the flattened pasted program", got)
	}
}

// TestPasteWithOtherPaneFocusedSkipsPlayground: with the playground mounted in
// one pane and the focus in another editor pane, the paste is an ordinary
// editor insert.
func TestPasteWithOtherPaneFocusedSkipsPlayground(t *testing.T) {
	m := playApp(t, `{"foo":[1,2,3]}`)
	playKey := m.activeWS().Panes.Focused()
	m.SplitFocused(layout.ZoneRight)
	otherKey := m.activeWS().Panes.Focused()
	m.setFocus(playKey)
	m = openJQ(t, m)
	m = setProgram(m, ".foo[]")
	program := m.play.program.Text

	m.setFocus(otherKey)
	m = drainKey(m, tea.KeyPressMsg{Code: 'i', Text: "i"})
	tm, cmd := m.Update(tea.PasteMsg{Content: "PASTED"})
	m = drainCmd(tm.(Model), cmd)

	ed := m.activeWS().Panes.Get(otherKey).Editor()
	if ed == nil || !strings.Contains(ed.Text(), "PASTED") {
		t.Fatal("the paste must land in the focused editor pane")
	}
	if got := m.play.program.Text; got != program {
		t.Fatalf("query line = %q, it must not take another pane's paste", got)
	}
}

// TestPasteWithToolPaneFocusedSkipsPlayground: the same for a tool window with
// a text input of its own (#2002) — the issues filter.
func TestPasteWithToolPaneFocusedSkipsPlayground(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":[1,2,3]}`))
	program := m.play.program.Text

	k := m.activeWS().Panes.AddIssues()
	m = focusPane(t, m, k)
	gi := m.activeWS().Panes.Get(k).Issues()
	gi.Update(slash())
	if !gi.Filtering() {
		t.Fatal("test setup: '/' must open the issues filter")
	}

	tm, cmd := m.Update(tea.PasteMsg{Content: "bug"})
	m = drainCmd(tm.(Model), cmd)

	if !strings.Contains(gi.View(), "bug") {
		t.Fatalf("the issues filter did not take the paste:\n%s", gi.View())
	}
	if got := m.play.program.Text; got != program {
		t.Fatalf("query line = %q, it must not take the tool pane's paste", got)
	}
}
