package editor

import (
	tea "charm.land/bubbletea/v2"
)

// takeClipboardSignal drains the register store's last system-clipboard
// failure and turns it into a toast (#1255).
//
// Before this, every `"+` write was `_ = clip.Write(text)`: a clipboard
// utility that was missing, sandboxed or failing produced the exact same
// "copied 3 lines" feedback as a working one, because the internal register
// was filled regardless. The store now records the error; Update drains it
// after every keypress and the Cmd+C/Cmd+X toasts drain it before reporting
// success, so a dead bridge is visible at the moment it fails.
//
// Draining is destructive: the first reader reports it, later ones see
// nothing. That keeps one failed write to one toast, not a toast per
// consumer.
func (m *Model) takeClipboardSignal() tea.Cmd {
	err := m.regs.TakeClipboardError()
	if err == nil {
		return nil
	}
	return notice("system clipboard unavailable: " + err.Error())
}
