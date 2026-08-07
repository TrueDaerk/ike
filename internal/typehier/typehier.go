// Package typehier is the type-hierarchy overlay (#1454): a centered modal
// rendering the supertypes or subtypes of the type the hierarchy was prepared
// on as a lazily-expanding tree — the callhier pattern verbatim, with the
// direction toggle swapped to supertypes/subtypes. The bridge prepares the
// roots and supplies a Fetch continuation; expanding a node runs it and the
// resulting TypeHierarchyItemsMsg fills that node's children. Selecting a row
// navigates through the same DefinitionMsg path go-to-definition uses.
package typehier

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
	"ike/internal/theme"
	"ike/internal/ui"
)

// node is one tree row: an entry plus its lazily-fetched children. loaded
// marks a completed fetch (an empty children set is then a leaf); loading
// marks one in flight.
type node struct {
	entry    ilsp.TypeHierarchyEntry
	depth    int
	children []*node
	expanded bool
	loaded   bool
	loading  bool
}

// Model is the overlay state. The root model routes keys here while open and
// feeds TypeHierarchyItemsMsg through Apply.
type Model struct {
	open       bool
	supertypes bool // direction: true = parents, false = children

	roots []ilsp.TypeHierarchyEntry
	nodes []*node
	fetch func(reqID int, item protocol.TypeHierarchyItem, supertypes bool) tea.Cmd

	// pending maps in-flight fetch request ids to the node awaiting children;
	// a direction toggle rebuilds the tree, so stale replies (their node no
	// longer in pending) are dropped.
	pending map[int]*node
	nextReq int

	cursor int // index into the visible-row sequence
	top    int // first visible row of the render window

	width, height int
	pal           *theme.Palette
	displayPath   func(string) string
}

// New returns a closed overlay.
func New() *Model { return &Model{} }

// SetPalette threads the active theme in.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// SetDisplayPath injects the row path formatter.
func (m *Model) SetDisplayPath(f func(string) string) { m.displayPath = f }

// SetSize records the terminal size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// IsOpen reports whether the overlay is shown.
func (m *Model) IsOpen() bool { return m.open }

// Open shows the overlay on the prepared roots, in supertypes direction, and
// immediately expands the first root so the first paint is useful.
func (m *Model) Open(msg ilsp.TypeHierarchyMsg) tea.Cmd {
	m.open = true
	m.supertypes = true
	m.roots = msg.Roots
	m.fetch = msg.Fetch
	return m.rebuild()
}

// Close hides the overlay and drops the tree.
func (m *Model) Close() {
	m.open = false
	m.nodes = nil
	m.pending = nil
}

// rebuild resets the tree to fresh root nodes for the current direction and
// kicks off the first root's expansion. In-flight fetches are orphaned (their
// pending entries are gone), so late replies fall on the floor.
func (m *Model) rebuild() tea.Cmd {
	m.pending = map[int]*node{}
	m.cursor, m.top = 0, 0
	m.nodes = make([]*node, len(m.roots))
	for i, e := range m.roots {
		m.nodes[i] = &node{entry: e}
	}
	if len(m.nodes) == 0 {
		return nil
	}
	return m.expand(m.nodes[0])
}

// expand fetches (or re-shows) a node's children.
func (m *Model) expand(n *node) tea.Cmd {
	if n.loaded {
		n.expanded = true
		return nil
	}
	if n.loading || m.fetch == nil {
		return nil
	}
	n.loading = true
	m.nextReq++
	m.pending[m.nextReq] = n
	return m.fetch(m.nextReq, n.entry.Item, m.supertypes)
}

// Apply consumes one expansion result, dropping replies whose node is gone
// (direction toggled or overlay reopened) or whose direction is stale.
func (m *Model) Apply(msg ilsp.TypeHierarchyItemsMsg) {
	n := m.pending[msg.ReqID]
	if n == nil {
		return
	}
	delete(m.pending, msg.ReqID)
	if msg.Supertypes != m.supertypes {
		return
	}
	n.loading = false
	n.loaded = true
	n.expanded = true
	n.children = make([]*node, len(msg.Items))
	for i, e := range msg.Items {
		n.children[i] = &node{entry: e, depth: n.depth + 1}
	}
}

// visible returns the rows a depth-first walk of the expanded tree shows.
func (m *Model) visible() []*node {
	var out []*node
	var walk func(ns []*node)
	walk = func(ns []*node) {
		for _, n := range ns {
			out = append(out, n)
			if n.expanded {
				walk(n.children)
			}
		}
	}
	walk(m.nodes)
	return out
}

