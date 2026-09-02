package debugpanel

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
)

// copy.go is the debug panel's copy key (#2400). Telemetry recorded cmd+c
// pressed in the debug panel and logged unbound: the panel showed variable
// values and console output but had no way to get either onto the clipboard.
// The keymap layer resolves debug.copy in the debug context and dispatches it
// here, mirroring httppane.CopyKeyCmd.

// CopyMsg asks the host to put Text on the system clipboard; What names the
// copied thing for the confirmation notice. The panel cannot reach the
// clipboard itself, like every other read-only surface (httppane.CopyMsg,
// ghissues.CopyMsg).
type CopyMsg struct {
	Text string
	What string
}

// CopyKeyCmd is debug.copy: with the console visible it copies the terminal's
// mouse selection, otherwise the focused column's selected row — a variable
// as "name = value" (the watch expression for a watch row) or the stack
// frame's "func — file:line". Nothing selected means nothing to copy and the
// chord stays inert rather than copying a whole panel nobody asked for.
func (m *Model) CopyKeyCmd() tea.Cmd {
	text, what := m.copyTarget()
	if text == "" {
		return nil
	}
	msg := CopyMsg{Text: text, What: what}
	return func() tea.Msg { return msg }
}

// copyTarget resolves what the copy key acts on in the panel's current state.
func (m *Model) copyTarget() (text, what string) {
	if m.ConsoleActive() {
		return m.term.SelectionText(), "console selection"
	}
	if m.col == colFrames {
		f, ok := m.SelectedFrame()
		if !ok {
			return "", ""
		}
		return f.Name + " — " + baseOf(f.Source.Path) + ":" + strconv.Itoa(f.Line), "stack frame"
	}
	n, ok := m.selectedVar()
	if !ok {
		return "", ""
	}
	if n.isWatch {
		if n.v.Value == "" {
			return n.v.Name, "watch expression"
		}
		return n.v.Name + " = " + n.v.Value, "watch value"
	}
	if n.v.Value == "" {
		return n.v.Name, "variable name"
	}
	return n.v.Name + " = " + n.v.Value, "variable value"
}
