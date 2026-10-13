package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexitypanel"
	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/scratch"
)

// complexity_panel.go wires the Complexity tool window (Roadmap 0550,
// #2917): a singleton bottom-split pane listing the project's functions
// with their cyclomatic / cognitive complexity from the project complexity
// index (complexityindex.go, #2916). The pane is a pure consumer: it is
// seeded from the index's snapshot when it opens or is restored, re-read on
// every ComplexityChangedMsg, and told where the focused editor's caret is
// on every settled Update pass so its File tab follows. Nothing here
// parses; the index measures on its own goroutines.

// ComplexityToggleMsg runs complexity.toggle.
type ComplexityToggleMsg struct{}

// ComplexityRefreshMsg runs complexity.refresh: rebuild the index (the
// pane's 'r').
type ComplexityRefreshMsg struct{}

// ComplexityExportMsg runs complexity.export: the pane's 'e' from the
// palette.
type ComplexityExportMsg struct{}

// ComplexityGroupMsg runs complexity.group (#2937): cycle the current
// tab's grouping — none → class → file on Project, none → class on File.
type ComplexityGroupMsg struct{}

// toolWindowMode is the persisted display mode of a tool window
// (paneIdentity.Mode): the Complexity window's grouping levels (#2937) and
// its hide-tests toggle (#2950), "" for every other kind and for a window
// in its default state.
func toolWindowMode(inst *pane.Instance) string {
	if inst != nil && inst.Kind() == pane.KindComplexity {
		return inst.Complexity().Mode()
	}
	return ""
}

// restoreToolWindowMode puts a saved toolWindowMode back on a restored
// tool window.
func restoreToolWindowMode(inst *pane.Instance, mode string) {
	if inst != nil && inst.Kind() == pane.KindComplexity && mode != "" {
		inst.Complexity().SetMode(mode)
	}
}

