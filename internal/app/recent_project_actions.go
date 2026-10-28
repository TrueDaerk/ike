package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/palette"
	"ike/internal/project"
)

// recent_project_actions.go holds the row actions of the recent-files
// dialog's Recent Projects column (#3083). Telemetry showed projects being
// switched *into* only to be closed and left again; the column already marks
// the workspaces still open in memory with "●" (#820), and these two
// Palette-scoped commands act on the focused row without a switch:
//
//   - project.closeFromList (cmd+backspace / ctrl+backspace, and the Global
//     close chord cmd+shift+w while the list is up) closes an open project
//     through the close-from-list path — busy workspaces get the #821 guard
//     prompt, the session was persisted when the workspace was parked, and
//     the teardown is timed as a project.close op. The row stays listed,
//     just without its badge.
//   - project.removeFromList (cmd+delete / ctrl+delete) forgets a closed
//     project's history entry and is refused for an open one.
//
// The active project is never a row of this column (it is excluded from the
// list), so closing it stays project.close's job — which switches to the most
// recently used parked project.

const (
	recentCloseCommand  = "project.closeFromList"
	recentRemoveCommand = "project.removeFromList"
)

// recentRowCommands are the palette overlay commands that only own their
// chords while a project row of the recent-files dialog has the focus.
var recentRowCommands = map[string]bool{recentCloseCommand: true, recentRemoveCommand: true}

// RecentCloseRowMsg runs project.closeFromList on the focused row.
type RecentCloseRowMsg struct{}

// RecentRemoveRowMsg runs project.removeFromList on the focused row.
type RecentRemoveRowMsg struct{}

// recentProjectRow resolves the focused row of the open recent-files dialog
// when it is a Recent Projects row, whose aux action says whether the
// project is open (close) or not (remove from the history).
func (m Model) recentProjectRow() (palette.Item, bool) {
	if prefix, ok := m.palette.LockedPrefix(); !ok || prefix != palette.RecentPrefix {
		return palette.Item{}, false
	}
	it, ok := m.palette.FocusedItem()
	if !ok {
		return palette.Item{}, false
	}
	switch it.Aux.(type) {
	case project.CloseWorkspaceMsg, project.RemoveFromHistoryMsg:
		return it, true
	}
	return palette.Item{}, false
}

// noRecentRowNotice is the answer when the commands run with no project row
// focused — e.g. picked from the command palette.
const noRecentRowNotice = "focus a project in the Recent Files list (cmd+e, tab) first"

// handleRecentRowClose closes the focused open project without switching.
func (m Model) handleRecentRowClose() (tea.Model, tea.Cmd) {
	it, ok := m.recentProjectRow()
	if !ok {
		m.host.Notify(host.Info, noRecentRowNotice)
		return m, nil
	}
	switch aux := it.Aux.(type) {
	case project.CloseWorkspaceMsg:
		return m.closeWorkspaceFromList(aux.Path)
	case project.RemoveFromHistoryMsg:
		m.host.Notify(host.Info, it.Title+" is not open")
	}
	return m, nil
}

// handleRecentRowRemove drops the focused closed project from the history;
// an open project is refused, so its row never vanishes under a live
// workspace.
func (m Model) handleRecentRowRemove() (tea.Model, tea.Cmd) {
	it, ok := m.recentProjectRow()
	if !ok {
		m.host.Notify(host.Info, noRecentRowNotice)
		return m, nil
	}
	switch aux := it.Aux.(type) {
	case project.CloseWorkspaceMsg:
		m.host.Notify(host.Info, it.Title+" is open — close it first (cmd+backspace)")
	case project.RemoveFromHistoryMsg:
		return m.Update(aux)
	}
	return m, nil
}

// closeWorkspaceFromList unloads the background workspace at root without
// switching (#820) — sessions terminated, memory freed; the history entry
// stays. The active workspace cannot be closed this way, and a busy one goes
// through the #821 confirm guard (closing the palette, so the prompt owns the
// keyboard). An idle close keeps the palette open; the badge disappears.
func (m Model) closeWorkspaceFromList(root string) (tea.Model, tea.Cmd) {
	if m.activeWS() != nil && m.activeWS().Root == root {
		m.host.Notify(host.Info, "cannot close the active project from the list")
		return m, nil
	}
	w := m.ws.Peek(root)
	if w == nil {
		return m, nil // already gone (evicted meanwhile)
	}
	if act := collectActivity(w); act.busy() {
		m.palette.Close()
		m.openWsClosePrompt(root, act)
		return m, nil
	}
	return m, m.finishWorkspaceClose(root)
}
