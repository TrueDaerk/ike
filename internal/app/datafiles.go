package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/datasrc"
	"ike/internal/dataview"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/registry"
)

// OpenDataMsg asks the root model to open a database file in a data viewer
// pane (#1764). Dispatched by the "data" file handler when the explorer, `:e`
// or the CLI opens a database, so its binary pages never land in a text
// buffer.
type OpenDataMsg struct{ Path string }

// dataProvider is the compile-in plugin claiming database files by extension
// and content sniff, routing them to the data viewer.
type dataProvider struct{}

func (dataProvider) ID() string { return "data" }

func (dataProvider) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{FileHandlers: []plugin.FileHandler{{
		ID:         "data.view",
		Extensions: []string{".db", ".sqlite", ".sqlite3", ".duckdb", ".ddb", ".parquet", ".pqt"},
		// The magic sniff catches a SQLite or DuckDB database, or a Parquet
		// table, under any other name.
		Match: datasrc.IsDatabase,
		Open: func(h host.API, path string) tea.Cmd {
			return h.Dispatch(OpenDataMsg{Path: path})
		},
	}}}
}

func init() { registry.Register(dataProvider{}) }

// openDataPane opens (or refocuses) the data viewer for path, split off the
// leaf viewerSplitTarget picks — the pane the user last worked in, never the
// explorer they opened the file from (#1779).
func (m *Model) openDataPane(path string) {
	if hostKey, tabIdx, _, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindData && c.Data().Path() == path
	}); ok {
		m.focusContentAt(hostKey, tabIdx) // may live in a tab (#1778)
		return
	}
	target := m.viewerSplitTarget()
	key := m.activeWS().Panes.AddDataView(path)
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

// showTableSchema shows one table's DDL in a read-only editor tab (#1764).
// The buffer's virtual path is <db>!<table>.sql, so the tab titles itself
// "users.sql (app.db)" and the .sql tail picks up SQL highlighting — while
// nothing on disk answers to the whole string, which is why the buffer can
// never be written back.
func (m *Model) showTableSchema(msg dataview.ShowSchemaMsg) tea.Cmd {
	vpath := archiveEntryPath(msg.Path, msg.Table+".sql")
	key := m.fileEditorKey()
	if key == "" {
		key = m.spawnEditor()
	}
	inst := m.activeWS().Panes.Get(key)
	if inst == nil {
		return nil
	}
	if idx := inst.TabForPath(vpath); idx >= 0 {
		m.activateTab(inst, idx)
		m.setFocus(key)
		return nil
	}
	// Same tab policy as a file open (#156): an empty scratch tab is filled in
	// place, anything else gets a fresh tab.
	if ed := inst.Editor(); ed == nil || !ed.IsEmpty() {
		inst.AddTab()
		m.installEmitter(key)
	}
	ed := inst.Editor()
	if ed == nil {
		return nil
	}
	ed.ShowReadOnly(vpath, msg.SQL+"\n")
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	return ed.Reparse()
}
