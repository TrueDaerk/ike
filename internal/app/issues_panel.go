package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
	"ike/internal/ghissues"
	"ike/internal/host"
	"ike/internal/pane"
)

// issues_panel.go wires the GitHub Issues tool window (#1934): a singleton
// pane listing the repository's open issues through the gh CLI, mirroring the
// Problems panel's toggle state machine. The pane is a pure consumer —
// forge.RefreshCmd fetches off-loop and the forge.IssuesMsg result is routed
// in here; the start-work and open-in-browser actions come back as request
// messages the app runs.

// IssuesToggleMsg runs issues.toggle.
type IssuesToggleMsg struct{}

// toggleIssuesPanel is the issues.toggle state machine, mirroring
// toggleProblemsPanel: no panel → open (returning the first fetch); unfocused
// → focus it; focused → return focus to the remembered pane.
func (m *Model) toggleIssuesPanel() tea.Cmd {
	if !m.activeWS().Panes.Has(pane.IssuesKey) {
		m.issuesReturnFocus = m.activeWS().Panes.Focused()
		return m.openIssuesPanel()
	}
	if m.activeWS().Panes.Focused() != pane.IssuesKey {
		m.issuesReturnFocus = m.activeWS().Panes.Focused()
		m.setFocus(pane.IssuesKey)
		// Looking at the issues window counts as viewing the pending forge
		// events (#2086): the unread badge clears.
		m.clearForgeUnread()
		return nil
	}
	target := m.issuesReturnFocus
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = m.activeEditorKey()
	}
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = pane.ExplorerKey
	}
	m.setFocus(target)
	return nil
}

// issuesPanel returns the singleton panel model, or nil when it is not open.
func (m Model) issuesPanel() *ghissues.Model {
	if !m.activeWS().Panes.Has(pane.IssuesKey) {
		return nil
	}
	return m.activeWS().Panes.Get(pane.IssuesKey).Issues()
}

// openIssuesPanel splits the active editor (fallback: focused leaf) at the
// adaptive placement (auxZone, #1588) with the singleton panel and starts the
// first fetch.
func (m *Model) openIssuesPanel() tea.Cmd {
	target := m.activeEditorKey()
	if target == "" {
		target = m.activeWS().Panes.Focused()
	}
	if target == "" || m.activeWS().Tree == nil {
		return nil
	}
	key := m.activeWS().Panes.AddIssues()
	if !m.insertToolPane(key, target, m.auxZone(target)) {
		m.activeWS().Panes.Close(key)
		return nil
	}
	// Opening the window views the pending forge events (#2086).
	m.clearForgeUnread()
	p := m.activeWS().Panes.Get(key).Issues()
	refresh := forge.RefreshCmd(".")
	p.SetRefresh(refresh)
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	if !p.Loaded() {
		p.MarkLoading()
		return refresh
	}
	return nil
}

// fillIssuesPanel routes one finished fetch into the pane, if it still exists.
func (m *Model) fillIssuesPanel(msg forge.IssuesMsg) {
	if p := m.issuesPanel(); p != nil {
		p.SetResult(msg)
	}
}

// finishStartWork reports the start-work outcome (#1934) and lets the VCS
// layer re-read the switched worktree.
func (m *Model) finishStartWork(msg forge.StartWorkDoneMsg) tea.Cmd {
	if msg.Err != nil {
		m.host.Notify(host.Error, "start work failed: "+msg.Err.Error())
		return nil
	}
	if msg.Warning != "" {
		m.host.Notify(host.Warn, "on "+msg.Branch+" — "+msg.Warning)
	} else {
		m.host.Notify(host.Info, "on branch "+msg.Branch)
	}
	return m.scheduleVCSRefresh()
}

// openIssueURL opens the issue's page in the platform browser ('o').
func (m *Model) openIssueURL(url string) tea.Cmd {
	if err := browserOpen(url); err != nil {
		m.host.Notify(host.Error, "open in browser failed: "+err.Error())
	}
	return nil
}
