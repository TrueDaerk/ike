package httppane

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/ui"
)

// websocket.go is the pane side of a live WebSocket session (#2422): while the
// session is open the footer becomes an input line ("i" or enter opens it),
// enter sends the typed message, up/down step through the sent-message
// history, and "x" closes the session through the same CancelMsg path a
// stream abort takes. The pane cannot reach the connection — WSSendMsg names
// the moment and the host sends through the session handle, the CopyMsg
// arrangement once more.

// WSSendMsg asks the host to send one message over the open websocket session
// (#2422) — enter in the pane's input line.
type WSSendMsg struct{ Text string }

// SetWSLive marks the live stream as an open websocket session (#2422),
// unlocking the interactive input line. Call after StartStream; the mark
// clears with the stream (recompose).
func (m *Model) SetWSLive() {
	if m.streaming {
		m.ws = true
	}
}

// WSLive reports whether an open websocket session is on show (tests).
func (m *Model) WSLive() bool { return m.streaming && m.ws }

// WSInputOpen reports whether the session's input line is open (tests).
func (m *Model) WSInputOpen() bool { return m.wsInput }

// WSSentHistory reports the sent-message history, oldest first (tests).
func (m *Model) WSSentHistory() []string { return m.wsSent }

// openWSInput opens the input line over the session footer.
func (m *Model) openWSInput() {
	m.wsInput = true
	m.wsSentIdx = -1
	m.wsCur = len([]rune(m.wsText))
}

// wsInputKey handles one key while the input line is open. enter sends and
// keeps the prompt open — a session is a conversation, not a form — while esc
// closes it, keeping the draft for the next open.
func (m *Model) wsInputKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.wsInput = false
		return nil
	case "enter":
		text := m.wsText
		if text == "" {
			return nil
		}
		// Consecutive duplicates collapse, shell-history style.
		if n := len(m.wsSent); n == 0 || m.wsSent[n-1] != text {
			m.wsSent = append(m.wsSent, text)
		}
		m.wsText, m.wsCur, m.wsSentIdx, m.wsDraft = "", 0, -1, ""
		return func() tea.Msg { return WSSendMsg{Text: text} }
	case "up":
		if m.wsSentIdx+1 >= len(m.wsSent) {
			return nil
		}
		if m.wsSentIdx == -1 {
			m.wsDraft = m.wsText // the fresh line survives the browse
		}
		m.wsSentIdx++
		m.wsText = m.wsSent[len(m.wsSent)-1-m.wsSentIdx]
		m.wsCur = len([]rune(m.wsText))
		return nil
	case "down":
		if m.wsSentIdx < 0 {
			return nil
		}
		m.wsSentIdx--
		if m.wsSentIdx == -1 {
			m.wsText = m.wsDraft
		} else {
			m.wsText = m.wsSent[len(m.wsSent)-1-m.wsSentIdx]
		}
		m.wsCur = len([]rune(m.wsText))
		return nil
	}
	if t, cur, handled, changed := ui.EditKey(msg, m.wsText, m.wsCur); handled {
		m.wsText, m.wsCur = t, cur
		if changed {
			m.wsSentIdx = -1 // an edit leaves the history walk
		}
	}
	return nil
}

// wsFooter is the footer line while the input prompt is open (#2422).
func (m *Model) wsFooter() string {
	return " ➤ " + ui.CursorView(m.wsText, m.wsCur) + "  ↩ send · ↑/↓ history · esc close input"
}
