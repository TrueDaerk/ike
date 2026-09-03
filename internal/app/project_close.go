package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/project"
	"ike/internal/telemetry"
	"ike/internal/ui"
)

// project_close.go closes the current project (#1355): the active workspace
// tears down like a close-from-list (#820) and the most recently used
// background workspace resumes in place via the seamless-switch path (#777).
// With no background workspace the request degrades to a guarded app quit
// (#287/#821). A busy active workspace — dirty buffers, running processes —
// asks first with the same save/discard/cancel shape as the #821 guard.

// pendingProjectClose is the busy close-current guard state: the MRU
// background root to resume plus the activity the close would kill.
type pendingProjectClose struct {
	target string
	act    wsActivity
}

// handleCloseProject routes project.close: quit when this is the only open
// project, otherwise close the active workspace and resume the MRU parked one,
// gated on the busy guard.
func (m Model) handleCloseProject() (tea.Model, tea.Cmd) {
	bg := m.ws.Background()
	if len(bg) == 0 {
		return m.guardedQuit()
	}
	target := bg[len(bg)-1] // most-recently-used parked root
	act := collectActivity(m.activeWS())
	// The active popup terminal and project-owned floating panels live on the
	// model (#1407, #1793) and die with the close; global panels ride to the
	// resumed project and don't count.
	// A global-scope popup (#2406) is app state: the close carries it to the
	// resumed project like a global panel, so its shells are not activity this
	// close would end and must not raise the guard.
	if !m.popupScopeGlobal() {
		for _, inst := range m.popup.instances() {
			act.addPopup(inst)
		}
	}
	for _, f := range projectFloatTerms(m.floatTerms) {
		act.addPopup(f.inst)
	}
	if act.busy() {
		m.openProjectClosePrompt(target, act)
		return m, nil
	}
	return m.performCloseAndSwitch(target)
}

// performCloseAndSwitch runs the seamless switch to target — which persists
// the closing project's session and layout and parks its workspace — then
// drops and tears the parked workspace down. On a failed switch (chdir error)
// nothing was parked and the current project stays untouched.
func (m Model) performCloseAndSwitch(target string) (tea.Model, tea.Cmd) {
	// The close is a timed operation of its own (#2403): it wraps a whole
	// project.switch plus the departing workspace's teardown, so the two op
	// spans nest and the difference is what tearing the old project down cost.
	endOp := m.usage.OpTimer(telemetry.OpProjectClose)
	oldRoot := ""
	if w := m.ws.Active(); w != nil {
		oldRoot = w.Root
	}
	// No peek escalation (#2136): closing a peeked active project discards
	// it, so its root must not be recorded into project.history on the way.
	next, cmd := m.performSwitchOpts(target, switchOpts{record: true, closing: true})
	sized, ok := next.(Model)
	if !ok {
		endOp("error", nil)
		return next, cmd
	}
	w := sized.ws.Drop(oldRoot)
	if w == nil {
		endOp("error", nil)
		return sized, cmd // switch failed; the close never happened
	}
	closeCmd := sized.closeWorkspace(w)
	endOp("ok", nil)
	sized.host.Notify(host.Info, "closed project "+project.CompactPath(oldRoot))
	return sized, tea.Batch(cmd, closeCmd)
}

// openProjectClosePrompt shows the busy close-current guard: the active
// project still has live state, closing it resumes target.
func (m *Model) openProjectClosePrompt(target string, act wsActivity) {
	m.projectClosePending = &pendingProjectClose{target: target, act: act}
	body := "the current project still has:\n  " +
		strings.Join(act.summary(), "\n  ") + "\n\n"
	if len(act.dirty) > 0 {
		body += guardLine("s", "save all, then close it", true)
	}
	body += guardLine("d", "close it — stop processes, discard unsaved changes", len(act.dirty) == 0) +
		guardCancel("cancel — keep the project open")
	m.shell.SetContent(ui.ModelContent{
		Heading: "Close project?",
		Body:    func() string { return body },
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// projectClosePromptOpen reports whether the guard currently owns the keyboard.
func (m Model) projectClosePromptOpen() bool {
	return m.projectClosePending != nil && m.shell.IsOpen()
}

// updateProjectClosePrompt consumes every key while the guard is open: s saves
// the active workspace's dirty buffers then closes (staying open when a write
// fails), d closes discarding, esc cancels with the project untouched. Enter
// takes the primary option — saving when there is anything to save, otherwise
// the plain close (#1356).
func (m Model) updateProjectClosePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	pending := m.projectClosePending
	primary := "s"
	if len(pending.act.dirty) == 0 {
		primary = "d"
	}
	switch guardAnswer(msg, primary) {
	case "s":
		if len(pending.act.dirty) == 0 {
			return m, nil
		}
		m.projectClosePending = nil
		m.shell.Close()
		// Editor writes apply synchronously inside UpdateTab (the returned
		// cmds only carry follow-up events), so the close can proceed in the
		// same step when everything saved.
		cmds := m.saveAllDirty()
		if len(collectActivity(m.activeWS()).dirty) > 0 {
			m.host.Notify(host.Error, "not closed: save failed")
			return m, tea.Batch(cmds...)
		}
		next, cmd := m.performCloseAndSwitch(pending.target)
		return next, tea.Batch(append(cmds, cmd)...)
	case "d":
		m.projectClosePending = nil
		m.shell.Close()
		return m.performCloseAndSwitch(pending.target)
	case "esc":
		m.projectClosePending = nil
		m.shell.Close()
		return m, nil
	}
	return m, nil
}
