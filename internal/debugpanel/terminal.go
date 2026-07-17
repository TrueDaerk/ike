package debugpanel

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/terminal"
)

// Terminal embedding (#676): a DAP runInTerminal debuggee runs inside the
// panel's Output column instead of a separate bottom-split pane. While a
// terminal is embedded its PTY view replaces the DAP output rows; keys reach
// it when the Output column is focused and the process runs, and the mouse
// (click/wheel/drag) forwards with column-local coordinates. Without a
// terminal the column keeps rendering DAP output events as before.

// SetTerminal embeds t into the Output column, replacing (and closing) any
// previous debuggee terminal — the reuse-across-sessions path: a new session's
// runInTerminal hands over a fresh model while the old one has exited. The
// terminal is sized to the column and inherits the panel's palette and focus.
func (m *Model) SetTerminal(t *terminal.Model) {
	if m.term != nil && m.term != t {
		m.term.Close()
	}
	m.term = t
	if t != nil {
		t.SetPalette(m.pal)
	}
	m.sizeTerminal()
	m.syncTermFocus()
}

// Terminal returns the embedded debuggee terminal, nil when none.
func (m Model) Terminal() *terminal.Model { return m.term }

// HasTerminal reports whether a debuggee terminal is embedded.
func (m Model) HasTerminal() bool { return m.term != nil }

// CloseTerminal ends the embedded session and detaches it; the Output column
// falls back to DAP output rows. Called when the panel closes (pane registry)
// so the debuggee's PTY never outlives its host.
func (m *Model) CloseTerminal() {
	if m.term == nil {
		return
	}
	m.term.Close()
	m.term = nil
}

// OutputTermCapturing reports whether the embedded terminal owns the keyboard:
// the Output column is focused and the debuggee still runs. The app routes
// keys raw to the panel then (like a focused terminal pane), bypassing the
// keymap layer so plain letters reach the debuggee's stdin.
func (m Model) OutputTermCapturing() bool {
	return m.col == colOutput && m.term != nil && m.term.Running()
}

// sizeTerminal fits the embedded terminal to the Output column's interior
// (the column width, the rows under the title).
func (m *Model) sizeTerminal() {
	if m.term == nil {
		return
	}
	_, _, ow := m.colWidths()
	if ow < 1 || m.bodyHeight() < 1 {
		return
	}
	m.term.SetSize(ow, m.bodyHeight())
}

// syncTermFocus mirrors the panel's focus state onto the embedded terminal:
// its cursor cell renders only while the panel is focused on the Output
// column. Called whenever focus or the focused column changes.
func (m *Model) syncTermFocus() {
	if m.term != nil {
		m.term.SetFocused(m.focused && m.col == colOutput)
	}
}

// outputTermKey routes a key press while the Output column is focused with an
// embedded terminal. A running debuggee takes every key raw — except
// shift+tab, the reserved escape back to the variables column (the spatial
// focus keys leave the pane at the app layer as usual). After the process
// exited the panel's navigation returns; j/k page the dead terminal's
// scrollback so the final output stays reviewable.
func (m *Model) outputTermKey(k tea.KeyPressMsg) tea.Cmd {
	if m.term.Running() {
		if k.String() == "shift+tab" {
			m.col = colVars
			m.syncTermFocus()
			return nil
		}
		return m.term.Update(k)
	}
	switch k.String() {
	case "h", "left", "shift+tab":
		m.col = colVars
		m.syncTermFocus()
	case "j", "down":
		m.term.ScrollBy(-1)
	case "k", "up":
		m.term.ScrollBy(1)
	}
	return nil
}

// termOrigin is the pane-content-local cell of the embedded terminal's top-left
// corner: past the frames and variables columns plus their separators, under
// the title row.
func (m Model) termOrigin() (x, y int) {
	fw, vw, _ := m.colWidths()
	return fw + 1 + vw + 1, 1
}

// OutputTermHit reports whether the pane-content-local (x, y) lands on the
// embedded terminal's grid — the app starts a selection drag then.
func (m Model) OutputTermHit(x, y int) bool {
	if m.term == nil || x < 0 || x >= m.w || y < 1 || y >= m.h {
		return false
	}
	return m.columnAt(x) == colOutput
}

// TermDrag forwards a selection drag to the embedded terminal, translating
// pane-content-local coordinates to the terminal's own grid.
func (m *Model) TermDrag(x, y int) {
	if m.term == nil {
		return
	}
	ox, oy := m.termOrigin()
	m.term.MouseDrag(x-ox, y-oy)
}

// TermRelease forwards the drag release the same way.
func (m *Model) TermRelease(x, y int) {
	if m.term == nil {
		return
	}
	ox, oy := m.termOrigin()
	m.term.MouseRelease(x-ox, y-oy)
}
