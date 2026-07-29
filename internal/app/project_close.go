package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/project"
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
	if act := collectActivity(m.activeWS()); act.busy() {
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
	oldRoot := ""
	if w := m.ws.Active(); w != nil {
		oldRoot = w.Root
	}
	next, cmd := m.performSwitch(target)
	sized, ok := next.(Model)
	if !ok {
		return next, cmd
	}
	w := sized.ws.Drop(oldRoot)
	if w == nil {
		return sized, cmd // switch failed; the close never happened
	}
	closeCmd := sized.closeWorkspace(w)
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
		body += "  [s]   save all, then close it\n"
	}
	body += "  [d]   close it — stop processes, discard unsaved changes\n" +
		"  [esc] cancel — keep the project open"
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
// fails), d closes discarding, esc cancels with the project untouched.
func (m Model) updateProjectClosePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	pending := m.projectClosePending
	switch msg.String() {
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
