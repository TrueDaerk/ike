package editor

import (
	"strconv"
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
//
// A peek can carry several candidates (#2168): a multi-target definition
// answer is picked from inside the popup instead of through a modal list —
// the candidate rows sit under the excerpt, tab / shift+tab switch between
// them, and enter jumps to the selected one.

// peekVisibleRows caps how many excerpt rows the popup shows at once; longer
// excerpts scroll. peekCandidateRows caps the candidate list under it; a
// longer list scrolls with the selection.
const (
	peekVisibleRows   = 8
	peekCandidateRows = 4
)

// PeekTarget is one peek candidate as the app hands it over: the header
// title, the raw excerpt around the definition line, and the jump target
// Enter navigates to.
type PeekTarget struct {
	Title string   // "path:line", shown in the header row
	Lines []string // raw excerpt, highlighted on open
	Path  string   // jump target (editor coordinates)
	Line  int
	Col   int
}

// peekTarget is a PeekTarget with its excerpt already syntax-highlighted.
type peekTarget struct {
	title string
	lines []string // pre-styled excerpt rows
	path  string
	line  int
	col   int
}

// peekState is the open peek popup: the candidates, which one is selected,
// and the scroll offset into the selected candidate's excerpt.
type peekState struct {
	targets []peekTarget
	sel     int // selected candidate
	scroll  int // first visible excerpt row of the selected candidate
	listTop int // first visible candidate row
}

// cur returns the selected candidate.
func (p *peekState) cur() peekTarget { return p.targets[p.sel] }

// OpenPeek opens the peek popup over this editor on a single target. title
// names it ("path:line"), lines is the raw excerpt, path/line/col is the jump
// target Enter navigates to.
func (m *Model) OpenPeek(title string, lines []string, path string, line, col int) {
	m.OpenPeekTargets([]PeekTarget{{Title: title, Lines: lines, Path: path, Line: line, Col: col}})
}

// OpenPeekTargets opens the peek popup over this editor on one or more
// candidates (#2168), the first selected. Candidates without an excerpt are
// dropped; nothing left means no popup. Every excerpt is syntax-highlighted
// with its file's language when a grammar backs it (highlight.Highlight
// parses the standalone excerpt, like hover code fences do, #379); otherwise
// the rows render plain inside the popup frame.
func (m *Model) OpenPeekTargets(targets []PeekTarget) {
	styled := make([]peekTarget, 0, len(targets))
	for _, t := range targets {
		if len(t.Lines) == 0 {
			continue
		}
		styled = append(styled, peekTarget{
			title: t.Title,
			lines: m.styledPeekLines(t.Path, t.Lines),
			path:  t.Path,
			line:  t.Line,
			col:   t.Col,
		})
	}
	if len(styled) == 0 {
		return
	}
	m.peek = &peekState{targets: styled}
}

// styledPeekLines highlights an excerpt with path's language, falling back to
// the raw lines when no grammar backs the file.
func (m Model) styledPeekLines(path string, lines []string) []string {
	ix := highlight.NewIndex(highlight.Highlight(path, lines))
	out := make([]string, len(lines))
	for i, l := range lines {
		if ix.Empty() {
			out[i] = l
			continue
		}
		out[i] = m.styledCodeLine(ix, i, l)
	}
	return out
}

// PeekOpen reports whether the peek popup is showing.
func (m Model) PeekOpen() bool { return m.peek != nil && len(m.peek.targets) > 0 }

// PeekAnchor returns the buffer-relative cell the peek popup anchors to: the
// cursor, like the keyboard-triggered hover.
func (m Model) PeekAnchor() (col, line int) { return m.cursor.Col, m.cursor.Line }

// dismissPeek closes the peek popup.
func (m *Model) dismissPeek() { m.peek = nil }

// peekKey handles a key while the peek popup is open. It returns true when
// the key was consumed (close, jump, scroll, candidate switch); any other key
// closes the peek and returns false so normal dispatch handles it — like a
// hover dismiss.
func (m *Model) peekKey(key tea.KeyPressMsg) (bool, tea.Cmd) {
	p := m.peek
	switch {
	case key.Code == tea.KeyEscape:
		m.dismissPeek()
		return true, nil
	case key.Code == tea.KeyEnter:
		// Jump for real: the same DefinitionMsg funnel as go-to-definition,
		// so the pane-dedup (#930) and nav-history recording apply.
		t := p.cur()
		m.dismissPeek()
		return true, func() tea.Msg {
			return ilsp.DefinitionMsg{Path: t.path, Line: t.line, Col: t.col}
		}
	case key.Code == tea.KeyTab && key.Mod&tea.ModShift != 0:
		m.peekSelect(-1)
		return true, nil
	case key.Code == tea.KeyTab:
		m.peekSelect(1)
		return true, nil
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

// peekSelect moves the candidate selection by delta, wrapping, and shows the
// new candidate's excerpt from its top. A single candidate ignores it — tab
// still counts as consumed, so a peek never leaks a tab into the buffer.
func (m *Model) peekSelect(delta int) {
	p := m.peek
	n := len(p.targets)
	if n < 2 {
		return
	}
	p.sel = ((p.sel+delta)%n + n) % n
	p.scroll = 0
	// Keep the selected row inside the candidate window.
	if p.sel < p.listTop {
		p.listTop = p.sel
	}
	if p.sel >= p.listTop+peekCandidateRows {
		p.listTop = p.sel - peekCandidateRows + 1
	}
}

// peekScroll moves the excerpt window by delta rows, clamped to the excerpt.
func (m *Model) peekScroll(delta int) {
	p := m.peek
	max := len(p.cur().lines) - peekVisibleRows
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

// PeekView renders the peek popup: a bold header naming the target (with a
// candidate counter when there are several), a rule, the visible excerpt
// window, and — for a multi-candidate peek — the candidate list below a
// second rule. Rows are truncated (not wrapped) at the popup width cap so
// code lines stay one row each; dim ellipsis rows mark clipped content
// above/below the scroll window.
func (m Model) PeekView() string {
	p := m.peek
	if p == nil {
		return ""
	}
	cur := p.cur()
	end := p.scroll + peekVisibleRows
	if end > len(cur.lines) {
		end = len(cur.lines)
	}
	visible := cur.lines[p.scroll:end]

	header := cur.title
	if len(p.targets) > 1 {
		header += "  (" + strconv.Itoa(p.sel+1) + "/" + strconv.Itoa(len(p.targets)) + ")"
	}
	candidates := m.peekCandidateRows()

	maxW := m.popupMaxWidth()
	width := lipgloss.Width(header)
	for _, l := range append(append([]string{}, visible...), candidates...) {
		if w := lipgloss.Width(l); w > width {
			width = w
		}
	}
	if width > maxW {
		width = maxW
	}
	th := m.theme()
	dim := lipgloss.NewStyle().Foreground(th.Border)
	rule := dim.Render(strings.Repeat("─", width))
	rows := []string{
		lipgloss.NewStyle().Foreground(th.Accent).Bold(true).Render(peekTitle(header, width)),
		rule,
	}
	if p.scroll > 0 {
		rows = append(rows, dim.Render("…"))
	}
	for _, l := range visible {
		rows = append(rows, ansi.Truncate(l, width, "…"))
	}
	if end < len(cur.lines) {
		rows = append(rows, dim.Render("…"))
	}
	if len(candidates) > 0 {
		rows = append(rows, rule)
		for _, l := range candidates {
			rows = append(rows, ansi.Truncate(l, width, "…"))
		}
		rows = append(rows, dim.Render("tab: next candidate · enter: jump · esc: close"))
	}
	box := m.popupFrame().Padding(0, 1)
	return box.Render(strings.Join(rows, "\n"))
}

// peekCandidateRows renders the candidate list of a multi-candidate peek: the
// window of at most peekCandidateRows entries around the selection, the
// selected one marked and accented, the rest dim. A single-candidate peek has
// no list.
func (m Model) peekCandidateRows() []string {
	p := m.peek
	if len(p.targets) < 2 {
		return nil
	}
	th := m.theme()
	sel := lipgloss.NewStyle().Foreground(th.Accent).Bold(true)
	dim := lipgloss.NewStyle().Foreground(th.Border)
	top := p.listTop
	if max := len(p.targets) - peekCandidateRows; top > max {
		top = max
	}
	if top < 0 {
		top = 0
	}
	end := top + peekCandidateRows
	if end > len(p.targets) {
		end = len(p.targets)
	}
	// Candidate rows fit like the header: dropped from the LEFT, so the
	// filename and line survive a long path (the marker column costs two).
	fit := m.popupMaxWidth() - 2
	rows := make([]string, 0, end-top)
	for i := top; i < end; i++ {
		title := peekTitle(p.targets[i].title, fit)
		if i == p.sel {
			rows = append(rows, sel.Render("› "+title))
			continue
		}
		rows = append(rows, dim.Render("  "+title))
	}
	return rows
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
