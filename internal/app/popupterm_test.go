package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/registry"
	"ike/internal/terminal"
)

// openTestPopup toggles the popup terminal open in a sized model and returns
// the model; the popup's sessions are ended on cleanup.
func openTestPopup(t *testing.T) Model {
	t.Helper()
	return openTestPopupWith(t, sized(t, 100, 40))
}

func openTestPopupWith(t *testing.T, m Model) Model {
	t.Helper()
	out, _ := m.Update(TerminalPopupMsg{})
	m = out.(Model)
	if !m.popup.open || m.popup.inst == nil {
		t.Fatal("terminal.popup should open the popup with a live instance")
	}
	inst := m.popup.inst
	t.Cleanup(func() { inst.CloseTerminalTabs() })
	return m
}

func TestPopupTerminalToggle(t *testing.T) {
	m := openTestPopup(t)
	if m.popup.inst.TabCount() != 1 {
		t.Fatalf("first toggle should spawn one shell tab, got %d", m.popup.inst.TabCount())
	}
	if v := m.render(); !strings.Contains(v, "POPUP TERMINAL") {
		t.Fatal("open popup should render its chrome")
	}
	sess := m.popup.inst.ActiveTerminal().SessionKey()

	// Hide: state survives, rendering stops.
	out, _ := m.Update(TerminalPopupMsg{})
	m = out.(Model)
	if m.popup.open {
		t.Fatal("second toggle should hide the popup")
	}
	if m.popup.inst == nil || m.popup.inst.TabCount() != 1 {
		t.Fatal("hiding must retain the instance and its tabs")
	}
	if v := m.render(); strings.Contains(v, "POPUP TERMINAL") {
		t.Fatal("hidden popup must not render")
	}
	// Output while hidden still resolves to the popup's terminal.
	if m.terminalModelForSession(sess) == nil {
		t.Fatal("hidden popup sessions must stay resolvable for OutputMsg")
	}

	// Reopen: same instance, same session.
	out, _ = m.Update(TerminalPopupMsg{})
	m = out.(Model)
	if !m.popup.open || m.popup.inst.ActiveTerminal().SessionKey() != sess {
		t.Fatal("reopening must reveal the same session")
	}
}

func TestPopupTerminalKeysBypassGlobalHandling(t *testing.T) {
	m := openTestPopup(t)
	// 'q' must go to the popup's shell, not quit the app.
	out, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = out.(Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if _, isQuit := msg.(tea.QuitMsg); isQuit {
				t.Fatal("q in the popup terminal must not quit")
			}
		}
	}
	if !m.popup.open {
		t.Fatal("popup should survive plain keys")
	}
}

// TestPopupToggleChord: cmd+alt+t opens the popup through the keymap layer and
// hides it again from inside through the reserved-set handler (#1398).
func TestPopupToggleChord(t *testing.T) {
	m := sizedWith(t, registry.Global(), 100, 40)
	m = drainKey(m, tea.KeyPressMsg{Code: 't', Mod: tea.ModSuper | tea.ModAlt})
	if !m.popup.open || m.popup.inst == nil {
		t.Fatal("cmd+alt+t should open the popup terminal")
	}
	inst := m.popup.inst
	t.Cleanup(func() { inst.CloseTerminalTabs() })

	m = drainKey(m, tea.KeyPressMsg{Code: 't', Mod: tea.ModSuper | tea.ModAlt})
	if m.popup.open {
		t.Fatal("cmd+alt+t inside the popup should hide it")
	}
	if m.popup.inst == nil {
		t.Fatal("hiding via chord must retain the instance")
	}
}

// TestTerminalNewChordMoved: terminal.new moved to cmd+alt+shift+t when the
// popup took cmd+alt+t (#1398).
func TestTerminalNewChordMoved(t *testing.T) {
	m := sizedWith(t, registry.Global(), 100, 40)
	m = drainKey(m, tea.KeyPressMsg{Code: 't', Mod: tea.ModSuper | tea.ModAlt | tea.ModShift})
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.ActiveTerminal() == nil {
		t.Fatalf("cmd+alt+shift+t should open a terminal pane, focused %q", key)
	}
	t.Cleanup(func() { inst.ActiveTerminal().Close() })
}

func TestPopupTerminalTabs(t *testing.T) {
	m := openTestPopup(t)
	// cmd+t opens a sibling tab inside the popup.
	out, _ := m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModSuper})
	m = out.(Model)
	if m.popup.inst.TabCount() != 2 {
		t.Fatalf("cmd+t should add a popup tab, got %d", m.popup.inst.TabCount())
	}
	if m.popup.inst.ActiveTab() != 1 {
		t.Fatal("the new tab should be active")
	}
	// ctrl+tab cycles.
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModCtrl})
	m = out.(Model)
	if m.popup.inst.ActiveTab() != 0 {
		t.Fatalf("ctrl+tab should cycle popup tabs, active %d", m.popup.inst.ActiveTab())
	}
}

func TestPopupShellExitClosesTabThenPopup(t *testing.T) {
	m := openTestPopup(t)
	out, _ := m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModSuper})
	m = out.(Model)
	first := m.popup.inst.TabTerminal(0).SessionKey()
	second := m.popup.inst.TabTerminal(1).SessionKey()

	out, _ = m.Update(terminal.ExitedMsg{Key: first})
	m = out.(Model)
	if m.popup.inst == nil || m.popup.inst.TabCount() != 1 {
		t.Fatal("a popup shell exit should close only its tab")
	}
	out, _ = m.Update(terminal.ExitedMsg{Key: second})
	m = out.(Model)
	if m.popup.inst != nil || m.popup.open {
		t.Fatal("the last popup shell exit should drop the instance and hide")
	}
	// The next toggle starts fresh.
	m = openTestPopupWith(t, m)
	if m.popup.inst.TabCount() != 1 {
		t.Fatal("reopening after the last exit should spawn a fresh shell")
	}
}

func TestPopupOutsideClickHides(t *testing.T) {
	m := openTestPopup(t)
	m = step(m, press(0, 0)) // far outside the centered box
	if m.popup.open {
		t.Fatal("a press outside the popup should hide it")
	}
	if m.popup.inst == nil {
		t.Fatal("outside-click hide must retain the instance")
	}
}

func TestPopupResizeDragPersistsDelta(t *testing.T) {
	m := openTestPopup(t)
	px, py, pw, ph := m.popupTermRect()
	// Grab the right border's middle cell and drag outward.
	bx, by := px+pw-1, py+ph/2
	m = step(m, press(bx, by))
	if m.floatDrag == nil || m.floatDrag.kind != "popupterm" {
		t.Fatal("a border press should start a popupterm resize drag")
	}
	m = step(m, motion(bx+3, by))
	m = step(m, release(bx+3, by))
	if dw, _ := m.winSizes.Get(popupTermSizeKey); dw == 0 {
		t.Fatal("the drag should persist a width delta")
	}
	if _, _, w2, _ := m.popupTermRect(); w2 <= pw {
		t.Fatalf("the box should have grown, %d -> %d", pw, w2)
	}
}

func TestPopupPaletteOpensAbove(t *testing.T) {
	m := sizedWith(t, registry.Global(), 100, 40)
	m = openTestPopupWith(t, m)
	// The global palette chord stays with the IDE inside the popup (#805).
	m = drainKey(m, tea.KeyPressMsg{Code: 'a', Mod: tea.ModSuper | tea.ModShift})
	if !m.palette.IsOpen() {
		t.Fatal("cmd+shift+a inside the popup must open the palette")
	}
	if !m.popup.open {
		t.Fatal("the popup stays open underneath the palette")
	}
}
