package app

// merge_view.go wires the three-way merge view (#1478) into the root model:
// entry points (the vcs.mergeFile command and enter on a conflicted VCS-panel
// row), the stage fetch, pane placement, the apply command (save + stage) and
// the unresolved-conflicts close guard.

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/ui"
	"ike/internal/vcs"
)

// MergeFileMsg runs vcs.mergeFile: open the three-way merge view for the
// focused conflicted file.
type MergeFileMsg struct{}

// MergeApplyMsg runs vcs.mergeApply: save the focused merge view's result to
// the file, stage it and close the view. Unresolved conflicts block it.
type MergeApplyMsg struct{}

// mergeFocusedFile validates the focused file and fetches its merge stages.
func (m Model) mergeFocusedFile() (tea.Model, tea.Cmd) {
	if m.vcs.snap == nil {
		m.host.Notify(host.Info, "not a git repository")
		return m, nil
	}
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, "no file to merge")
		return m, nil
	}
	return m.mergePath(ed.Path())
}

// mergePath opens the merge view for path when it is conflicted.
func (m Model) mergePath(path string) (tea.Model, tea.Cmd) {
	snap := m.vcs.snap
	if snap.Status(path) != vcs.StatusConflicted {
		m.host.Notify(host.Info, "no merge conflict in "+baseName(path))
		return m, nil
	}
	return m, vcs.MergeStagesCmd(snap.Root, path)
}

// openMergePane places a merge view fed with the fetched stages beside the
// editor; re-opening the same file focuses the existing view instead.
func (m *Model) openMergePane(msg vcs.MergeStagesMsg) {
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst != nil && inst.Kind() == pane.KindMerge && inst.Merge().Path() == msg.Path {
			m.setFocus(key)
			return
		}
	}
	key := m.activeWS().Panes.AddMerge(msg.Path)
	if !m.placeDiffLeaf(key) {
		return
	}
	m.activeWS().Panes.Get(key).Merge().SetContents(msg.Base, msg.Ours, msg.Theirs)
	m.setFocus(key)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// mergeApply saves the focused merge view's result, stages the file and
// closes the view. Unresolved conflicts block with a notification.
func (m Model) mergeApply() (tea.Model, tea.Cmd) {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindMerge {
		m.host.Notify(host.Info, "no merge view focused")
		return m, nil
	}
	mg := inst.Merge()
	if n := mg.Unresolved(); n > 0 {
		m.host.Notify(host.Warn, strconv.Itoa(n)+" unresolved conflicts — resolve them before applying")
		return m, nil
	}
	writeCmd := inst.Update(editor.ActionMsg{Action: "write"})
	if mg.Editor().Dirty() {
		// The write failed (read-only file, full disk): keep the view open.
		m.host.Notify(host.Error, "merge result not saved")
		return m, writeCmd
	}
	cmds := []tea.Cmd{writeCmd}
	if snap := m.vcs.snap; snap != nil {
		cmds = append(cmds, vcs.StageCmd(snap.Root, mg.Path()))
	}
	m.host.Notify(host.Info, "merge result saved and staged: "+baseName(mg.Path()))
	m.closePane(inst.Key())
	return m, tea.Batch(cmds...)
}

// guardMergeClose opens the merge close guard when closing inst would drop
// unresolved conflicts or an unsaved result; reports whether it did.
func (m *Model) guardMergeClose(inst *pane.Instance) bool {
	if inst == nil || inst.Kind() != pane.KindMerge {
		return false
	}
	unresolved := inst.Merge().Unresolved()
	if unresolved == 0 && !inst.Merge().Editor().Dirty() {
		return false
	}
	m.openMergeClosePrompt(inst.Key(), unresolved)
	return true
}

// openMergeClosePrompt shows the guard for a pending merge-view close.
func (m *Model) openMergeClosePrompt(key string, unresolved int) {
	m.mergeClosePending = key
	body := "The merge result is not saved.\n\n"
	if unresolved > 0 {
		body = strconv.Itoa(unresolved) + " conflicts are still unresolved.\n\n"
	}
	body += guardLine("d", "close and discard the merge result", true) +
		guardCancel("cancel — keep resolving")
	m.shell.SetContent(ui.ModelContent{
		Heading: "Close merge view",
		Body:    func() string { return body },
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// mergeClosePromptOpen reports whether the merge close guard owns the keyboard.
func (m Model) mergeClosePromptOpen() bool { return m.mergeClosePending != "" && m.shell.IsOpen() }

// updateMergeClosePrompt consumes every key while the guard is open: d — or
// enter, the primary option — discards and closes, esc cancels.
func (m Model) updateMergeClosePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch guardAnswer(msg, "d") {
	case "d":
		key := m.mergeClosePending
		m.mergeClosePending = ""
		m.shell.Close()
		m.closePane(key)
	case "esc":
		m.mergeClosePending = ""
		m.shell.Close()
	}
	return m, nil
}
