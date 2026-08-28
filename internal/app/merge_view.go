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
	// Complete blocks are gone, but a half-edited one leaves marker lines the
	// block scan cannot see (#2258) — and those must never reach the file.
	if n := mg.MarkerLines(); n > 0 {
		m.host.Notify(host.Warn, strconv.Itoa(n)+" conflict marker lines remain in the result — remove them before applying")
		return m, nil
	}
	// The save/finish offer (#2258) may still be up — it is what usually
	// dispatches this — and its view is about to close under it.
	if m.mergeFinishPending == inst.Key() {
		m.mergeFinishPending = ""
		m.shell.Close()
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

// syncMergeFinish watches every merge pane's remaining-conflict count on the
// settled Update pass (#2258) and offers save/finish the moment a view's last
// conflict is resolved — whichever way it was resolved (an accept chord, a
// palette command, plain typing over the markers), which is why this rides the
// settled pass instead of one key route. The per-pane count is remembered, so
// an undo that brings a conflict back re-arms the offer, and a view that opens
// already conflict-free (nothing to resolve) never raises it.
func (m *Model) syncMergeFinish() {
	live := map[string]bool{}
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindMerge {
			continue
		}
		live[key] = true
		n := inst.Merge().Unresolved()
		was, seen := m.mergeUnresolved[key]
		if n == 0 && seen && was > 0 {
			// The blocks are gone — but a half-edited one leaves markers the
			// block scan cannot see, and that is not a finished merge. The
			// buffer walk that finds them only runs on this transition, and
			// leaving the remembered count where it was keeps the transition
			// armed for when the last marker finally goes.
			if inst.Merge().MarkerLines() > 0 {
				continue
			}
			m.openMergeFinishPrompt(key)
		}
		m.mergeUnresolved[key] = n
	}
	for key := range m.mergeUnresolved {
		if !live[key] {
			delete(m.mergeUnresolved, key)
		}
	}
	// A view closed under its own offer (the discard guard, a workspace
	// switch) leaves nothing to save: drop the dialog with it.
	if m.mergeFinishPending != "" && !live[m.mergeFinishPending] {
		m.mergeFinishPending = ""
		m.shell.Close()
	}
}

// openMergeFinishPrompt raises the save/finish offer for a merge view whose
// last conflict just went away. Another dialog already owning the shell wins —
// the offer is a convenience, and the counter plus the status-line hint keep
// pointing at vcs.mergeApply.
func (m *Model) openMergeFinishPrompt(key string) {
	if m.shell.IsOpen() || m.mergeFinishPending != "" {
		return
	}
	inst := m.activeWS().Panes.Get(key)
	if inst == nil {
		return
	}
	m.mergeFinishPending = key
	body := "All conflicts in " + baseName(inst.Merge().Path()) + " are resolved.\n\n" +
		guardLine("s", "save the result, stage it and close the merge view", true) +
		guardCancel("keep editing the result")
	m.shell.SetContent(ui.ModelContent{
		Heading: "Merge complete",
		Body:    func() string { return body },
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// mergeFinishPromptOpen reports whether the finish offer owns the keyboard.
func (m Model) mergeFinishPromptOpen() bool { return m.mergeFinishPending != "" && m.shell.IsOpen() }

// updateMergeFinishPrompt consumes every key while the offer is open: s — or
// enter, the primary option — applies the merge, anything else dismisses it.
// The view stays open on dismissal, so a late correction is still possible.
func (m Model) updateMergeFinishPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := m.mergeFinishPending
	m.mergeFinishPending = ""
	m.shell.Close()
	if guardAnswer(msg, "s") != "s" {
		return m, nil
	}
	m.setFocus(key)
	return m.mergeApply()
}

// offerMergeTool raises the merge-tool offer for a freshly opened file that
// git reports as conflicted (#2258) — the discoverability half of the merge
// view, since an editor full of `<<<<<<<` markers says nothing about the
// three-way tool that can resolve them. Once per path per session, and never
// over another dialog: the file is open and editable either way.
func (m *Model) offerMergeTool(path string) {
	if m.vcs.snap == nil || m.mergeOffered[path] || m.shell.IsOpen() {
		return
	}
	if m.vcs.snap.Status(path) != vcs.StatusConflicted {
		return
	}
	m.mergeOffered[path] = true
	m.mergeOfferPending = path
	body := baseName(path) + " has unresolved merge conflicts.\n\n" +
		guardLine("m", "open the three-way merge tool (ours / result / theirs)", true) +
		guardCancel("edit the conflict markers here")
	m.shell.SetContent(ui.ModelContent{
		Heading: "Conflicted file",
		Body:    func() string { return body },
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// mergeOfferPromptOpen reports whether the merge-tool offer owns the keyboard.
func (m Model) mergeOfferPromptOpen() bool { return m.mergeOfferPending != "" && m.shell.IsOpen() }

// updateMergeOfferPrompt consumes every key while the offer is open: m — or
// enter, the primary option — opens the merge view, anything else leaves the
// user in the editor with the markers.
func (m Model) updateMergeOfferPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	path := m.mergeOfferPending
	m.mergeOfferPending = ""
	m.shell.Close()
	if guardAnswer(msg, "m") != "m" {
		return m, nil
	}
	return m.mergePath(path)
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
