package app

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/project"
	"ike/internal/ui"
)

// project_peek.go is the quick-peek project switch (#2136): project.peek opens
// another project through the normal seamless-switch transaction (#777) but
// marks the workspace as a peek — the open is not recorded into
// project.history, so the look-up never becomes the startup restore head — and
// project.peek.return goes back to the origin in one action and unloads the
// peeked workspace again (memory, terminals, LSP — the #820 teardown path).
// project.peek.keep converts the peek into a normal workspace, as does any
// normal switch away from it (escalation, handled in performSwitchOpts).

// peekState marks the active workspace as a peek (#2136). It lives on the
// model, not the workspace: it only ever describes the *active* workspace and
// deliberately dies with the process — a peek does not survive a restart.
type peekState struct {
	// origin is the root project.peek.return switches back to.
	origin string
	// enterSession/enterLayout snapshot the peeked project's persisted state
	// right after peek-enter; the return skips the session/layout write when
	// nothing changed, so a quick look-up plants no .ike residue.
	enterSession []byte
	enterLayout  []byte
}

// peekStateSnapshot encodes the active workspace's would-be session and layout
// payloads for the unchanged check.
func (m Model) peekStateSnapshot() (session, layout []byte) {
	session, _ = json.Marshal(m.snapshotSession())
	layout, _ = encodeLayoutState(m.activeWS().Tree, m.activeWS().Panes)
	return session, layout
}

// peekStateChanged reports whether the peeked project's persisted state
// drifted since peek-enter. No peek reads as changed, so the caller's save
// stays on the safe side.
func (m Model) peekStateChanged() bool {
	if m.peek == nil {
		return true
	}
	session, layout := m.peekStateSnapshot()
	return !bytes.Equal(session, m.peek.enterSession) || !bytes.Equal(layout, m.peek.enterLayout)
}

// handlePeekProject routes a validated peek request (#2136): a root equal to
// the current one is a friendly no-op; peeking from within a peek escalates
// the current one first (its root is recorded, exactly like a normal switch
// away would), then the switch runs with the peek marker set.
func (m Model) handlePeekProject(msg project.PeekProjectMsg) (tea.Model, tea.Cmd) {
	if cur := m.activeWS().Root; cur == msg.Root {
		m.host.Notify(host.Info, "already in "+msg.Root)
		return m, nil
	}
	var escalate tea.Cmd
	if m.peek != nil {
		cur := m.activeWS().Root
		escalate = project.RecordOpenCmd(config.Discover("."), cur, time.Now())
		m.peek = nil
	}
	next, cmd := m.performSwitchOpts(msg.Root, switchOpts{peekEnter: true})
	return next, tea.Batch(escalate, cmd)
}

// handlePeekReturn routes project.peek.return: switch back to the origin and
// drop the peeked workspace, behind the busy guard when the drop would kill
// live state (dirty buffers, running processes) — same shape as project.close.
func (m Model) handlePeekReturn() (tea.Model, tea.Cmd) {
	if m.peek == nil {
		m.host.Notify(host.Info, "not peeking — nothing to return from")
		return m, nil
	}
	act := collectActivity(m.activeWS())
	// The active popup terminal and project-owned floating panels live on the
	// model (#1407, #1793) and die with the drop; global panels ride back to
	// the origin and don't count.
	for _, inst := range m.popup.instances() {
		act.addPopup(inst)
	}
	for _, f := range projectFloatTerms(m.floatTerms) {
		act.addPopup(f.inst)
	}
	if act.busy() {
		m.openPeekReturnPrompt(act)
		return m, nil
	}
	return m.performPeekReturn()
}

// performPeekReturn runs the seamless switch back to the origin — skipping the
// peeked project's session/layout write when nothing changed there, recording
// the origin open like any switch-back — then drops and tears the parked
// peeked workspace down. A failed switch (chdir error: the origin root is
// gone) keeps the peek intact; the SwitchFailedMsg toast names the reason. An
// origin evicted while peeking resumes as a cold first visit — performSwitch
// handles both alike.
func (m Model) performPeekReturn() (tea.Model, tea.Cmd) {
	origin := m.peek.origin
	peekedRoot := m.activeWS().Root
	next, cmd := m.performSwitchOpts(origin, switchOpts{record: true, skipUnchangedPeekSave: true})
	sized, ok := next.(Model)
	if !ok {
		return next, cmd
	}
	w := sized.ws.Drop(peekedRoot)
	if w == nil {
		return sized, cmd // switch failed; still peeking, workspace untouched
	}
	closeCmd := sized.closeWorkspace(w)
	sized.host.Notify(host.Info, "peek ended — unloaded "+project.CompactPath(peekedRoot))
	return sized, tea.Batch(cmd, closeCmd)
}

