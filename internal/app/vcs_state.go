package app

import (
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/commitui"
	"ike/internal/editor"
	"ike/internal/pane"
	"ike/internal/vcs"
)

// Git status plumbing (Roadmap 0320, #462). The model holds one shared
// vcsState pointer: the latest snapshot plus the refresh scheduling flags.
// Refreshes are debounced (a watcher burst or save storm collapses into one
// `git status` run) and serialized (at most one git subprocess in flight; a
// trigger arriving mid-flight queues exactly one follow-up run).

// vcsDebounce collapses refresh triggers; watcher events are already
// debounced at 100ms, this adds slack for multi-file bursts (checkout, save
// all) so one git run covers them.
const vcsDebounce = 250 * time.Millisecond

// vcsInvalidateMsg reports one working-tree mutation (buffer save, mutating
// VCS command); the handler arms the debounce tick.
type vcsInvalidateMsg struct{}

// vcsTickMsg wakes the model to run the debounced git status refresh.
type vcsTickMsg struct{}

type vcsState struct {
	snap       *vcs.Snapshot // latest snapshot; nil = not a git repo (or unloaded)
	tickArmed  bool          // a vcsTickMsg is pending
	refreshing bool          // a git status run is in flight
	dirty      bool          // triggers arrived mid-flight: run again after
	branches   []vcs.Branch  // last fetched branch list, behind the picker (#467)
}

// scheduleVCSRefresh arms the debounce tick unless one is already pending.
// Callers batch the returned command with their own.
func (m Model) scheduleVCSRefresh() tea.Cmd {
	if m.vcs.tickArmed {
		return nil
	}
	m.vcs.tickArmed = true
	return tea.Tick(vcsDebounce, func(time.Time) tea.Msg { return vcsTickMsg{} })
}

// startVCSRefresh launches the git status run, or queues a follow-up when one
// is already in flight.
func (m Model) startVCSRefresh() tea.Cmd {
	if m.vcs.refreshing {
		m.vcs.dirty = true
		return nil
	}
	m.vcs.refreshing = true
	return vcs.Refresh(".")
}

// applyVCSSnapshot stores a finished run's snapshot and chains the queued
// follow-up run, if triggers arrived while git was running.
func (m Model) applyVCSSnapshot(msg vcs.SnapshotMsg) tea.Cmd {
	m.vcs.refreshing = false
	m.vcs.snap = msg.Snap
	// Consumers read the snapshot per frame; the explorer holds its own
	// reference (#463).
	if m.panes.Has(pane.ExplorerKey) {
		m.explorer().SetVCS(msg.Snap)
	}
	// The open commit dialog re-reads the changed files (#465); losing the
	// repo underneath it closes it.
	if m.commitUI.IsOpen() {
		if msg.Snap == nil {
			m.commitUI.Close()
		} else {
			m.commitUI.SetRows(commitRows(msg.Snap))
		}
	}
	if m.vcs.dirty {
		m.vcs.dirty = false
		m.vcs.refreshing = true
		return vcs.Refresh(".")
	}
	// Recompute the gutter diff markers of every open buffer against the new
	// snapshot (#464); clean/untracked buffers get their markers cleared.
	return tea.Batch(m.vcsMarksCmds()...)
}

// vcsMarksCmds fans one marks recompute out per open document.
func (m Model) vcsMarksCmds() []tea.Cmd {
	seen := map[string]bool{}
	var cmds []tea.Cmd
	for _, key := range m.panes.Keys() {
		inst := m.panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			if !ed.HasFile() || seen[ed.Path()] {
				continue
			}
			seen[ed.Path()] = true
			cmds = append(cmds, m.vcsMarksCmd(ed))
		}
	}
	return cmds
}

// vcsMarksCmd recomputes one buffer's gutter diff markers (#464). Buffers
// without HEAD-relative changes — clean, untracked, outside the repo — get a
// clearing message instead of a git subprocess.
func (m Model) vcsMarksCmd(ed *editor.Model) tea.Cmd {
	if ed == nil || !ed.HasFile() {
		return nil
	}
	snap, path := m.vcs.snap, ed.Path()
	switch snap.Status(path) {
	case vcs.StatusModified, vcs.StatusConflicted, vcs.StatusRenamed:
		return vcs.RefreshMarks(snap.Root, path, ed.Text())
	default:
		return func() tea.Msg { return vcs.MarksMsg{Path: path} }
	}
}

// OpenCommitMsg opens the commit dialog (vcs.commit, #465).
type OpenCommitMsg struct{}

// commitRows converts snapshot entries into the dialog's rows, sorted by path.
func commitRows(snap *vcs.Snapshot) []commitui.Row {
	rows := make([]commitui.Row, 0, len(snap.Entries))
	for _, e := range snap.Entries {
		rows = append(rows, commitui.Row{
			Path:    e.Path,
			Status:  e.Status,
			Staged:  e.Staged(),
			Partial: e.PartiallyStaged(),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Path < rows[j].Path })
	return rows
}

// VCSSnapshot exposes the current snapshot to tests and consumers; nil means
// "not a git repository" and must render as a clean no-op everywhere.
func (m Model) VCSSnapshot() *vcs.Snapshot { return m.vcs.snap }
