// Package callhier is the call-hierarchy overlay (#173): a centered modal
// rendering the callers (incoming) or callees (outgoing) of the symbol the
// hierarchy was prepared on as a lazily-expanding tree. The bridge prepares
// the roots and supplies a Fetch continuation; expanding a node runs it and
// the resulting CallHierarchyCallsMsg fills that node's children. Selecting a
// row navigates through the same DefinitionMsg path go-to-definition uses.
// Beside the tree it carries the shared code-preview column (#2053), so the
// selected caller's source is visible before one jumps into it.
//
// The tree itself — nodes, keys, request bookkeeping, rendering — is the
// shared hiertree (#2465); this package keeps the LSP message types, the
// callers/callees direction and the overlay chrome.
package callhier

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/codepreview"
	"ike/internal/hiertree"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
	"ike/internal/theme"
)

// row is the shared tree node carrying a call-hierarchy item.
type row = hiertree.Row[protocol.CallHierarchyItem]

// Model is the overlay state. The root model routes keys here while open and
// feeds CallHierarchyCallsMsg through Apply.
type Model struct {
	open     bool
	incoming bool // direction: true = callers, false = callees

	tree hiertree.Tree[protocol.CallHierarchyItem]

	width, height int
	pal           *theme.Palette
	displayPath   func(string) string

	// prev caches the code-preview window (#2053), so moving the cursor
	// through the tree re-reads a file only when the target actually moves.
	prev codepreview.Cache
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
func rows(entries []ilsp.CallHierarchyEntry) []row {
	out := make([]row, len(entries))
	for i, e := range entries {
		out[i] = row{
			Entry: hiertree.Entry{Name: e.Name, Detail: e.Detail, Path: e.Path, Line: e.Line, Col: e.Col},
			Item:  e.Item,
		}
	}
	return out
}

// Open shows the overlay on the prepared roots, in callers direction, and
// immediately expands the first root so the first paint is useful. The tree's
// fetch reads the direction at request time, so a toggle needs no rewiring.
func (m *Model) Open(msg ilsp.CallHierarchyMsg) tea.Cmd {
	m.open = true
	m.incoming = true
	var fetch hiertree.Fetch[protocol.CallHierarchyItem]
	if msg.Fetch != nil {
		fetch = func(reqID int, item protocol.CallHierarchyItem) tea.Cmd {
			return msg.Fetch(reqID, item, m.incoming)
		}
	}
	return m.tree.Open(rows(msg.Roots), fetch)
}

// Close hides the overlay and drops the tree.
func (m *Model) Close() {
	m.open = false
	m.tree.Clear()
	m.prev.SetFocus(false)
}

// Apply consumes one expansion result, dropping replies whose node is gone
// (direction toggled or overlay reopened) or whose direction is stale.
func (m *Model) Apply(msg ilsp.CallHierarchyCallsMsg) {
	m.tree.Apply(msg.ReqID, msg.Incoming != m.incoming, rows(msg.Calls))
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
	// The excerpt column is a focusable read-only viewport (#2327): while it
	// holds the focus its motions win over the tree's, and esc hands the
	// keyboard back to the tree instead of closing the overlay.
	if codepreview.IsFocusKey(msg.String()) {
		m.prev.SetFocus(!m.prev.Focused())
		return nil
	}
	if m.prev.Focused() {
		if msg.String() == "esc" {
			m.prev.SetFocus(false)
			return nil
		}
		if m.prev.Key(msg.String()) {
			return nil
		}
	}
	onEnter := func(r row) tea.Cmd {
		e := r.Entry
		m.Close()
		return func() tea.Msg {
			return ilsp.DefinitionMsg{Path: e.Path, Line: e.Line, Col: e.Col}
		}
	}
	onToggle := func() tea.Cmd {
		// Toggle callers <-> callees: same roots, fresh tree.
		m.incoming = !m.incoming
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
	// Two columns need the extra width the single-column tree did not (#2053),
	// the find-in-path overlay's 120-cell cap.
	if boxW > 120 {
		boxW = 120
	}
	if boxW < 40 {
		boxW = min(40, m.width-2)
	}
	innerW := boxW - 4 // border + padding

	heading := "Call Hierarchy — Callees"
	if m.incoming {
		heading = "Call Hierarchy — Callers"
	}
	title := lipgloss.NewStyle().Bold(true).Underline(true).Render(heading)
	rows := []string{title, ""}

	rows = append(rows, strings.Split(m.body(innerW, pal), "\n")...)
	hint := "enter jumps · space expands · tab callers/callees · alt+p/ctrl+e preview · esc closes"
	if m.prev.Focused() {
		hint = "preview — j/k ctrl+d/u scroll · h/l scrolls right · z back to the entry · esc leaves"
	}
	rows = append(rows, "", lipgloss.NewStyle().Faint(true).Render(hint))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.BorderFocus).
		Padding(0, 1).
		Width(boxW - 2)
	return box.Render(strings.Join(rows, "\n"))
}

// body renders the overlay's two-column content (#2053): the call tree on the
// left, blank-padded to a stable height, and — when the box is wide enough to
// split — an excerpt of the selected entry's file behind a vertical rule.
func (m *Model) body(innerW int, pal *theme.Palette) string {
	h := m.listHeight()
	listW, previewW := m.previewSplit(innerW)
	tree := m.tree.RenderRows(listW, h, pal, m.displayPath, "no calls")
	return m.prev.Columns(tree, listW, previewW, h, m.target(), pal)
}

// previewSplit is the overlay's column geometry: the excerpt column adapts to
// the code around the selected entry, within the shared width bounds (#2327).
func (m *Model) previewSplit(innerW int) (listW, previewW int) {
	return m.prev.SplitFor(innerW, m.listHeight(), m.target())
}

// target is the selected row's source location, the zero Target when the tree
// is empty — which renders a blank column instead of an excerpt.
func (m *Model) target() codepreview.Target {
	r := m.tree.Current()
	if r == nil {
		return codepreview.Target{}
	}
	return codepreview.Target{Path: r.Entry.Path, Line: r.Entry.Line + 1}
}
