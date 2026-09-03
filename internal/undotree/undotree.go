// Package undotree is the undo-tree overlay (#59): a centered view of the
// focused editor's change tree (vim's undotree plugin). Every state the
// buffer ever reached is a row — its age, an excerpt of the change, the
// current and last-saved states marked — ordered newest-first with sibling
// branches indented under their branch point. j/k move, enter restores the
// selected state (the root model dispatches the jump back into the editor and
// refreshes the view, so the overlay stays open for further time travel).
//
// Rows carry a relative age ("5m ago") and the selected state gets a live
// inline diff preview against the current buffer (#2143, preview.go); "t"
// asks for an age in minutes and restores the newest state at least that old
// (timejump.go).
package undotree

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/editor/history"
	"ike/internal/theme"
	"ike/internal/ui"
)

// JumpMsg asks the root model to restore the focused editor's buffer to the
// history state Seq.
type JumpMsg struct{ Seq int }

// row is one rendered node: the tree node plus its display indent.
type row struct {
	node  history.NodeInfo
	depth int
}

// Model is the overlay state. The root model routes keys here while open and
// re-feeds the tree via SetNodes after a jump.
type Model struct {
	open   bool
	rows   []row
	cursor int // index into rows
	top    int // first visible row (scroll offset)

	width, height int
	pal           *theme.Palette

	// src supplies the buffer text the diff preview (#2143) is computed
	// against; cache memoizes the rendered diff per state seq.
	src   Source
	cache map[int][]string

	// now overrides the clock the relative timestamps and the time jump use
	// (tests only); nil means time.Now.
	now func() time.Time

	// asking/ageInput are the time-jump prompt ("t"): the typed age in
	// minutes, confirmed with enter.
	asking   bool
	ageInput string

	// lay records, during View, where the list rows sit so Click can hit-test.
	listTop, listRows int
}

// New returns a closed overlay.
func New() *Model { return &Model{} }

// SetPalette threads the active theme in.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// SetSize records the terminal size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// Open shows the overlay over the given tree, selecting the current state.
func (m *Model) Open(nodes []history.NodeInfo) {
	m.open = true
	m.SetNodes(nodes)
}

// SetNodes replaces the displayed tree (after a jump), keeping the selection
// on the current state so repeated jumps read naturally.
func (m *Model) SetNodes(nodes []history.NodeInfo) {
	m.rows = layout(nodes)
	m.cache = nil // the jump moved the current state: every diff is stale
	m.cursor = 0
	for i, r := range m.rows {
		if r.node.Current {
			m.cursor = i
			break
		}
	}
}

// Close hides the overlay and releases the row snapshot (#1550) — up to a
// thousand rows of tree state nobody reads while the overlay is closed; Open
// rebuilds them via SetNodes.
func (m *Model) Close() {
	m.open = false
	m.rows = nil
	m.cache = nil
	m.src = nil
	m.cursor, m.top = 0, 0
	m.asking, m.ageInput = false, ""
}

// IsOpen reports whether the overlay is shown.
func (m *Model) IsOpen() bool { return m.open }

