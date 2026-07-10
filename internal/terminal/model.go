package terminal

import (
	"strconv"
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
	// scroll is the scrollback offset in lines (0 = live view). Paging keys
	// (shift+pgup/pgdn) and the mouse wheel move it; any other key snaps back
	// to live and goes to the shell.
	scroll int
}

// New starts a terminal model: shell (already resolved via Shell) spawned in
// dir with the extraEnv overlay (#98). A failed spawn yields a model
// rendering the error instead of a grid — the pane stays usable (closable)
// rather than crashing the layout.
func New(key, shell, dir string, w, h int, extraEnv []string, send func(tea.Msg)) Model {
	m := Model{w: w, h: h}
	sess, err := StartSession(key, shell, dir, w, h, extraEnv, send)
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

// ScrollbackLen reports the history length (0 for a failed spawn).
func (m Model) ScrollbackLen() int {
	if m.sess == nil {
		return 0
	}
	return m.sess.ScrollbackLen()
}

// Title returns the application-set OSC title ("" when none).
func (m Model) Title() string {
	if m.sess == nil {
		return ""
	}
	return m.sess.Title()
}

// Clear empties the scrollback and repaints (terminal.clear).
func (m *Model) Clear() {
	m.scroll = 0
	if m.sess != nil {
		m.sess.Clear()
	}
}

// Dir returns the session's origin directory ("" for a failed spawn).
func (m Model) Dir() string {
	if m.sess == nil {
		return ""
	}
	return m.sess.Dir()
}

// ShellPath returns the spawned shell binary ("" for a failed spawn).
func (m Model) ShellPath() string {
	if m.sess == nil {
		return ""
	}
	return m.sess.ShellPath()
}

// Close ends the underlying session.
func (m *Model) Close() {
	if m.sess != nil {
		m.sess.Close()
	}
}

// Update routes a key press: the scrollback paging keys move the view,
// everything else goes raw to the PTY (snapping the view back to live). The
// reserved set the root model never forwards is documented there
// (terminalReservedKeys in internal/app).
func (m *Model) Update(msg tea.KeyPressMsg) tea.Cmd {
	if m.sess == nil {
		return nil
	}
	switch msg.String() {
	case "shift+pgup":
		m.ScrollBy(m.pageSize())
		return nil
	case "shift+pgdown":
		m.ScrollBy(-m.pageSize())
		return nil
	}
	m.scroll = 0
	if ev, ok := motionKey(msg); ok {
		m.sess.SendKey(ev)
		return nil
	}
	for _, ev := range toVTKeys(msg) {
		m.sess.SendKey(ev)
	}
	return nil
}

// motionKey translates the macOS-conventional editing chords into the
// readline/ZLE emacs-mode defaults — the iTerm "natural text editing"
// convention (#225): option+arrows jump words (ESC b / ESC f), cmd+arrows go
// to line start/end (ctrl+a / ctrl+e). Shift-augmented variants behave the
// same; a PTY has no selection to extend.
func motionKey(k tea.KeyPressMsg) (vt.KeyPressEvent, bool) {
	mod := k.Mod &^ textMods
	isCmd := mod == tea.ModSuper || mod == tea.ModMeta
	switch {
	case mod == tea.ModAlt && k.Code == tea.KeyLeft:
		return vt.KeyPressEvent{Code: 'b', Mod: vt.ModAlt}, true
	case mod == tea.ModAlt && k.Code == tea.KeyRight:
		return vt.KeyPressEvent{Code: 'f', Mod: vt.ModAlt}, true
	case isCmd && k.Code == tea.KeyLeft:
		return vt.KeyPressEvent{Code: 'a', Mod: vt.ModCtrl}, true
	case isCmd && k.Code == tea.KeyRight:
		return vt.KeyPressEvent{Code: 'e', Mod: vt.ModCtrl}, true
	}
	return vt.KeyPressEvent{}, false
}

// pageSize is one paging step: half the grid, at least one line.
func (m Model) pageSize() int {
	if m.h > 1 {
		return m.h / 2
	}
	return 1
}

// ScrollBy moves the scrollback view by delta lines (positive = older),
// clamped to the available history; 0 is the live view.
func (m *Model) ScrollBy(delta int) {
	if m.sess == nil {
		return
	}
	m.scroll += delta
	if m.scroll < 0 {
		m.scroll = 0
	}
	if max := m.sess.ScrollbackLen(); m.scroll > max {
		m.scroll = max
	}
}

// Scroll reports the current scrollback offset (0 = live).
func (m Model) Scroll() int { return m.scroll }

// PasteText forwards pasted text through the bracketed-paste path.
func (m *Model) PasteText(text string) {
	if m.sess != nil {
		m.sess.Paste(text)
	}
}

// textMods are the modifiers that only transform which text a key produces;
// a key carrying nothing beyond these is plain typed input, not a chord.
const textMods = tea.ModShift | tea.ModCapsLock | tea.ModNumLock

// toVTKeys converts a bubbletea key press into the emulator's key events; the
// two structs share the same shape (code, shifted code, modifiers, text).
// The emulator's encoder writes a plain key only when no modifier is set, so
// shifted or caps-locked characters (shift+a → "A") would be dropped (#224);
// such presses are replayed as their produced text instead.
func toVTKeys(k tea.KeyPressMsg) []vt.KeyPressEvent {
	if k.Text != "" && k.Mod != 0 && k.Mod&^textMods == 0 {
		evs := make([]vt.KeyPressEvent, 0, 1)
		for _, r := range k.Text {
			evs = append(evs, vt.KeyPressEvent{Code: r, Text: string(r)})
		}
		return evs
	}
	return []vt.KeyPressEvent{{
		Code:        k.Code,
		ShiftedCode: k.ShiftedCode,
		Mod:         vt.KeyMod(k.Mod),
		Text:        k.Text,
	}}
}

// View renders the grid, with the cursor cell reversed while focused; a
// scrolled view windows over [scrollback ++ screen] instead. A dead or failed
// session renders its state.
func (m Model) View() string {
	if m.sess == nil {
		return "terminal failed: " + m.err
	}
	if m.scroll > 0 {
		return m.scrolledView()
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

// scrolledView renders the paging window: scroll lines above the live screen,
// filled from the scrollback, the remainder from the screen's top. The last
// line carries a position marker instead of the cursor.
func (m Model) scrolledView() string {
	sbLen := m.sess.ScrollbackLen()
	off := m.scroll
	if off > sbLen {
		off = sbLen
	}
	screen := strings.Split(m.sess.View(), "\n")
	rows := make([]string, 0, m.h)
	for i := 0; i < m.h; i++ {
		virtual := sbLen - off + i // index into [scrollback ++ screen]
		switch {
		case virtual < sbLen:
			rows = append(rows, m.sess.HistoryLine(virtual))
		case virtual-sbLen < len(screen):
			rows = append(rows, screen[virtual-sbLen])
		}
	}
	marker := "[scrollback -" + strconv.Itoa(off) + "  shift+pgdn to return]"
	if len(rows) > 0 {
		rows[len(rows)-1] = marker
	}
	return strings.Join(rows, "\n")
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
