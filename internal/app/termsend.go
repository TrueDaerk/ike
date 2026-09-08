package app

import (
	"strings"

	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/terminal"
)

// termsend.go is the bridge from the editor into the shell (#2542):
// `terminal.sendSelection` and `terminal.sendSelectionRun` hand the visual
// selection — or, with nothing selected, the caret's line — to a terminal
// instead of making the user copy it, focus a shell and paste it there.
//
// The telemetry behind it: `terminal.popup` is the single most dispatched
// command, and the dispatch that precedes it is usually an editor action. The
// popup was already the "run this next to the code" surface; this closes the
// last manual step of that loop.
//
// Two decisions are worth recording:
//
//   - The payload travels as a **bracketed paste** (terminal.PasteToShell),
//     never as key presses. A selection is arbitrary text: embedded newlines
//     typed one by one would make the shell run each fragment as its own
//     command, and shell-special characters would be subject to the line
//     editor's own bindings. The `Run` flavour's newline is consequently an
//     Enter key press *after* the paste — inside the brackets it would be
//     inserted literally rather than submitting.
//   - The target order (`terminalSendTarget`) prefers what is already on
//     screen over what would have to be created: the popup's focused tab when
//     the popup is open, else the focused terminal pane, else the popup is
//     opened for the purpose. Sending never moves the keyboard away from the
//     editor when a shell is already there — the round trip
//     select → send → keep editing is the point.

// terminalSendResult reports what a send did, for the tests and the caller's
// notification.
type terminalSendResult int

const (
	termSendNoText  terminalSendResult = iota // nothing selected, caret on a blank line
	termSendNoShell                           // a target existed but its child is gone
	termSendOK
)

// sendSelectionToTerminal is the body of terminal.sendSelection (run=false)
// and terminal.sendSelectionRun (run=true).
func (m *Model) sendSelectionToTerminal(run bool) terminalSendResult {
	text := m.terminalSendText()
	if strings.TrimSpace(text) == "" {
		m.host.Notify(host.Info, "terminal: nothing to send — select some text or put the caret on a non-empty line")
		return termSendNoText
	}
	term := m.terminalSendTarget()
	if term == nil || !term.PasteToShell(text, run) {
		m.host.Notify(host.Warn, "terminal: no live shell to send to")
		return termSendNoShell
	}
	return termSendOK
}

// terminalSendText is the payload source: the active editor's visual
// selection, or — with no selection — the whole line the caret sits on.
//
// The current-line fallback is what makes the command usable without visual
// mode at all: "run the line I am looking at" is the shape most of these
// sends have. Trailing whitespace is dropped so a line ending in spaces does
// not arrive as a command with a dangling argument separator; leading
// indentation is kept, since a shell ignores it and a here-doc body does not.
func (m Model) terminalSendText() string {
	ed := m.activeEditor()
	if ed == nil {
		return ""
	}
	if sel, ok := ed.SelectionText(); ok {
		return strings.TrimRight(sel, " \t\r\n")
	}
	line, _ := ed.Cursor() // 1-based
	return strings.TrimRight(ed.LineText(line-1), " \t\r\n")
}

// terminalSendTarget resolves the shell the payload goes to, opening the popup
// terminal as the last resort. The order is "the shell the user is looking
// at, else give them one":
//
//  1. the popup terminal's focused tab while the popup layer is open — open
//     includes *blurred* (#2309), i.e. visible with the keyboard in the
//     editor, which is exactly the state this command is used from;
//  2. the focused terminal pane (a custom tool pane, #741/#772, is not one:
//     its shell belongs to the tool, not to the user);
//  3. otherwise the popup is opened — spawning its first shell when it has
//     none — and the payload goes there.
//
// Only the third case moves focus, because there the popup appearing *is* the
// answer to the command; the first two leave the editor with the keyboard.
func (m *Model) terminalSendTarget() *terminal.Model {
	if m.popup.open {
		if inst := m.popupFocused(); inst != nil {
			if t := inst.ActiveTerminal(); t != nil {
				return t
			}
		}
	}
	if inst := m.activeWS().Panes.FocusedInstance(); inst != nil && inst.Kind() == pane.KindTerminal && inst.Terminal().Tool() == "" {
		if t := inst.ActiveTerminal(); t != nil {
			return t
		}
	}
	m.ensurePopupTerminalOpen()
	if inst := m.popupFocused(); inst != nil {
		return inst.ActiveTerminal()
	}
	return nil
}
