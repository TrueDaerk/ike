// Package undotree is the undo-tree overlay (#59): a centered view of the
// focused editor's change tree (vim's undotree plugin). Every state the
// buffer ever reached is a row — timestamps, an excerpt of the change, the
// current and last-saved states marked — ordered newest-first with sibling
// branches indented under their branch point. j/k move, enter restores the
// selected state (the root model dispatches the jump back into the editor and
// refreshes the view, so the overlay stays open for further time travel).
package undotree

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/editor/history"
	"ike/internal/theme"
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
	m.cursor = 0
	for i, r := range m.rows {
		if r.node.Current {
			m.cursor = i
			break
		}
	}
}

// Close hides the overlay.
func (m *Model) Close() { m.open = false }

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

// Update handles one key while the overlay is open.
func (m *Model) Update(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q":
		m.Close()
	case "enter":
		return m.jumpCurrent()
	case "down", "j":
		m.move(1)
	case "up", "k":
		m.move(-1)
	case "pgdown":
		m.move(10)
	case "pgup":
		m.move(-10)
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = len(m.rows) - 1
	}
	return nil
}

// move shifts the selection by delta rows, clamped.
func (m *Model) move(delta int) {
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
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

// Wheel scrolls the selection by delta rows.
func (m *Model) Wheel(delta int) { m.move(delta) }

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

	listH := m.height/2 - 6
	if listH < 4 {
		listH = 4
	}
	if listH > len(m.rows) {
		listH = len(m.rows)
	}
	// Keep the selection in the window.
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+listH {
		m.top = m.cursor - listH + 1
	}
	if m.top < 0 {
		m.top = 0
	}
	m.listTop = len(rows)
	m.listRows = listH
	for i := m.top; i < m.top+listH && i < len(m.rows); i++ {
		rows = append(rows, m.renderRow(m.rows[i], i == m.cursor, innerW))
	}

	dim := lipgloss.NewStyle().Faint(true)
	rows = append(rows, "",
		dim.Render(strconv.Itoa(len(m.rows))+" states — j/k move, enter restores, esc closes"))

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
	when := "        "
	if !r.node.At.IsZero() {
		when = r.node.At.Format("15:04:05")
	}
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

// pad right-pads s to width with spaces.
func pad(s string, width int) string {
	for len(s) < width {
		s += " "
	}
	return s
}
