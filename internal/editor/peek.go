package editor

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/highlight"
	ilsp "ike/internal/lsp"
)

// peek.go is the peek-definition popup (#1154): a cursor-anchored box showing
// the definition target's surrounding lines without navigating. A sibling of
// the hover popup — same frame (#316), same app-side compositing — but it owns
// a few keys while open: esc closes, enter jumps for real (through the shared
// ilsp.DefinitionMsg funnel, so nav history records like any jump), up/down
// and ctrl+u/ctrl+d scroll the excerpt; any other key closes the peek and is
// handled normally (the hover-dismiss precedent).

// peekVisibleRows caps how many excerpt rows the popup shows at once; longer
// excerpts scroll.
const peekVisibleRows = 8

// peekState is the open peek popup: the pre-styled excerpt rows, the scroll
// offset into them, and the jump target Enter navigates to.
type peekState struct {
	title  string   // "path:line", shown in the header row
	lines  []string // pre-styled excerpt rows
	scroll int      // first visible excerpt row
	path   string   // jump target (editor coordinates)
	line   int
	col    int
}

// OpenPeek opens the peek popup over this editor. title names the target
// ("path:line"), lines is the raw excerpt, path/line/col is the jump target
// Enter navigates to. The excerpt is syntax-highlighted with the target
// file's language when a grammar backs it (highlight.Highlight parses the
// standalone excerpt, like hover code fences do, #379); otherwise the rows
// render plain inside the popup frame.
func (m *Model) OpenPeek(title string, lines []string, path string, line, col int) {
	if len(lines) == 0 {
		return
	}
	ix := highlight.NewIndex(highlight.Highlight(path, lines))
	styled := make([]string, len(lines))
	for i, l := range lines {
		if ix.Empty() {
			styled[i] = l
			continue
		}
		styled[i] = m.styledCodeLine(ix, i, l)
	}
	m.peek = &peekState{title: title, lines: styled, path: path, line: line, col: col}
}

// PeekOpen reports whether the peek popup is showing.
func (m Model) PeekOpen() bool { return m.peek != nil && len(m.peek.lines) > 0 }

// PeekAnchor returns the buffer-relative cell the peek popup anchors to: the
// cursor, like the keyboard-triggered hover.
func (m Model) PeekAnchor() (col, line int) { return m.cursor.Col, m.cursor.Line }

// dismissPeek closes the peek popup.
func (m *Model) dismissPeek() { m.peek = nil }

// peekKey handles a key while the peek popup is open. It returns true when
// the key was consumed (close, jump, scroll); any other key closes the peek
// and returns false so normal dispatch handles it — like a hover dismiss.
func (m *Model) peekKey(key tea.KeyPressMsg) (bool, tea.Cmd) {
	p := m.peek
	switch {
	case key.Code == tea.KeyEscape:
		m.dismissPeek()
		return true, nil
	case key.Code == tea.KeyEnter:
		// Jump for real: the same DefinitionMsg funnel as go-to-definition,
		// so the pane-dedup (#930) and nav-history recording apply.
		m.dismissPeek()
		path, line, col := p.path, p.line, p.col
		return true, func() tea.Msg {
			return ilsp.DefinitionMsg{Path: path, Line: line, Col: col}
		}
	case key.Code == tea.KeyDown:
		m.peekScroll(1)
		return true, nil
	case key.Code == tea.KeyUp:
		m.peekScroll(-1)
		return true, nil
	case key.Code == 'd' && key.Mod == tea.ModCtrl:
		m.peekScroll(peekVisibleRows / 2)
		return true, nil
	case key.Code == 'u' && key.Mod == tea.ModCtrl:
		m.peekScroll(-peekVisibleRows / 2)
		return true, nil
	}
	m.dismissPeek()
	return false, nil
}

// peekScroll moves the excerpt window by delta rows, clamped to the excerpt.
func (m *Model) peekScroll(delta int) {
	p := m.peek
	max := len(p.lines) - peekVisibleRows
	if max < 0 {
		max = 0
	}
	p.scroll += delta
	if p.scroll > max {
		p.scroll = max
	}
	if p.scroll < 0 {
		p.scroll = 0
	}
}

// PeekView renders the peek popup: a bold header naming the target, a rule,
// and the visible excerpt window. Rows are truncated (not wrapped) at the
// popup width cap so code lines stay one row each; dim ellipsis rows mark
// clipped content above/below the scroll window.
func (m Model) PeekView() string {
	p := m.peek
	if p == nil {
		return ""
	}
	end := p.scroll + peekVisibleRows
	if end > len(p.lines) {
		end = len(p.lines)
	}
	visible := p.lines[p.scroll:end]
	maxW := m.popupMaxWidth()
	width := lipgloss.Width(p.title)
	for _, l := range visible {
		if w := lipgloss.Width(l); w > width {
			width = w
		}
	}
	if width > maxW {
		width = maxW
	}
	th := m.theme()
	dim := lipgloss.NewStyle().Foreground(th.Border)
	rows := []string{
		lipgloss.NewStyle().Foreground(th.Accent).Bold(true).Render(peekTitle(p.title, width)),
		dim.Render(strings.Repeat("─", width)),
	}
	if p.scroll > 0 {
		rows = append(rows, dim.Render("…"))
	}
	for _, l := range visible {
		rows = append(rows, ansi.Truncate(l, width, "…"))
	}
	if end < len(p.lines) {
		rows = append(rows, dim.Render("…"))
	}
	box := m.popupFrame().Padding(0, 1)
	return box.Render(strings.Join(rows, "\n"))
}

// peekTitle fits the "path:line" header into width cells, dropping from the
// LEFT when too long — the filename and line number are the valuable end of a
// long path.
func peekTitle(title string, width int) string {
	r := []rune(title)
	if lipgloss.Width(title) <= width || width < 2 {
		return title
	}
	for len(r) > 0 && lipgloss.Width("…"+string(r)) > width {
		r = r[1:]
	}
	return "…" + string(r)
}

// LineRange returns up to n buffer lines starting at start (0-based, clamped)
// — the live-buffer excerpt source for the peek popup when the target file is
// already open (#1154).
func (m Model) LineRange(start, n int) []string {
	last := m.buf.LineCount()
	if start < 0 {
		start = 0
	}
	if start >= last {
		return nil
	}
	end := start + n
	if end > last {
		end = last
	}
	out := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, m.buf.Line(i))
	}
	return out
}
