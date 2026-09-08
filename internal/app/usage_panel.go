package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/scratch"
	"ike/internal/usagepanel"
)

// usage_panel.go wires the Usage tool window (#2552): a singleton
// bottom-split pane reporting top commands by source, unbound chords per
// context, palette dismissal rates per mode and slow operations from the
// local usage log.
//
// It shares the Time window's reader and report (#2426): one background read
// over the telemetry directory fills both aggregates, so opening both windows
// costs one directory scan, and the mtime cache is shared. Everything here
// reads. Nothing writes to the log, and nothing uploads.

// UsageToggleMsg runs usage.toggle.
type UsageToggleMsg struct{}

// UsageRefreshMsg runs usage.refresh: re-read the log (the pane's 'r').
type UsageRefreshMsg struct{}

// toggleUsagePanel is the usage.toggle state machine, mirroring
// toggleTimePanel: no panel → open at the bottom; unfocused → focus it;
// focused → return focus to the remembered pane.
func (m *Model) toggleUsagePanel() tea.Cmd {
	return m.togglePanel(pane.UsageKey, m.openUsagePanel)
}

// usagePanel returns the singleton panel model, or nil when it is not open.
func (m Model) usagePanel() *usagepanel.Model {
	if !m.activeWS().Panes.Has(pane.UsageKey) {
		return nil
	}
	return m.activeWS().Panes.Get(pane.UsageKey).Usage()
}

// openUsagePanel splits the active editor (fallback: focused leaf) at the
// bottom with the singleton panel, seeded from the last report, and starts a
// fresh read.
func (m *Model) openUsagePanel() tea.Cmd {
	if !m.openToolPane(m.activeWS().Panes.AddUsage, fixedZone(layout.ZoneBottom), func(key string) {
		p := m.activeWS().Panes.Get(key).Usage()
		if m.timeReport != nil {
			p.Set(m.timeReport)
		} else {
			p.SetLoading(true)
		}
	}) {
		return nil
	}
	return m.timeReadCmd()
}

// handleUsageExport writes the pane's CSV view to a scratch file ('e') and
// opens it, exactly like the Time window's export.
func (m Model) handleUsageExport(msg usagepanel.ExportMsg) (tea.Model, tea.Cmd) {
	path, err := scratch.CreateWithContent("csv", []byte(msg.CSV))
	if err != nil {
		m.host.Notify(host.Error, "usage: export failed: "+err.Error())
		return m, nil
	}
	m.host.Notify(host.Info, "usage: "+strings.ToLower(msg.Label)+" exported to "+displayPath(path))
	return m.openPathAt(path, 0, 0)
}
