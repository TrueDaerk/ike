package app

import (
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/terminal"
)

// termrerun.go is terminal.rerunLast (#2543): repeat the previous shell
// command without going to the shell. The telemetry behind it: the popup
// terminal was opened 137 times in four days, and a good share of those were
// opened, `up`, `enter`, closed — a round trip whose only variable part is
// the two keys. Run configurations cover file runs, not the ad-hoc shell
// command the user last typed.
//
// The shape is #2542's send-to-terminal with the payload replaced by a
// history recall: the target order is the same (what is on screen wins over
// what would have to be created), delivery is `terminal.Model.RerunLast`
// (Up + Enter through the session's key encoder), and the keyboard never
// moves unless the popup had to be created for the purpose.
//
// Two additions the payload-less command needs:
//
//   - **Prompt gating.** Up + Enter into a foreground program (a build, vim, a
//     REPL) would do whatever those keys mean there, so the send only happens
//     while the shell itself owns the terminal (`Session.AtPrompt`, #1340's
//     gate reused). A busy shell gets a toast instead, and nothing is typed.
//   - **The hidden popup.** A retained popup layer that is currently hidden is
//     still the shell the user last used, so the re-run goes there without
//     showing the layer, and the statusbar activity indicator (#2309) is
//     armed by hand: the output that follows would arm it anyway, but a
//     command that prints nothing (a redeploy, a `make` with nothing to do)
//     would otherwise leave no trace that anything happened.

// terminalRerunResult reports what a re-run did, for the tests and the
// caller's notification.
type terminalRerunResult int

const (
	termRerunNoShell terminalRerunResult = iota // no live shell to type into
	termRerunBusy                               // the shell is not at its prompt
	termRerunOK
)

// rerunLastInTerminal is the body of terminal.rerunLast.
func (m *Model) rerunLastInTerminal() terminalRerunResult {
	term, hidden := m.terminalRerunTarget()
	if term == nil || !term.Running() {
		m.host.Notify(host.Warn, "terminal: no live shell to re-run in")
		return termRerunNoShell
	}
	if !term.AtPrompt() {
		m.host.Notify(host.Info, "terminal: the shell is busy — nothing re-run")
		return termRerunBusy
	}
	if !term.RerunLast() {
		// The prompt check raced a program taking the terminal over.
		m.host.Notify(host.Info, "terminal: the shell is busy — nothing re-run")
		return termRerunBusy
	}
	if hidden {
		m.popupUnseen = true
	}
	return termRerunOK
}

// terminalRerunTarget resolves the shell the re-run goes to and reports
// whether that shell sits in the hidden popup layer. The order extends
// #2542's terminalSendTarget by one step for the popup's retained state:
//
//  1. the popup terminal's focused tab while the popup layer is open —
//     blurred included (#2309);
//  2. the focused terminal pane (a custom tool pane, #741/#772, is not one);
//  3. the popup terminal's focused tab while the layer is *hidden* but
//     retained: that shell is the one the user last worked in, and the whole
//     point of the command is not having to bring it up — the activity
//     indicator (#2309) shows the re-run happened;
//  4. otherwise the popup is opened, spawning its first shell, and the re-run
//     goes there. A fresh shell inherits its history file, so Up still recalls
//     the last command the user ran in one.
//
// Only the fourth case moves focus, because there the popup appearing is the
// visible answer to the command.
func (m *Model) terminalRerunTarget() (t *terminal.Model, hidden bool) {
	if m.popup.open {
		if inst := m.popupFocused(); inst != nil {
			if t := inst.ActiveTerminal(); t != nil {
				return t, false
			}
		}
	}
	if inst := m.activeWS().Panes.FocusedInstance(); inst != nil && inst.Kind() == pane.KindTerminal && inst.Terminal().Tool() == "" {
		if t := inst.ActiveTerminal(); t != nil {
			return t, false
		}
	}
	if m.popup.inst != nil || len(m.floatTerms) > 0 {
		if inst := m.popupFocused(); inst != nil {
			if t := inst.ActiveTerminal(); t != nil {
				return t, true
			}
		}
	}
	m.ensurePopupTerminalOpen()
	if inst := m.popupFocused(); inst != nil {
		return inst.ActiveTerminal(), false
	}
	return nil, false
}