// current returns the node under the cursor.
func (m *Model) current(rows []*node) *node {
	if m.cursor < 0 || m.cursor >= len(rows) {
		return nil
	}
	return rows[m.cursor]
}

// listHeight is the render window's row count — the pgup/pgdn page size
// (#1666). It mirrors the height renderRows lays out into.
func (m *Model) listHeight() int {
	h := m.height/2 - 5
	if h < 4 {
		h = 4
	}
	return h
}

// Update handles one key while the overlay is open.
func (m *Model) Update(msg tea.KeyPressMsg) tea.Cmd {
	rows := m.visible()
	// Shared list semantics (#1666): steps wrap, page jumps clamp.
	if ui.ListNav(msg.String(), &m.cursor, len(rows), m.listHeight(), ui.NavFull) {
		return nil
	}
	switch msg.String() {
	case "esc", "q":
		m.Close()
		return nil
	case "enter":
		if n := m.current(rows); n != nil {
			e := n.entry
			m.Close()
			return func() tea.Msg {
				return ilsp.DefinitionMsg{Path: e.Path, Line: e.Line, Col: e.Col}
			}
		}
		return nil
	case "right", "l", "space":
		if n := m.current(rows); n != nil {
			if n.expanded {
				return nil
			}
			return m.expand(n)
		}
		return nil
	case "left", "h":
		n := m.current(rows)
		if n == nil {
			return nil
		}
		if n.expanded {
			n.expanded = false
			return nil
		}
		// Collapsed already: land on the parent, JetBrains-tree style.
		for i := m.cursor - 1; i >= 0; i-- {
			if rows[i].depth < n.depth {
				m.cursor = i
				break
			}
		}
		return nil
	case "tab":
		// Toggle supertypes <-> subtypes: same roots, fresh tree.
		m.supertypes = !m.supertypes
		return m.rebuild()
	}
	return nil
}

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
	if boxW > 100 {
		boxW = 100
	}
	if boxW < 40 {
		boxW = min(40, m.width-2)
	}
	innerW := boxW - 4 // border + padding

	heading := "Type Hierarchy — Subtypes"
	if m.supertypes {
		heading = "Type Hierarchy — Supertypes"
	}
	title := lipgloss.NewStyle().Bold(true).Underline(true).Render(heading)
	rows := []string{title, ""}

	rows = append(rows, m.renderRows(innerW, m.listHeight(), pal)...)
	rows = append(rows, "", lipgloss.NewStyle().Faint(true).Render(
		"enter jumps · space expands · tab supertypes/subtypes · esc closes"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.BorderFocus).
		Padding(0, 1).
		Width(boxW - 2)
	return box.Render(strings.Join(rows, "\n"))
}

// renderRows lays the visible tree out to width×height, scrolled so the
// cursor row stays in the window.
func (m *Model) renderRows(width, height int, pal *theme.Palette) []string {
	rows := m.visible()
	if len(rows) == 0 {
		return []string{lipgloss.NewStyle().Faint(true).Render("no types")}
	}
	if m.cursor >= len(rows) {
		m.cursor = len(rows) - 1
	}
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+height {
		m.top = m.cursor - height + 1
	}
	if max := len(rows) - height; m.top > max {
		m.top = max
	}
	if m.top < 0 {
		m.top = 0
	}

	sel := lipgloss.NewStyle().Background(pal.SelectionMuted)
	name := lipgloss.NewStyle().Bold(true)
	dim := lipgloss.NewStyle().Faint(true)
	clip := lipgloss.NewStyle().MaxWidth(width)

	displayPath := m.displayPath
	if displayPath == nil {
		displayPath = func(p string) string { return p }
	}

	var out []string
	for i := m.top; i < len(rows) && i < m.top+height; i++ {
		n := rows[i]
		marker := "▸"
		switch {
		case n.loading:
			marker = "…"
		case n.expanded:
			marker = "▾"
		case n.loaded && len(n.children) == 0:
			marker = "·"
		}
		indent := strings.Repeat("  ", n.depth)
		loc := displayPath(n.entry.Path) + ":" + strconv.Itoa(n.entry.Line+1)
		detail := ""
		if n.entry.Detail != "" {
			detail = " " + n.entry.Detail
		}
		if i == m.cursor {
			row := indent + marker + " " + n.entry.Name + detail + "  " + loc
			out = append(out, clip.Render(sel.Render(row)))
			continue
		}
		row := dim.Render(indent+marker+" ") + name.Render(n.entry.Name) +
			dim.Render(detail+"  "+loc)
		out = append(out, clip.Render(row))
	}
	return out
}
