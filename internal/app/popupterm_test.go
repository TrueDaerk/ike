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

// splitTestPopup opens the popup and splits it via the reserved cmd+d (#1427).
func splitTestPopup(t *testing.T) Model {
	t.Helper()
	m := openTestPopup(t)
	out, _ := m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModSuper})
	m = out.(Model)
	if m.popup.split == nil {
		t.Fatal("cmd+d inside the popup should split it")
	}
	split := m.popup.split
	t.Cleanup(func() { split.CloseTerminalTabs() })
	return m
}

// TestPopupSplit: cmd+d splits the popup into two side-by-side shells (#1427),
// the fresh right side takes focus, and a second cmd+d is a no-op.
func TestPopupSplit(t *testing.T) {
	m := splitTestPopup(t)
	if !m.popup.focusRight {
		t.Fatal("the fresh split side should take focus")
	}
	if m.popupFocused() != m.popup.split {
		t.Fatal("popupFocused should resolve the right side")
	}
	left := m.popup.inst.ActiveTerminal().SessionKey()
	right := m.popup.split.ActiveTerminal().SessionKey()
	if left == right {
		t.Fatal("the split side must run its own shell session")
	}
	if v := m.render(); strings.Count(v, "POPUP TERMINAL") != 2 {
		t.Fatal("a split popup should render two side boxes")
	}
	// A second cmd+d is a no-op — only one split is supported.
	prev := m.popup.split
	out, _ := m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModSuper})
	m = out.(Model)
	if m.popup.split != prev {
		t.Fatal("a second cmd+d must not replace the split")
	}
	// cmd+t opens the sibling tab on the focused (right) side.
	out, _ = m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModSuper})
	m = out.(Model)
	if m.popup.split.TabCount() != 2 || m.popup.inst.TabCount() != 1 {
		t.Fatalf("cmd+t should add a tab on the focused side, got left=%d right=%d",
			m.popup.inst.TabCount(), m.popup.split.TabCount())
	}
}

// TestPopupSplitFocusKeys: the spatial focus keys (default ctrl+left/right)
// move the keyboard between the split sides (#1427).
func TestPopupSplitFocusKeys(t *testing.T) {
	m := splitTestPopup(t)
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl})
	m = out.(Model)
	if m.popup.focusRight {
		t.Fatal("ctrl+left should focus the left side")
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl})
	m = out.(Model)
	if !m.popup.focusRight {
		t.Fatal("ctrl+right should focus the right side")
	}
}

// TestPopupBroadcastToggle: cmd+shift+i mirrors input to both split sides
// (#1427) — the toggle needs a split, marks both titles, and routes pastes to
// every active shell.
func TestPopupBroadcastToggle(t *testing.T) {
	m := openTestPopup(t)
	// Unsplit the chord is a no-op.
	out, _ := m.Update(tea.KeyPressMsg{Code: 'i', Mod: tea.ModSuper | tea.ModShift})
	m = out.(Model)
	if m.popup.broadcast {
		t.Fatal("broadcast must not toggle without a split")
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModSuper})
	m = out.(Model)
	split := m.popup.split
	t.Cleanup(func() { split.CloseTerminalTabs() })
	out, _ = m.Update(tea.KeyPressMsg{Code: 'i', Mod: tea.ModSuper | tea.ModShift})
	m = out.(Model)
	if !m.popup.broadcast {
		t.Fatal("cmd+shift+i should enable broadcast on a split popup")
	}
	if got := len(m.popupInputTerminals()); got != 2 {
		t.Fatalf("broadcast input should target both shells, got %d", got)
	}
	if v := m.render(); strings.Count(v, "⇉") != 2 {
		t.Fatal("broadcast should mark both side titles")
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: 'i', Mod: tea.ModSuper | tea.ModShift})
	m = out.(Model)
	if m.popup.broadcast {
		t.Fatal("cmd+shift+i should toggle broadcast off again")
	}
	if got := len(m.popupInputTerminals()); got != 1 {
		t.Fatalf("without broadcast input targets the focused shell only, got %d", got)
	}
}

// TestPopupBroadcastBorders: while broadcast is on, both split sides render
// the focus border (#1592) so the shared-input state is obvious at a glance;
// toggling broadcast off restores the single focused border.
func TestPopupBroadcastBorders(t *testing.T) {
	m := splitTestPopup(t)
	focus := fgSGR(m.pal().BorderFocus)
	off := strings.Count(m.renderPopupTerm(), focus)
	if off == 0 {
		t.Fatal("the focused split side should render the focus border")
	}
	out, _ := m.Update(tea.KeyPressMsg{Code: 'i', Mod: tea.ModSuper | tea.ModShift})
	m = out.(Model)
	if on := strings.Count(m.renderPopupTerm(), focus); on != 2*off {
		t.Fatalf("broadcast should focus-border both sides: off=%d on=%d", off, on)
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: 'i', Mod: tea.ModSuper | tea.ModShift})
	m = out.(Model)
	if got := strings.Count(m.renderPopupTerm(), focus); got != off {
		t.Fatalf("toggling broadcast off should restore one focused border: off=%d got=%d", off, got)
	}
}

// TestPopupSplitCollapse: a side's last shell exit collapses the split back to
// a single box (#1427) — the surviving side keeps running, broadcast resets.
func TestPopupSplitCollapse(t *testing.T) {
	m := splitTestPopup(t)
	out, _ := m.Update(tea.KeyPressMsg{Code: 'i', Mod: tea.ModSuper | tea.ModShift})
	m = out.(Model)
	leftSess := m.popup.inst.ActiveTerminal().SessionKey()
	rightSess := m.popup.split.ActiveTerminal().SessionKey()

	// The right side's shell exits: the primary spans the box again.
	out, _ = m.Update(terminal.ExitedMsg{Key: rightSess})
	m = out.(Model)
	if m.popup.split != nil || m.popup.inst == nil || !m.popup.open {
		t.Fatal("the split side's exit should collapse to the primary")
	}
	if m.popup.broadcast || m.popup.focusRight {
		t.Fatal("collapsing must reset broadcast and focus")
	}
	if m.popup.inst.ActiveTerminal().SessionKey() != leftSess {
		t.Fatal("the primary session must survive the collapse")
	}

	// Split again, then the primary's shell exits: the right side is promoted.
	out, _ = m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModSuper})
	m = out.(Model)
	promoted := m.popup.split
	t.Cleanup(func() { promoted.CloseTerminalTabs() })
	out, _ = m.Update(terminal.ExitedMsg{Key: leftSess})
	m = out.(Model)
	if m.popup.inst != promoted || m.popup.split != nil {
		t.Fatal("the primary's exit should promote the split side")
	}
	if !m.popup.open {
		t.Fatal("the popup stays open across the promotion")
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
