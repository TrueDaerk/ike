package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/project"
)

// popuppin_test.go covers the pinned popup terminal (#2406): pinning keeps the
// box on screen while the keyboard goes back to the panes, the toggle chord
// becomes a focus switch there, unpinning hides it again, and the pinned box
// is a bottom-edge strip rather than the centered overlay. The pin state rides
// with the workspace, so a project resumed after a switch comes back as left.

// unmodaled dismisses whatever first-start dialog the fresh model queued (the
// welcome tour, the LSP onboarding, #658): they own the modal shell and would
// paint over the popup box these tests assert on.
func unmodaled(t *testing.T, m Model) Model {
	t.Helper()
	for i := 0; i < 4 && m.shell.IsOpen(); i++ {
		out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = out.(Model)
	}
	if m.shell.IsOpen() {
		t.Fatal("a first-start dialog stayed open; the popup assertions below would read its box")
	}
	return m
}

func TestPopupPinKeepsVisibleAndTogglesFocus(t *testing.T) {
	m := openTestPopupWith(t, unmodaled(t, sized(t, 100, 40)))
	out, _ := m.Update(TerminalPopupPinMsg{})
	m = out.(Model)
	if !m.popup.pinned || !m.popup.open || m.popup.blurred {
		t.Fatalf("pinning must leave the popup open and focused, got %+v", m.popup)
	}

	// The toggle chord now blurs instead of hiding: the box stays on screen
	// while the panes below own the keyboard.
	out, _ = m.Update(TerminalPopupMsg{})
	m = out.(Model)
	if !m.popup.open || !m.popup.blurred {
		t.Fatal("the toggle chord must move focus off a pinned popup, not hide it")
	}
	if v := m.render(); !strings.Contains(v, "POPUP TERMINAL") {
		t.Fatal("a pinned popup must stay visible while the editor has the keyboard")
	}

	// And back: the same chord returns the keyboard to the popup.
	out, _ = m.Update(TerminalPopupMsg{})
	m = out.(Model)
	if !m.popup.open || m.popup.blurred {
		t.Fatal("the toggle chord must hand the keyboard back to the pinned popup")
	}

	// Unpinning hides it again — the state the plain toggle chord manages.
	out, _ = m.Update(TerminalPopupPinMsg{})
	m = out.(Model)
	if m.popup.pinned || m.popup.open {
		t.Fatalf("unpinning must hide the popup, got %+v", m.popup)
	}
	if v := m.render(); strings.Contains(v, "POPUP TERMINAL") {
		t.Fatal("the unpinned, hidden popup must not render")
	}
	if m.popup.inst == nil {
		t.Fatal("unpinning must retain the instance and its shells, like hiding does")
	}
}

// TestPinnedPopupAnchorsToBottomEdge: pinned, the box is a full-width strip at
// the bottom of the screen instead of the centered overlay (#2406).
func TestPinnedPopupAnchorsToBottomEdge(t *testing.T) {
	m := openTestPopupWith(t, unmodaled(t, sized(t, 100, 40)))
	_, _, cw, _ := m.popupTermRect()
	if cw >= m.width {
		t.Fatalf("test setup: the unpinned box should be narrower than the screen, got %d of %d", cw, m.width)
	}
	out, _ := m.Update(TerminalPopupPinMsg{})
	m = out.(Model)
	x, y, w, h := m.popupTermRect()
	if x != 0 || w != m.width {
		t.Fatalf("pinned box = x %d w %d, want the full width from column 0 (%d)", x, w, m.width)
	}
	if y+h != m.height {
		t.Fatalf("pinned box bottom = %d, want the screen bottom %d", y+h, m.height)
	}
	if y == 0 {
		t.Fatal("the pinned strip must leave the panes above it visible")
	}
	// The persisted height is what the strip uses, so a resize still applies.
	_, wantH := m.popupSize()
	if h != wantH {
		t.Fatalf("pinned height = %d, want the persisted popup height %d", h, wantH)
	}
}

// TestPopupPinRestoredAfterSwitch: the pinned state parks with the workspace
// (#1407 payload) and comes back on resume, while the other project's popup is
// unaffected (#2406).
func TestPopupPinRestoredAfterSwitch(t *testing.T) {
	a, b := twoRoots(t)
	m := openTestPopupWith(t, unmodaled(t, switchModel(t)))
	out, _ := m.Update(TerminalPopupPinMsg{})
	m = out.(Model)
	if !m.popup.pinned {
		t.Fatal("test setup: the popup must be pinned before switching away")
	}

	out, _ = m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	if m.popup.pinned || m.popup.open {
		t.Fatal("the incoming project must start with its own unpinned popup")
	}

	out, _ = m.Update(project.SwitchProjectMsg{Root: a})
	m = out.(Model)
	if !m.popup.pinned || !m.popup.open {
		t.Fatalf("switching back must restore the pinned, visible popup, got %+v", m.popup)
	}
	if v := unmodaled(t, m).render(); !strings.Contains(v, "POPUP TERMINAL") {
		t.Fatal("the restored pinned popup must render again")
	}
}