// cycleComplexityGroup is the complexity.group handler: an unopened panel
// is opened first, then the selected tab cycles its grouping; the Changes
// tab does not group and says so.
func (m *Model) cycleComplexityGroup() {
	p := m.complexityPanel()
	if p == nil {
		m.openComplexityPanel()
		if p = m.complexityPanel(); p == nil {
			return
		}
	}
	if !p.CycleGroup() {
		m.host.Notify(host.Info, "complexity: the Changes tab does not group")
		return
	}
	// The grouping is layout state: persist it now, so it survives
	// without a clean quit.
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// ComplexityToggleTestsMsg runs complexity.toggleTests (#2950): hide or
// show the test files on every tab of the Complexity window.
type ComplexityToggleTestsMsg struct{}

// toggleComplexityTests is the complexity.toggleTests handler: an unopened
// panel is opened first, then the test files flip; the state is layout
// state and saved at once, like the grouping.
func (m *Model) toggleComplexityTests() {
	p := m.complexityPanel()
	if p == nil {
		m.openComplexityPanel()
		if p = m.complexityPanel(); p == nil {
			return
		}
	}
	p.ToggleTests()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// ComplexitySitesMsg runs complexity.sites (#2946): expand the selected
// function row of the Complexity window into its complexity sites, or
// collapse it.
type ComplexitySitesMsg struct{}

// toggleComplexitySites is the complexity.sites handler: it needs an open
// panel with a function row selected; a selection with nothing to expand
// (a group row, a removed function, a function without sites) says so.
func (m *Model) toggleComplexitySites() {
	p := m.complexityPanel()
	if p == nil {
		m.openComplexityPanel()
		if p = m.complexityPanel(); p == nil {
			return
		}
	}
	if !p.ToggleSites() {
		m.host.Notify(host.Info, "complexity: the selected row has no sites to expand")
	}
}

// toggleComplexityPanel is the complexity.toggle state machine, mirroring
// toggleUsagePanel: no panel → open at the bottom; unfocused → focus it;
// focused → return focus to the remembered pane.
func (m *Model) toggleComplexityPanel() tea.Cmd {
	return m.togglePanel(pane.ComplexityKey, m.openComplexityPanel)
}

// complexityPanel returns the singleton panel model, or nil when it is not
// open.
func (m Model) complexityPanel() *complexitypanel.Model {
	if inst := m.toolWindow(pane.KindComplexity); inst != nil {
		return inst.Complexity()
	}
	return nil
}

// openComplexityPanel splits the active editor (fallback: focused leaf) at
// the bottom with the singleton panel, seeded from the index's current
// snapshot and following the focused editor.
func (m *Model) openComplexityPanel() tea.Cmd {
	m.openToolPane(m.activeWS().Panes.AddComplexity, fixedZone(layout.ZoneBottom), func(key string) {
		m.seedComplexityPanel(m.activeWS().Panes.Get(key).Complexity())
	})
	m.syncComplexityPanel()
	return nil
}

// seedComplexityPanel hands the panel the project root, the warning
// thresholds and the index's current snapshot — on open and on a layout
// restore alike.
func (m *Model) seedComplexityPanel(p *complexitypanel.Model) {
	if p == nil || m.complexityIndex == nil {
		return
	}
	p.SetRoot(m.complexityIndex.Root())
	c := config.Get().Complexity
	p.SetWarn(c.CCNWarn, c.CognitiveWarn, c.CommentDensityWarn)
	p.Set(m.complexityIndex.Snapshot(), m.complexityIndex.Stats())
	m.seedComplexityChanges(p)
}

// refreshComplexityPanel re-reads the snapshot into an open panel: the
// index reached a new generation.
func (m *Model) refreshComplexityPanel() {
	if p := m.complexityPanel(); p != nil && m.complexityIndex != nil {
		p.Set(m.complexityIndex.Snapshot(), m.complexityIndex.Stats())
	}
}

// syncComplexityPanel runs once per settled Update pass (the Update
// wrapper): while the panel is open its File tab follows the active
// editor's file and caret. A moved caret is an in-place re-mark, a buffer
// switch rebuilds the tab's rows from the snapshot already held; nothing
// is parsed.
func (m *Model) syncComplexityPanel() {
	p := m.complexityPanel()
	if p == nil {
		return
	}
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		return
	}
	line, _ := ed.CursorPos() // 0-based
	p.Follow(canonicalPath(ed.Path()), line)
}

// rebuildComplexityIndex drops the walk and starts a fresh one ('r' /
// complexity.refresh); the panel shows the scanning state until the
// settled generation change re-reads it.
func (m *Model) rebuildComplexityIndex() tea.Cmd {
	if m.complexityIndex == nil {
		return nil
	}
	if !m.complexityIndex.Rebuild() {
		if m.complexityIndex.Stats().Unavailable {
			m.host.Notify(host.Warn, "complexity: no syntax tree in this build, nothing to scan")
		} else {
			m.host.Notify(host.Warn, "complexity: the index is off (complexity.enabled)")
		}
		return nil
	}
	m.host.Notify(host.Info, "complexity: rescanning the project")
	m.refreshComplexityPanel()
	return nil
}

// exportComplexityPanel runs the pane's CSV export from the palette; an
// unopened panel is opened first so the export has a tab to render.
func (m *Model) exportComplexityPanel() tea.Cmd {
	p := m.complexityPanel()
	if p == nil {
		m.openComplexityPanel()
		p = m.complexityPanel()
		if p == nil {
			return nil
		}
	}
	return p.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
}

// handleComplexityExport writes the pane's CSV view to a scratch file ('e')
// and opens it, exactly like the Usage window's export.
func (m Model) handleComplexityExport(msg complexitypanel.ExportMsg) (tea.Model, tea.Cmd) {
	path, err := scratch.CreateWithContent("csv", []byte(msg.CSV))
	if err != nil {
		m.host.Notify(host.Error, "complexity: export failed: "+err.Error())
		return m, nil
	}
	m.host.Notify(host.Info, "complexity: "+strings.ToLower(msg.Label)+" exported to "+displayPath(path))
	return m.openPathAt(path, 0, 0)
}
