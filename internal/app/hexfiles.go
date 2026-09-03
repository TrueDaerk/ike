package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/registry"
)

// OpenHexMsg asks the root model to open a file in a hex viewer pane (#2420).
// Dispatched by the "hex" file handler when a binary file with no dedicated
// viewer would otherwise land in a text buffer, and by the "Open file as…"
// chooser. Forced marks the explicit chooser pick, which the files.binary_open
// setting must not redirect back to the editor.
type OpenHexMsg struct {
	Path   string
	Forced bool
}

// hexProvider is the compile-in plugin claiming binary files by content
// sniff — a NUL byte in the head and no other viewer's magic — routing them
// to the hex viewer instead of a text buffer.
type hexProvider struct{}

func (hexProvider) ID() string { return "hex" }

func (hexProvider) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{FileHandlers: []plugin.FileHandler{{
		ID: "hex.view",
		// No extensions on purpose: the claim is by content only, so a
		// binary-looking .db or .png still reaches its dedicated viewer —
		// handlers match in ID order and the archive/data/gzip sniffs run
		// before this one; the image sniff runs after, hence the exclusion.
		Match: func(path string, head []byte) bool {
			return isBinary(head) && !sniffImage(head)
		},
		Open: func(h host.API, path string) tea.Cmd {
			return h.Dispatch(OpenHexMsg{Path: path})
		},
	}}}
}

func init() { registry.Register(hexProvider{}) }

// openHexPane opens (or refocuses) the hex viewer for path as a tab in the
// pane the open asked for — the focused pane for the palette (#1825), the
// last-focused editor for the explorer's default open (#1851) — and otherwise
// split off the leaf viewerSplitTarget picks, the pane the user last worked
// in (#1779). Reads are windowed, so the pane appears at once whatever the
// file size.
func (m *Model) openHexPane(path string) {
	tabHost := m.takeViewerTabHost()
	if hostKey, tabIdx, _, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindHex && c.Hex().Path() == path
	}); ok {
		m.focusContentAt(hostKey, tabIdx) // may live in a tab (#1778)
		return
	}
	if tabHost != "" {
		if _, ok := m.openContentTab(tabHost, pane.KindHex, path); ok {
			return
		}
	}
	target := m.viewerSplitTarget()
	key := m.activeWS().Panes.AddHexView(path)
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, key, layout.ZoneRight)
	if !ok {
		m.activeWS().Panes.Close(key)
		return
	}
	m.activeWS().Tree = tree
	m.layout()
	m.setFocus(key)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// binaryOpensInEditor reads files.binary_open (#2420): "editor" sends a
// sniffed binary to the text editor (insight off) instead of the hex viewer;
// anything else — the default "hex" included — keeps the hex viewer.
func (m Model) binaryOpensInEditor() bool {
	cfg := m.host.Config()
	if cfg == nil {
		return false
	}
	v, ok := cfg.Get("files.binary_open")
	return ok && v == "editor"
}
