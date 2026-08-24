package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/keymap"
	ilsp "ike/internal/lsp"
	"ike/internal/palette"
	"ike/internal/pane"
)

// find_panel.go implements "Open in Find Window" (#2055): the hit list of a
// transient palette overlay — search everywhere and the find-usages popup —
// is tipped into the persistent bottom panel so it can be worked through
// entry by entry instead of vanishing on the first jump.
//
// It reuses the Usages tool window (#1155) rather than adding a second
// results pane: both list "locations grouped by file, enter jumps there", and
// one singleton keeps the layout persistence, the toggle state machine, the
// mouse handling and the tool-window menu entry single-sourced. The pane's
// heading follows what filled it ("Find: foo" vs "Usages: Foo"), so the two
// sources stay distinguishable; a second hand-off replaces the content, the
// way JetBrains' Find tool window reuses its tab.

// OpenInFindPanelMsg runs find.openInPanel: hand the open overlay's current
// hits to the persistent panel and close the overlay.
type OpenInFindPanelMsg struct{}

// findPanelCommand is the command id the Palette-context binding runs.
const findPanelCommand = "find.openInPanel"

// paletteOverlayCommands are the commands a Palette-context binding may run
// while the palette overlay owns the keyboard. Every other binding stays out:
// the overlay is a text field, and swallowing arbitrary chords there would
// eat query editing.
var paletteOverlayCommands = map[string]bool{findPanelCommand: true}

// paletteBindingCmd resolves one key press against the Palette-context
// bindings while the overlay is up (#2055). The overlay branch of the key
// dispatch short-circuits the keymap layer, so the few overlay-level commands
// are looked up here directly — single-step chords only, and only the ones
// listed in paletteOverlayCommands.
func (m *Model) paletteBindingCmd(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k, ok := keymap.FromKeyMsg(msg)
	if !ok || m.bindings == nil {
		return nil, false
	}
	table := m.bindings.Table()
	if table == nil {
		return nil, false
	}
	b, found := table.Lookup(keymap.Chord{Steps: []keymap.Key{k}}, keymap.Palette)
	if !found || !paletteOverlayCommands[b.Command] {
		return nil, false
	}
	c, okc := m.reg.Command(b.Command)
	if !okc {
		return nil, false
	}
	return m.dispatchCommand(b.Command, c), true
}

// openInFindPanel moves the palette's current result rows into the panel and
// closes the overlay. Rows that carry no location — a command hit in search
// everywhere — are dropped; a list with none of them leaves the overlay open
// and says why.
func (m *Model) openInFindPanel() {
	title, items, ok := m.palette.PanelResults()
	if !ok {
		m.host.Notify(host.Info, "no result list to open in the Find panel")
		return
	}
	refs := panelRefs(items)
	if len(refs) == 0 {
		m.host.Notify(host.Info, "no locations in this list to open in the Find panel")
		return
	}
	m.palette.Close()
	if m.usagesPanel() == nil {
		m.usagesReturnFocus = m.activeWS().Panes.Focused()
		m.openUsagesPanel()
	}
	p := m.usagesPanel()
	if p == nil {
		return
	}
	p.SetTitled(title, refs)
	m.setFocus(pane.UsagesKey)
}

// panelRefs converts palette rows into panel entries, keeping the list order.
// The row's activation message is the location: go-to-definition and peek
// rows (symbols, usages) carry line and column, a file row opens at its top.
// Everything else — commands, project switches — has no place to jump to and
// is skipped.
func panelRefs(items []palette.Item) []ilsp.Reference {
	var out []ilsp.Reference
	for _, it := range items {
		switch msg := it.Msg.(type) {
		case ilsp.DefinitionMsg:
			out = append(out, ilsp.Reference{Path: msg.Path, Line: msg.Line, Col: msg.Col, Preview: panelPreview(it)})
		case ilsp.PeekDefinitionMsg:
			out = append(out, ilsp.Reference{Path: msg.Path, Line: msg.Line, Col: msg.Col, Preview: panelPreview(it)})
		case palette.OpenFileMsg:
			out = append(out, ilsp.Reference{Path: msg.Path, Preview: panelPreview(it)})
		}
	}
	return out
}

// panelPreview is the row's line text for the panel: the detail chip a
// usages row carries, falling back to its title (a symbol or file name).
func panelPreview(it palette.Item) string {
	if it.Detail != "" {
		return it.Detail
	}
	return it.Title
}