// handlePeekKeep routes project.peek.keep (#2136 escalation): the peek becomes
// a normal workspace — the open is recorded into project.history and the
// marker (with the return affordance) clears.
func (m Model) handlePeekKeep() (tea.Model, tea.Cmd) {
	if m.peek == nil {
		m.host.Notify(host.Info, "not peeking — nothing to keep")
		return m, nil
	}
	root := m.activeWS().Root
	m.peek = nil
	m.host.Notify(host.Info, "keeping "+project.CompactPath(root)+" — recorded in recent projects")
	return m, project.RecordOpenCmd(config.Discover("."), root, time.Now())
}

// pendingPeekReturn is the busy peek-return guard state (#2136): the activity
// the drop would kill. The origin re-resolves from m.peek at confirm time.
type pendingPeekReturn struct {
	act wsActivity
}

// openPeekReturnPrompt shows the busy peek-return guard: the peeked project
// still has live state the return would discard.
func (m *Model) openPeekReturnPrompt(act wsActivity) {
	m.peekReturnPending = &pendingPeekReturn{act: act}
	body := "the peeked project still has:\n  " +
		strings.Join(act.summary(), "\n  ") + "\n\n"
	if len(act.dirty) > 0 {
		body += guardLine("s", "save all, then return", true)
	}
	body += guardLine("d", "return — stop processes, discard unsaved changes", len(act.dirty) == 0) +
		guardCancel("cancel — stay in the peeked project")
	m.shell.SetContent(ui.ModelContent{
		Heading: "Return from peek?",
		Body:    func() string { return body },
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// peekReturnPromptOpen reports whether the guard currently owns the keyboard.
func (m Model) peekReturnPromptOpen() bool {
	return m.peekReturnPending != nil && m.shell.IsOpen()
}

// updatePeekReturnPrompt consumes every key while the guard is open: s saves
// the peeked workspace's dirty buffers then returns (staying open when a write
// fails), d returns discarding, esc cancels with the peek untouched. Enter
// takes the primary option (#1356).
func (m Model) updatePeekReturnPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	pending := m.peekReturnPending
	primary := "s"
	if len(pending.act.dirty) == 0 {
		primary = "d"
	}
	switch guardAnswer(msg, primary) {
	case "s":
		if len(pending.act.dirty) == 0 {
			return m, nil
		}
		m.peekReturnPending = nil
		m.shell.Close()
		// Editor writes apply synchronously inside UpdateTab (the returned
		// cmds only carry follow-up events), so the return can proceed in the
		// same step when everything saved.
		cmds := m.saveAllDirty()
		if len(collectActivity(m.activeWS()).dirty) > 0 {
			m.host.Notify(host.Error, "not returned: save failed")
			return m, tea.Batch(cmds...)
		}
		next, cmd := m.performPeekReturn()
		return next, tea.Batch(append(cmds, cmd)...)
	case "d":
		m.peekReturnPending = nil
		m.shell.Close()
		return m.performPeekReturn()
	case "esc":
		m.peekReturnPending = nil
		m.shell.Close()
		return m, nil
	}
	return m, nil
}

// peekSegment is the statusline peek indicator (#2136): visible whenever the
// active project is a peek, naming the origin and the live return chord so the
// one-key way back stays discoverable.
func peekSegment(m Model, _ *editor.Model) string {
	if m.peek == nil {
		return ""
	}
	return "peek ⇢ " + filepath.Base(m.peek.origin) + " (" + peekReturnChord(m) + ")"
}

// peekReturnChord renders the live chord bound to project.peek.return —
// resolver truth, so a remap shows the real key — falling back to the command
// id when the binding is gone or blocked.
func peekReturnChord(m Model) string {
	if m.bindings != nil {
		if s, ok := m.bindings.Binding("project.peek.return"); ok && s != "" && !strings.Contains(s, "✗") {
			return s
		}
	}
	return "project.peek.return"
}
