package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/layout"
)

// resizeModeModel is a two-pane workspace (the default explorer + editor
// split) with the explorer focused and the mode armed.
func resizeModeModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	tm, _ := m.Update(PaneResizeModeMsg{})
	m = tm.(Model)
	if !m.resizeMode {
		t.Fatal("pane.resizeMode must arm the mode")
	}
	return m
}

// TestResizeModeEnterStepsAndExit (#2150): one chord enters the mode, plain
// h/j/k/l and arrows resize repeatedly without re-typing it, and esc leaves.
func TestResizeModeEnterStepsAndExit(t *testing.T) {
	m := resizeModeModel(t)
	focused := m.activeWS().Panes.Focused()
	before := m.lay.Panes[focused].W

	for i := 0; i < 3; i++ {
		m = drainKey(m, tea.KeyPressMsg{Code: 'l', Text: "l"})
		if !m.resizeMode {
			t.Fatalf("step %d left the mode", i)
		}
	}
	grown := m.lay.Panes[focused].W
	if grown != before+3 {
		t.Fatalf("three l steps: width %d → %d, want +3", before, grown)
	}

	// The arrows are the same operation.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if w := m.lay.Panes[focused].W; w != grown-1 {
		t.Fatalf("left arrow: width %d → %d, want -1", grown, w)
	}

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.resizeMode {
		t.Fatal("esc must leave resize mode")
	}
	// The geometry survives leaving the mode.
	if w := m.lay.Panes[focused].W; w != before+2 {
		t.Fatalf("width after leaving = %d, want %d", w, before+2)
	}
}

// TestResizeModeEnterExitByEnter: enter is the second documented exit key.
func TestResizeModeEnterExitByEnter(t *testing.T) {
	m := resizeModeModel(t)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.resizeMode {
		t.Fatal("enter must leave resize mode")
	}
}

// TestResizeModeVerticalSteps: j/k move the horizontal edge of a stacked
// split, so the vertical pair resizes too.
func TestResizeModeVerticalSteps(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	tm, _ := m.Update(ToggleExplorerFocusMsg{}) // focus the editor
	m = tm.(Model)
	tm, _ = m.Update(SplitFocusedMsg{Zone: layout.ZoneBottom})
	m = tm.(Model)
	bottom := m.activeWS().Panes.Focused()
	tm, _ = m.Update(PaneResizeModeMsg{})
	m = tm.(Model)

	before := m.lay.Panes[bottom].H
	m = drainKey(m, tea.KeyPressMsg{Code: 'k', Text: "k"})
	if h := m.lay.Panes[bottom].H; h != before+1 {
		t.Fatalf("k on the bottom pane: height %d → %d, want +1", before, h)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if h := m.lay.Panes[bottom].H; h != before {
		t.Fatalf("j must undo the k step: height = %d, want %d", h, before)
	}
}

// TestResizeModeOtherKeysAreInert (#2150): while the mode is armed no other
// key reaches an editor or a command — no accidental edits, and the mode
// stays put.
func TestResizeModeOtherKeysAreInert(t *testing.T) {
	m := resizeModeModel(t)
	focused := m.activeWS().Panes.Focused()
	before := m.lay.Panes[focused].W

	for _, k := range []tea.KeyPressMsg{
		{Code: 'a', Text: "a"},
		{Code: 'x', Text: "x"},
		{Code: 'i', Text: "i"},
		{Code: 'p', Mod: tea.ModCtrl},
	} {
		m = drainKey(m, k)
		if !m.resizeMode {
			t.Fatalf("%v left the mode", k)
		}
	}
	if w := m.lay.Panes[focused].W; w != before {
		t.Fatalf("inert keys changed the width: %d → %d", before, w)
	}
	if m.overlayCapturesKeyboard() {
		t.Fatal("no command may run while the mode owns the keyboard")
	}
	if ed := m.activeEditor(); ed != nil && ed.Dirty() {
		t.Fatal("an inert key must never edit a buffer")
	}
}

// TestResizeModeStatusHint: the mode is visible while active and gone after.
func TestResizeModeStatusHint(t *testing.T) {
	m := resizeModeModel(t)
	line := m.statusLine()
	if !strings.Contains(line, "RESIZE") || !strings.Contains(line, "esc") {
		t.Fatalf("status line must announce the mode and its exit, got %q", line)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if strings.Contains(m.statusLine(), "RESIZE") {
		t.Fatal("the banner must disappear with the mode")
	}
}

// TestResizeModeRefusesWithoutSplit: a maximized pane has no divider to move,
// so the mode declines to arm instead of swallowing the keyboard.
func TestResizeModeRefusesWithoutSplit(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	tm, _ := m.Update(MaximizePaneMsg{})
	m = tm.(Model)
	tm, _ = m.Update(PaneResizeModeMsg{})
	if m = tm.(Model); m.resizeMode {
		t.Fatal("resize mode must not arm on a maximized pane")
	}
}
