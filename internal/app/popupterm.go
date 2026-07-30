package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"ike/internal/keymap"
	"ike/internal/pane"
	"ike/internal/terminal"
	"ike/internal/ui"
)

// popupterm.go — the popup terminal (#1398): a quake-style floating terminal
// overlay toggled by one chord, independent of the pane layout. It hosts a
// detached pane.Instance tab host (the #983 tab machinery) that lives outside
// every registry and outside the split tree, so toggling it never touches the
// workspace layout and its sessions keep running while hidden (the same
// goroutine independence toolhide relies on). Sessions never resurrect across
// app restarts; only the resize delta persists (via ui.WinSizes).

// popupTermSizeKey is the WinSizes delta key for the popup box (#774 store).
const popupTermSizeKey = "popupterm"

// Popup box geometry: fraction of the screen the box takes by default, and
// the floors a resize can never go below.
const (
	popupTermWFrac = 0.75
	popupTermHFrac = 0.70
	popupTermMinW  = 40
	popupTermMinH  = 10
)

// popupTerm is the popup terminal's state on the root model. The instance is
// created on first toggle and dropped when its last tab closes; open merely
// gates rendering and key routing.
type popupTerm struct {
	inst *pane.Instance // detached tab host; nil until first use
	open bool
	seq  int // key minting counter for "popup:term:N"
}

// togglePopupTerminal shows or hides the popup terminal (terminal.popup). The
// first show spawns a shell; later shows reveal the retained tabs unchanged.
// Pane focus is never moved — while the popup is open the key funnel routes to
// it before any pane, and hiding it simply falls back to the focused pane.
func (m *Model) togglePopupTerminal() {
	if m.popup.open {
		m.popup.open = false
		if m.popup.inst != nil {
			m.popup.inst.SetFocused(false)
		}
		return
	}
	if m.popup.inst == nil {
		term := m.newPopupShell()
		m.popup.inst = pane.NewDetachedTerminalHost("popup", term, m.host.Config(), m.pal())
	}
	m.popup.open = true
	m.applyPopupSize()
	m.popup.inst.SetFocused(true)
}

// newPopupShell spawns a fresh shell session for the popup, mirroring the
// pane-terminal creation recipe (shell config, toolchain env, host injector).
func (m *Model) newPopupShell() terminal.Model {
	shell := ""
	if v, ok := m.host.Config().Get("terminal.shell"); ok {
		shell = v
	}
	m.popup.seq++
	key := fmt.Sprintf("popup:term:%d", m.popup.seq)
	return terminal.New(key, terminal.Shell(shell), ".", 80, 24, terminalEnv(), m.host.Send)
}

// newPopupTerminalTab opens a sibling shell tab inside the popup (cmd+t there).
func (m *Model) newPopupTerminalTab() {
	if m.popup.inst == nil {
		return
	}
	m.popup.inst.AddTerminalTab(m.newPopupShell())
	m.applyPopupSize()
}

// popupSize computes the popup's outer box size: screen fractions capped by
// ui.popup_max_width, adjusted by the persisted resize delta, floored so the
// box never degenerates.
func (m Model) popupSize() (w, h int) {
	w = int(float64(m.width) * popupTermWFrac)
	h = int(float64(m.height) * popupTermHFrac)
	if cap := popupMaxWidth(); cap > 0 && w > cap {
		w = cap
	}
	maxW, maxH := m.width-2, m.height-2
	dw, dh := 0, 0
	if m.winSizes != nil {
		dw, dh = m.winSizes.Get(popupTermSizeKey)
	}
	w = ui.ClampDelta(w, dw, popupTermMinW, maxW)
	h = ui.ClampDelta(h, dh, popupTermMinH, maxH)
	return w, h
}

// applyPopupSize pushes the current box interior into the instance (and every
// tab's PTY). The popup lives outside the layout tree, so layout() never sizes
// it — every size-affecting event calls this instead.
func (m *Model) applyPopupSize() {
	if m.popup.inst == nil {
		return
	}
	w, h := m.popupSize()
	m.popup.inst.SetSize(paneInterior(w, paneChromeW), paneInterior(h, paneChromeH))
}

// popupTermRect is the popup box's screen rectangle (centered geometry), for
// mouse hit testing.
func (m Model) popupTermRect() (x, y, w, h int) {
	w, h = m.popupSize()
	return (m.width - w) / 2, (m.height - h) / 2, w, h
}

