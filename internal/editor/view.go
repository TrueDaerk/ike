package editor

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"ike/internal/editor/buffer"
)

// MouseClick moves the cursor to the content-local cell (x, y) — coordinates
// relative to the editor's content area (gutter included, title/border excluded).
// It maps the click through the gutter width and the current scroll offsets. In
// insert/replace mode the cursor may land one past the line end; otherwise it
// snaps onto a character.
func (m *Model) MouseClick(x, y int) {
	if y < 0 {
		y = 0
	}
	if x < 0 {
		x = 0
	}
	line := m.view.Top + y
	if line > m.buf.LineCount()-1 {
		line = m.buf.LineCount() - 1
	}
	col := x - m.view.GutterWidth(m.buf.LineCount()) + m.view.Left
	if col < 0 {
		col = 0
	}
	p := buffer.Position{Line: line, Col: col}
	if m.mode == Insert || m.mode == Replace {
		m.cursor = m.buf.Clamp(p)
	} else {
		m.cursor = m.buf.ClampCursor(p)
	}
	m.desiredCol = m.cursor.Col
	m.scroll()
	m.emit(EventCursorMove)
}

// CommandLine returns the text shown on the command line: ":cmd" in ex mode or
// "/pat" / "?pat" while searching. It is "" outside command mode.
func (m Model) CommandLine() string {
	if m.mode != Command {
		return ""
	}
	if m.searching {
		if m.searchDir == 0 { // search.Forward
			return "/" + m.cmdline
		}
		return "?" + m.cmdline
	}
	return ":" + m.cmdline
}

// View renders the buffer: a line-number gutter (when enabled), horizontally and
// vertically scrolled text, and the cursor cell highlighted when focused.
func (m Model) View() string {
	if m.path == "" && m.buf.LineCount() == 1 && m.buf.Line(0) == "" {
		return lipgloss.NewStyle().Faint(true).Render("(no file open)")
	}
	lineCount := m.buf.LineCount()
	bottom := m.view.Bottom(lineCount)
	gutterStyle := lipgloss.NewStyle().Faint(true)
	cursorStyle := lipgloss.NewStyle().Reverse(true)
	textWidth := m.view.TextWidth(lineCount)

	selStyle := lipgloss.NewStyle().Background(lipgloss.Color("238"))

	var out []string
	for i := m.view.Top; i < bottom; i++ {
		gutter := gutterStyle.Render(m.view.Gutter(i, m.cursor.Line, lineCount))
		body := m.renderLine(i, textWidth, cursorStyle, selStyle)
		out = append(out, gutter+body)
	}
	if len(out) == 0 {
		return ""
	}
	return lipgloss.JoinVertical(lipgloss.Left, out...)
}

// renderLine renders one buffer line within the horizontal window, overlaying
// the visual selection and the cursor cell (the cursor wins on overlap). It
// stops at the end of meaningful content so trailing blanks are not emitted.
func (m Model) renderLine(line, width int, cursorStyle, selStyle lipgloss.Style) string {
	runes := []rune(m.buf.Line(line))
	left := m.view.Left
	selStart, selEnd, hasSel := m.selectionOnLine(line, len(runes))
	isCursorLine := line == m.cursor.Line && m.focused

	var b strings.Builder
	for col := left; col < left+width; col++ {
		ch := " "
		if col < len(runes) {
			ch = string(runes[col])
		}
		cursorHere := isCursorLine && col == m.cursor.Col
		selected := hasSel && col >= selStart && col <= selEnd
		if col >= len(runes) && !cursorHere && !selected {
			break // nothing meaningful left on this line
		}
		switch {
		case cursorHere:
			b.WriteString(cursorStyle.Render(ch))
		case selected:
			b.WriteString(selStyle.Render(ch))
		default:
			b.WriteString(ch)
		}
	}
	return b.String()
}

// selectionOnLine returns the inclusive rune-column range to highlight on line
// for the active visual mode, or ok=false when the line is outside the selection
// or no visual mode is active.
func (m Model) selectionOnLine(line, runeLen int) (start, end int, ok bool) {
	if !m.mode.IsVisual() {
		return 0, 0, false
	}
	switch m.mode {
	case Visual:
		lo, hi := m.anchor, m.cursor
		if hi.Before(lo) {
			lo, hi = hi, lo
		}
		if line < lo.Line || line > hi.Line {
			return 0, 0, false
		}
		start = 0
		if line == lo.Line {
			start = lo.Col
		}
		end = runeLen // through the line break for middle lines
		if line == hi.Line {
			end = hi.Col
		}
		return start, end, true
	default: // VisualLine and VisualBlock
		lo, hi := minInt(m.anchor.Line, m.cursor.Line), maxInt(m.anchor.Line, m.cursor.Line)
		if line < lo || line > hi {
			return 0, 0, false
		}
		if m.mode == VisualBlock {
			return minInt(m.anchor.Col, m.cursor.Col), maxInt(m.anchor.Col, m.cursor.Col), true
		}
		return 0, runeLen, true
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
