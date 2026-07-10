package terminal

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/vt"

	"ike/internal/theme"
)

// Model is the pane-facing terminal: it owns a Session and adapts pane
// sizing, focus and key routing to it. It follows the explorer/editor value
// shape (value type, pointer-receiver mutations) so the pane registry can
// host it behind an Instance.
type Model struct {
	sess    *Session
	err     string
	focused bool
	w, h    int
	pal     *theme.Palette
}

// New starts a terminal model: shell (already resolved via Shell) spawned in
// dir. A failed spawn yields a model rendering the error instead of a grid —
// the pane stays usable (closable) rather than crashing the layout.
func New(key, shell, dir string, w, h int, send func(tea.Msg)) Model {
	m := Model{w: w, h: h}
	sess, err := StartSession(key, shell, dir, w, h, send)
	if err != nil {
		m.err = err.Error()
		return m
	}
	m.sess = sess
	return m
}

// SetPalette threads the active theme palette (chrome only; the grid's colors
// come from the application's own escape codes).
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// SetSize resizes the grid and the PTY.
func (m *Model) SetSize(w, h int) {
	m.w, m.h = w, h
	if m.sess != nil {
		m.sess.Resize(w, h)
	}
}

// SetFocused records focus; the cursor cell renders only while focused.
func (m *Model) SetFocused(on bool) { m.focused = on }

// Running reports whether the shell is alive.
func (m Model) Running() bool { return m.sess != nil && m.sess.Running() }

// Close ends the underlying session.
func (m *Model) Close() {
	if m.sess != nil {
		m.sess.Close()
	}
}

// Update routes a key press to the shell. Every key goes raw to the PTY —
// the escape hatch (focus-away chord) is enforced by the root model before
// keys reach the pane (the boundary is finalised in #96/#97).
func (m *Model) Update(msg tea.KeyPressMsg) tea.Cmd {
	if m.sess == nil {
		return nil
	}
	m.sess.SendKey(toVTKey(msg))
	return nil
}

// PasteText forwards pasted text through the bracketed-paste path.
func (m *Model) PasteText(text string) {
	if m.sess != nil {
		m.sess.Paste(text)
	}
}

// toVTKey converts a bubbletea key press into the emulator's key event; the
// two structs share the same shape (code, shifted code, modifiers, text).
func toVTKey(k tea.KeyPressMsg) vt.KeyPressEvent {
	return vt.KeyPressEvent{
		Code:        k.Code,
		ShiftedCode: k.ShiftedCode,
		Mod:         vt.KeyMod(k.Mod),
		Text:        k.Text,
	}
}

// View renders the grid, with the cursor cell reversed while focused. A dead
// or failed session renders its state instead.
func (m Model) View() string {
	if m.sess == nil {
		return "terminal failed: " + m.err
	}
	view := m.sess.View()
	if !m.focused || !m.sess.Running() {
		if !m.sess.Running() {
			view += "\n[process exited]"
		}
		return view
	}
	cx, cy := m.sess.CursorPosition()
	return overlayCursor(view, cx, cy)
}

// overlayCursor reverse-videos the cursor cell inside the rendered grid. The
// grid is ANSI-styled, so the splice walks the target line rune-aware while
// skipping escape sequences.
func overlayCursor(view string, x, y int) string {
	lines := strings.Split(view, "\n")
	if y < 0 || y >= len(lines) {
		return view
	}
	lines[y] = reverseCell(lines[y], x)
	return strings.Join(lines, "\n")
}

var cursorStyle = lipgloss.NewStyle().Reverse(true)

// reverseCell restyles the visible cell at column col of an ANSI-styled line.
func reverseCell(line string, col int) string {
	var b strings.Builder
	visible := 0
	inEsc := false
	done := false
	for i := 0; i < len(line); {
		if !inEsc && line[i] == 0x1b {
			inEsc = true
			b.WriteByte(line[i])
			i++
			continue
		}
		if inEsc {
			b.WriteByte(line[i])
			// CSI sequences end on a final byte in @-~; the two-byte forms
			// (ESC + single char) end immediately.
			if line[i] >= 0x40 && line[i] <= 0x7e && line[i] != '[' {
				inEsc = false
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		if visible == col && !done {
			b.WriteString(cursorStyle.Render(string(r)))
			done = true
		} else {
			b.WriteString(line[i : i+size])
		}
		visible++
		i += size
	}
	if !done {
		// Cursor past the rendered content: pad with spaces up to the column.
		if pad := col - visible; pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
		b.WriteString(cursorStyle.Render(" "))
	}
	return b.String()
}
