// Package typehier is the type-hierarchy overlay (#1454): a centered modal
// rendering the supertypes or subtypes of the type the hierarchy was prepared
// on as a lazily-expanding tree — the callhier overlay with the direction
// toggle swapped to supertypes/subtypes. The bridge prepares the roots and
// supplies a Fetch continuation; expanding a node runs it and the resulting
// TypeHierarchyItemsMsg fills that node's children. Selecting a row navigates
// through the same DefinitionMsg path go-to-definition uses.
//
// The tree itself — nodes, keys, request bookkeeping, rendering — is the
// shared hiertree (#2465); this package keeps the LSP message types, the
// supertypes/subtypes direction and the overlay chrome.
package typehier

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/hiertree"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
	"ike/internal/theme"
)

// row is the shared tree node carrying a type-hierarchy item.
type row = hiertree.Row[protocol.TypeHierarchyItem]

// Model is the overlay state. The root model routes keys here while open and
// feeds TypeHierarchyItemsMsg through Apply.
type Model struct {
	open       bool
	supertypes bool // direction: true = parents, false = children

	tree hiertree.Tree[protocol.TypeHierarchyItem]

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

// rows converts the bridge's entries into tree rows.
func rows(entries []ilsp.TypeHierarchyEntry) []row {
	out := make([]row, len(entries))
	for i, e := range entries {
		out[i] = row{
			Entry: hiertree.Entry{Name: e.Name, Detail: e.Detail, Path: e.Path, Line: e.Line, Col: e.Col},
			Item:  e.Item,
		}
	}
	return out
}

// Open shows the overlay on the prepared roots, in supertypes direction, and
// immediately expands the first root so the first paint is useful. The tree's
// fetch reads the direction at request time, so a toggle needs no rewiring.
func (m *Model) Open(msg ilsp.TypeHierarchyMsg) tea.Cmd {
	m.open = true
	m.supertypes = true
	var fetch hiertree.Fetch[protocol.TypeHierarchyItem]
	if msg.Fetch != nil {
		fetch = func(reqID int, item protocol.TypeHierarchyItem) tea.Cmd {
			return msg.Fetch(reqID, item, m.supertypes)
		}
	}
	return m.tree.Open(rows(msg.Roots), fetch)
}

// Close hides the overlay and drops the tree.
func (m *Model) Close() {
	m.open = false
	m.tree.Clear()
}

// Apply consumes one expansion result, dropping replies whose node is gone
// (direction toggled or overlay reopened) or whose direction is stale.
func (m *Model) Apply(msg ilsp.TypeHierarchyItemsMsg) {
	m.tree.Apply(msg.ReqID, msg.Supertypes != m.supertypes, rows(msg.Items))
}

// listHeight is the render window's row count — the pgup/pgdn page size
// (#1666). It mirrors the height the tree lays its rows out into.
func (m *Model) listHeight() int {
	h := m.height/2 - 5
	if h < 4 {
		h = 4
	}
	return h
}

// Update handles one key while the overlay is open.
func (m *Model) Update(msg tea.KeyPressMsg) tea.Cmd {
	onEnter := func(r row) tea.Cmd {
		e := r.Entry
		m.Close()
		return func() tea.Msg {
			return ilsp.DefinitionMsg{Path: e.Path, Line: e.Line, Col: e.Col}
		}
	}
	onToggle := func() tea.Cmd {
		// Toggle supertypes <-> subtypes: same roots, fresh tree.
		m.supertypes = !m.supertypes
		return m.tree.Rebuild()
	}
	if cmd, ok := m.tree.Key(msg.String(), m.listHeight(), onEnter, onToggle); ok {
		return cmd
	}
	switch msg.String() {
	case "esc", "q":
		m.Close()
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

	rows = append(rows, m.tree.RenderRows(innerW, m.listHeight(), pal, m.displayPath, "no types")...)
	rows = append(rows, "", lipgloss.NewStyle().Faint(true).Render(
		"enter jumps · space expands · tab supertypes/subtypes · esc closes"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.BorderFocus).
		Padding(0, 1).
		Width(boxW - 2)
	return box.Render(strings.Join(rows, "\n"))
}
