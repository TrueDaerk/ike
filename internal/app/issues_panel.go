package app

import (
	"strconv"

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
	return m.togglePanelWith(pane.IssuesKey, m.openIssuesPanel, func() tea.Cmd {
		// Looking at the issues window counts as viewing the pending forge
		// events (#2086): the unread badge clears.
		m.clearForgeUnread()
		return nil
	})
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
	var p *ghissues.Model
	if !m.openToolPane(m.activeWS().Panes.AddIssues, m.auxZone, func(key string) {
		// Opening the window views the pending forge events (#2086).
		m.clearForgeUnread()
		p = m.activeWS().Panes.Get(key).Issues()
		p.SetRefresh(forge.RefreshFactory("."))
		p.SetTimeline(forge.TimelineFactory("."))
		p.SetMutate(forge.MutateFactory("."))
		p.SetMeta(forge.MetaFactory("."))
		p.SetPRDetailFetch(forge.PRDetailFactory("."))
		p.SetPRAction(forge.PRActionFactory("."))
	}) {
		return nil
	}
	if !p.Loaded() {
		// The persisted snapshot (#2108) renders while the fetch runs: the
		// seed resolves off the Update loop (it reads a file and the remote
		// key) and lands as a forge.CachedListingMsg; SetCached drops it if
		// the fetch happens to win the race.
		return tea.Batch(forge.LoadCacheCmd("."), p.Refresh())
	}
	return nil
}

// fillIssuesPanel routes one finished fetch into the pane, if it still exists.
func (m *Model) fillIssuesPanel(msg forge.IssuesMsg) {
	if p := m.issuesPanel(); p != nil {
		p.SetResult(msg)
	}
}

// fillIssuesTimeline routes one fetched timeline page into the pane (#2084)
// and returns the follow-up page a depth-restoring refetch still owes (#2113).
func (m *Model) fillIssuesTimeline(msg forge.TimelineMsg) tea.Cmd {
	if p := m.issuesPanel(); p != nil {
		return p.SetTimelineResult(msg)
	}
	return nil
}

// fillIssuesMeta routes one repository-metadata probe into the pane (#2088):
// the capability gate in front of the mutation actions plus the label and
// user sets their pickers list.
func (m *Model) fillIssuesMeta(msg forge.RepoMetaMsg) {
	if p := m.issuesPanel(); p != nil {
		p.SetRepoMeta(msg)
	}
}

// finishIssueMutation routes one finished mutation into the pane (#2088),
// which rolls its optimistic state back on a rejection and refetches on
// success; a rejection is toasted too, so it is not missed while the pane is
// unfocused.
func (m *Model) finishIssueMutation(msg forge.MutationMsg) tea.Cmd {
	p := m.issuesPanel()
	if p == nil {
		return nil
	}
	if msg.Err != nil {
		m.host.Notify(host.Error, "issue #"+strconv.Itoa(msg.Issue)+": "+msg.Kind+" change failed: "+msg.Err.Error())
	}
	return p.SetMutationResult(msg)
}

// fillPRDetail routes one fetched PR detail into the pane (#2089).
func (m *Model) fillPRDetail(msg forge.PRDetailMsg) {
	if p := m.issuesPanel(); p != nil {
		p.SetPRDetailResult(msg)
	}
}

// finishPRAction routes one finished merge/close into the pane (#2089), which
// surfaces the forge's own reason on a rejection and refetches on success; a
// rejection is toasted too, so it is not missed while the pane is unfocused.
func (m *Model) finishPRAction(msg forge.PRActionMsg) tea.Cmd {
	p := m.issuesPanel()
	if p == nil {
		return nil
	}
	if msg.Err != nil {
		m.host.Notify(host.Error, "PR #"+strconv.Itoa(msg.PR)+": "+msg.Kind+" failed: "+msg.Err.Error())
	}
	return p.SetPRActionResult(msg)
}

// finishBranchCleanup reports the post-merge cleanup outcome (#2089) and lets
// the VCS layer re-read the switched worktree, mirroring finishStartWork.
func (m *Model) finishBranchCleanup(msg forge.CleanupDoneMsg) tea.Cmd {
	if msg.Err != nil {
		m.host.Notify(host.Error, "branch cleanup failed: "+msg.Err.Error())
		return nil
	}
	if msg.Warning != "" {
		m.host.Notify(host.Warn, "cleaned up "+msg.Branch+" — "+msg.Warning)
	} else {
		m.host.Notify(host.Info, "cleaned up "+msg.Branch)
	}
	return m.scheduleVCSRefresh()
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
