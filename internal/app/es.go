package app

// es.go wires the Elasticsearch console (#1927) into the root model: one
// palette command per configured endpoint opens (or refocuses) that cluster's
// console pane, the pane's background results route back by key, its
// read-only JSON views (mapping, hit, aggregations) open like the data
// viewer's schema buffer, and a query buffer runs against the console with
// es.run. The pane itself is internal/espane; the cluster client and the
// query-buffer naming are internal/esq.

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/espane"
	"ike/internal/esq"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/plugin"
)

// OpenESConsoleMsg asks the root model to open (or refocus) the console pane
// of the named configured endpoint. Dispatched by the per-endpoint palette
// commands.
type OpenESConsoleMsg struct{ Endpoint string }

// ESRunMsg asks the root model to run the active editor's query buffer
// against its endpoint's console. Dispatched by the es.run command.
type ESRunMsg struct{}

// esCommands builds one palette command per configured endpoint, plus the
// editor-scoped run command. It runs on every registry query (Capabilities is
// lazy, like toolCommands), so a config reload adding or removing endpoints
// re-shapes the command set without re-registration.
func esCommands() []plugin.Command {
	cmds := []plugin.Command{{
		ID:    "es.run",
		Title: "ES: Run Query Buffer",
		Scope: plugin.PaneScope("editor"),
		Run:   func(h host.API) tea.Cmd { return h.Dispatch(ESRunMsg{}) },
	}}
	for _, ep := range esq.Endpoints() {
		if ep.Name == "" {
			continue
		}
		cmds = append(cmds, appCommand("es.console."+toolSlug(ep.Name), "ES Console: "+ep.Name, OpenESConsoleMsg{Endpoint: ep.Name}))
	}
	return cmds
}

// openESPane opens (or refocuses) the console for the named endpoint — as a
// tab in the pane the open asked for when one is armed, otherwise split off
// the viewerSplitTarget leaf, the same funnel as the data viewer. The pane
// appears at once; connecting to the cluster is the returned background
// command.
func (m *Model) openESPane(endpoint string) tea.Cmd {
	return m.openViewerPane(pane.KindES, endpoint, func(c *pane.Instance) bool {
		return c.Kind() == pane.KindES && c.ES().Endpoint() == endpoint
	}, func() string { return m.activeWS().Panes.AddES(endpoint) })
}

// esResult routes one background console result to the pane that asked for it
// — dedicated or tab-nested (#1778), matched by the model's own key.
func (m *Model) esResult(msg espane.ResultMsg) tea.Cmd {
	return m.routeResult(msg, func(c *pane.Instance) bool {
		return c.Kind() == pane.KindES && c.ES().Key() == msg.Key
	}, msg.Discard)
}

// initESPanes starts the background connect of every console that has not
// begun one — the restore paths build their panes without a command of their
// own, exactly like the data viewer's initDataPanes.
func (m Model) initESPanes() []tea.Cmd {
	var cmds []tea.Cmd
	m.contentInstances(func(_ string, _ int, inst *pane.Instance) bool {
		if inst.Kind() == pane.KindES {
			if cmd := inst.Init(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return true
	})
	return cmds
}

// showESJSON shows a console JSON document — an index mapping, one hit, a
// result's aggregations — in a read-only editor tab. The virtual path
// follows the schema-buffer convention (#1764): "<endpoint>!<name>.json"
// names nothing on disk, titles the tab, and its .json tail picks up JSON
// highlighting and folding.
func (m *Model) showESJSON(msg espane.ShowJSONMsg) tea.Cmd {
	vpath := archiveEntryPath(msg.Endpoint, msg.Name+".json")
	key := m.fileEditorKey()
	if key == "" {
		key = m.spawnEditor()
	}
	inst := m.activeWS().Panes.Get(key)
	if inst == nil {
		return nil
	}
	if idx := inst.TabForPath(vpath); idx >= 0 {
		// The same document again (a re-shown mapping): refresh in place.
		m.activateTab(inst, idx)
		m.setFocus(key)
		if ed := inst.EditorForPath(vpath); ed != nil {
			ed.ShowReadOnly(vpath, msg.JSON+"\n")
			return ed.Reparse()
		}
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
	ed.ShowReadOnly(vpath, msg.JSON+"\n")
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	return ed.Reparse()
}

// openESQuery opens the index's query buffer in an editor tab, allocating the
// file (seeded with a runnable match-all body) on first use. It is an
// ordinary JSON file (internal/esq/query.go), so the editor, highlighting and
// the mapping-aware completion source all apply without special casing.
func (m Model) openESQuery(msg espane.OpenQueryMsg) (tea.Model, tea.Cmd) {
	path, err := esq.QueryPath(msg.Endpoint, msg.Index)
	if err != nil {
		m.host.Notify(host.Warn, "es: "+err.Error())
		return m, nil
	}
	return m.openPath(path, false)
}

// runESQuery runs the active editor's query buffer against its endpoint's
// console: the buffer text becomes the index's active query and the console
// greets it with a fresh first page. A console that is not open yet opens
// first and runs the query once its cluster listing lands.
func (m *Model) runESQuery() tea.Cmd {
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() || !esq.IsQueryPath(ed.Path()) {
		m.host.Notify(host.Info, "es.run: the active buffer is not an ES query buffer (open one with q in a console)")
		return nil
	}
	endpoint, index, ok := esq.QueryRef(ed.Path())
	if !ok {
		m.host.Notify(host.Warn, "es.run: cannot resolve the buffer's endpoint and index")
		return nil
	}
	body := ed.Text()
	if _, _, inst, found := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindES && c.ES().Endpoint() == endpoint
	}); found {
		cmd, ran := inst.ES().RunQuery(index, body)
		if !ran {
			// Still connecting, or the index vanished from the listing: queue
			// the query for the landing open to pick up.
			inst.ES().QueueQuery(index, body)
		}
		return cmd
	}
	openCmd := m.openESPane(endpoint)
	if _, _, inst, found := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindES && c.ES().Endpoint() == endpoint
	}); found {
		inst.ES().QueueQuery(index, body)
	}
	return openCmd
}