// renderPopupTerm renders the popup box: pane-style chrome (rounded border,
// title row) whose title row is the tab bar once multiple tabs exist, exactly
// like an editor pane hosting terminal tabs.
func (m Model) renderPopupTerm() string {
	inst := m.popup.inst
	w, h := m.popupSize()
	title := "POPUP TERMINAL"
	if t := inst.Tab(inst.ActiveTab()); t != nil && t.IsTerminal() {
		title = "POPUP TERMINAL — " + t.Title()
	}
	if bar, ok := m.tabBar(inst, w-paneChromeW); ok {
		title = bar
	}
	return paneBox(title, inst.View(), w, h, m.pal().BorderFocus)
}

// popupTabForSession resolves a session key to the popup tab hosting it;
// returns the tab index and its terminal model, or (-1, nil).
func (m Model) popupTabForSession(sess string) (int, *terminal.Model) {
	if m.popup.inst == nil {
		return -1, nil
	}
	for i := 0; i < m.popup.inst.TabCount(); i++ {
		if t := m.popup.inst.TabTerminal(i); t != nil && t.SessionKey() == sess {
			return i, t
		}
	}
	return -1, nil
}

// popupChordCommand resolves a single-step chord against the live binding
// table under the terminal context, so rebinds move the reserved popup chords
// along (the same rule terminalGlobalChord follows).
func (m Model) popupChordCommand(keys string) string {
	table := m.bindings.Table()
	if table == nil {
		return ""
	}
	k, err := keymap.ParseKey(keys)
	if err != nil {
		return ""
	}
	if b, found := table.Lookup(keymap.Chord{Steps: []keymap.Key{k}}, keymap.Context("terminal")); found {
		return b.Command
	}
	return ""
}

// popupReservedKey is the reserved chord set while the popup terminal owns the
// keyboard, mirroring terminalReservedKey: the toggle chord hides the popup
// from inside, cmd+t opens a sibling tab, tab-cycling and cmd+w act on the
// popup's tabs, and the floating-window resize chords (#774) adjust the box.
// Everything else belongs to the popup's shell.
func (m *Model) popupReservedKey(keys string) (bool, tea.Model, tea.Cmd) {
	if k, err := keymap.ParseKey(keys); err == nil {
		keys = k.String()
	}
	if !terminalShellChords[keys] {
		switch m.popupChordCommand(keys) {
		case "terminal.popup":
			m.togglePopupTerminal()
			return true, m, nil
		case "editor.tab.next":
			m.cyclePopupTab(1)
			return true, m, nil
		case "editor.tab.prev":
			m.cyclePopupTab(-1)
			return true, m, nil
		}
	}
	switch keys {
	case "cmd+t":
		// iTerm-style sibling tab, like the pane-terminal reserved cmd+t.
		m.newPopupTerminalTab()
		return true, m, nil
	case "ctrl+tab":
		m.cyclePopupTab(1)
		return true, m, nil
	case "cmd+w":
		m.requestPopupTabClose()
		return true, m, nil
	}
	if ddw, ddh, ok := ui.ResizeDelta(keys); ok {
		m.winSizes.Adjust(popupTermSizeKey, ddw, ddh)
		m.applyPopupSize()
		return true, m, nil
	}
	return false, m, nil
}

// cyclePopupTab activates the next/previous popup tab, wrapping around.
func (m *Model) cyclePopupTab(step int) {
	inst := m.popup.inst
	if inst == nil || inst.TabCount() < 2 {
		return
	}
	n := inst.TabCount()
	inst.ActivateTab(((inst.ActiveTab()+step)%n + n) % n)
}

// requestPopupTabClose handles the reserved cmd+w inside the popup (#986
// semantics): an idle shell gets an EOF — the exit path closes its tab — and
// a busy one raises the confirmation guard targeted at the popup.
func (m *Model) requestPopupTabClose() {
	inst := m.popup.inst
	if inst == nil {
		return
	}
	term := inst.ActiveTerminal()
	if term == nil {
		return
	}
	if term.Busy() {
		m.termClosePopup = true
		m.openTermClosePrompt()
		return
	}
	term.SendEOF()
}

// closePopupTab closes popup tab idx (session ends). Closing the last tab
// drops the whole instance and hides the popup — the next toggle starts a
// fresh shell, mirroring terminal.toggle's create-on-demand arm.
func (m *Model) closePopupTab(idx int) {
	inst := m.popup.inst
	if inst == nil {
		return
	}
	if inst.TabCount() <= 1 {
		inst.CloseTerminalTabs()
		m.popup.inst = nil
		m.popup.open = false
		return
	}
	inst.CloseTab(idx)
}