// layout flattens the tree into display rows, newest state first. Children
// are laid out depth-first with the newest sibling continuing its parent's
// indent (the main line) and older, abandoned siblings indented one step —
// then the whole list is reversed so time runs upward like vim's undotree.
func layout(nodes []history.NodeInfo) []row {
	children := make(map[int][]int)
	bySeq := make(map[int]history.NodeInfo, len(nodes))
	for _, n := range nodes {
		bySeq[n.Seq] = n
		if n.Parent >= 0 {
			children[n.Parent] = append(children[n.Parent], n.Seq)
		}
	}
	var out []row
	var walk func(seq, depth int)
	walk = func(seq, depth int) {
		out = append(out, row{node: bySeq[seq], depth: depth})
		kids := children[seq] // ascending seq: nodes arrive sorted
		for i, k := range kids {
			d := depth + 1
			if i == len(kids)-1 {
				d = depth // the newest branch continues the line
			}
			walk(k, d)
		}
	}
	if _, ok := bySeq[0]; ok {
		walk(0, 0)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// listHeight is the row window the overlay renders — the pgup/pgdn page size
// (#1666). It is derived from the terminal height, so it is known before the
// first render.
func (m *Model) listHeight() int {
	h := m.height/2 - 6 - m.previewHeight()
	if h < 4 {
		h = 4
	}
	if h > len(m.rows) {
		h = len(m.rows)
	}
	return h
}

// Update handles one key while the overlay is open.
func (m *Model) Update(msg tea.KeyPressMsg) tea.Cmd {
	if m.asking {
		return m.updateTimeJump(msg)
	}
	switch msg.String() {
	case "esc", "q":
		m.Close()
	case "enter":
		return m.jumpCurrent()
	case "t":
		// Time jump (#2143): ask for an age in minutes, then restore the
		// newest state at least that old.
		m.startTimeJump()
	default:
		// Shared list semantics (#1666): steps wrap, page jumps clamp to a
		// visible page.
		ui.ListNav(msg.String(), &m.cursor, len(m.rows), m.listHeight(), ui.NavFull)
	}
	return nil
}

// jumpCurrent dispatches the selected state. The overlay stays open — the
// root model refreshes it via SetNodes after the editor applied the jump.
func (m *Model) jumpCurrent() tea.Cmd {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	seq := m.rows[m.cursor].node.Seq
	return func() tea.Msg { return JumpMsg{Seq: seq} }
}

// Click handles a left press at panel-local coordinates (0,0 = the box's
// top-left border cell): a row selects, a press on the selected row jumps.
func (m *Model) Click(x, y int) tea.Cmd {
	if !m.open {
		return nil
	}
	cy := y - 1 // border
	if cy < m.listTop || cy >= m.listTop+m.listRows {
		return nil
	}
	idx := m.top + (cy - m.listTop)
	if idx < 0 || idx >= len(m.rows) {
		return nil
	}
	if idx == m.cursor {
		return m.jumpCurrent()
	}
	m.cursor = idx
	return nil
}

// Wheel scrolls the selection by delta rows, clamped — a wheel flick past the
// end must not teleport to the other end of the list (#1666).
func (m *Model) Wheel(delta int) { m.cursor = ui.ClampIndex(m.cursor+delta, len(m.rows)) }

// theme returns the active palette, defaulting when none was threaded in.
func (m *Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

// View renders the centered overlay box.
func (m *Model) View() string {
	if !m.open || m.width <= 0 {
		return ""
	}
	pal := m.theme()
	boxW := m.width - 12
	if boxW > 80 {
		boxW = 80
	}
	if boxW < 40 {
		boxW = min(40, m.width-2)
	}
	innerW := boxW - 4 // border + padding

	title := lipgloss.NewStyle().Bold(true).Underline(true).Render("Undo Tree")
	rows := []string{title, ""}

	listH := m.listHeight()
	// Keep the selection in the window (#2462).
	m.top = ui.ScrollToShow(m.top, m.cursor, listH, len(m.rows))
	m.listTop = len(rows)
	m.listRows = listH
	for i := m.top; i < m.top+listH && i < len(m.rows); i++ {
		rows = append(rows, m.renderRow(m.rows[i], i == m.cursor, innerW))
	}

	dim := lipgloss.NewStyle().Faint(true)
	rows = append(rows, m.previewBlock(innerW)...)
	if m.asking {
		rows = append(rows, "",
			"Jump to the state from "+m.ageInput+"▏minutes ago (enter confirms, esc cancels)")
	} else {
		rows = append(rows, "",
			dim.Render(strconv.Itoa(len(m.rows))+" states — j/k move, enter restores, "+
				"t time-jump, esc closes"))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.BorderFocus).
		Padding(0, 1).
		Width(boxW - 2)
	return box.Render(strings.Join(rows, "\n"))
}

// renderRow renders one state line: indent, marker, seq, time, excerpt, tags.
func (m *Model) renderRow(r row, selected bool, width int) string {
	pal := m.theme()
	marker := "○"
	if r.node.Current {
		marker = "●"
	}
	when := pad(relAge(r.node.At, m.clock()), 9)
	label := r.node.Preview
	if r.node.Parent < 0 {
		label = "(original)"
	}
	line := strings.Repeat("  ", r.depth) + marker + " " +
		pad(strconv.Itoa(r.node.Seq), 4) + " " + when + "  " + label
	if r.node.Saved {
		line += "  [saved]"
	}
	st := lipgloss.NewStyle().MaxWidth(width)
	switch {
	case selected:
		st = st.Reverse(true)
	case r.node.Current:
		st = st.Foreground(pal.BorderFocus).Bold(true)
	}
	return st.Render(line)
}

// previewBlock renders the diff preview for the selected node under the list:
// a header naming the compared states plus the inline diff (#2143).
func (m *Model) previewBlock(width int) []string {
	if m.src == nil || m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	seq := m.rows[m.cursor].node.Seq
	h := m.previewHeight()
	lines := m.preview(seq, h)
	if lines == nil {
		return nil
	}
	dim := lipgloss.NewStyle().Faint(true)
	out := []string{"", dim.Render("diff: current → state " + strconv.Itoa(seq))}
	for _, l := range lines {
		out = append(out, m.renderPreview(l, width))
	}
	return out
}

// pad right-pads s to width with spaces.
func pad(s string, width int) string {
	for len(s) < width {
		s += " "
	}
	return s
}
