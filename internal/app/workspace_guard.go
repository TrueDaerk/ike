package app

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/project"
	"ike/internal/ui"
	"ike/internal/workspace"
)

// workspace_guard.go confirms before a workspace with live state is torn
// down (#821): closing a background workspace from the recent-projects list
// (#820) prompts with a summary of what is running plus save/discard/cancel
// for dirty buffers, and quitting the IDE aggregates the same checks across
// every in-memory workspace. LRU eviction already has its own guard (#780).

// wsActivity summarises the live state closing a workspace would kill.
type wsActivity struct {
	running []string // running debug session, runs, tools — one line each
	shells  int      // running plain shell terminals
	dirty   []string // dirty buffer names
}

// busy reports whether tearing the workspace down loses anything at all
// (the close-from-list gate; shells count — their state dies too).
func (a wsActivity) busy() bool {
	return len(a.running) > 0 || a.shells > 0 || len(a.dirty) > 0
}

// summary renders the activity as prompt body lines.
func (a wsActivity) summary() []string {
	lines := append([]string(nil), a.running...)
	if a.shells > 0 {
		lines = append(lines, plural(a.shells, "running shell terminal", "running shell terminals"))
	}
	if len(a.dirty) > 0 {
		lines = append(lines, "unsaved: "+strings.Join(a.dirty, ", "))
	}
	return lines
}

// collectActivity inventories one workspace's live state. It mirrors
// workspaceBusy (#780) but keeps the details for the prompt body.
func collectActivity(w *workspace.Workspace) wsActivity {
	var a wsActivity
	if w == nil {
		return a
	}
	addTerm := func(tool, label string, isCmd bool) {
		switch {
		case tool != "":
			a.running = append(a.running, "tool "+tool)
		case isCmd:
			if label == "" {
				label = "command"
			}
			a.running = append(a.running, "run "+label)
		default:
			a.shells++
		}
	}
	for _, key := range w.Panes.Keys() {
		inst := w.Panes.Get(key)
		if inst == nil {
			continue
		}
		switch inst.Kind() {
		case pane.KindTerminal:
			if t := inst.Terminal(); t.Running() {
				addTerm(t.Tool(), t.Label(), t.IsCommand())
			}
		case pane.KindEditor:
			for i := 0; i < inst.TabCount(); i++ {
				if ed := inst.TabEditor(i); ed != nil && ed.Dirty() {
					name := "untitled"
					if p := ed.Path(); p != "" {
						name = filepath.Base(p)
					}
					a.dirty = append(a.dirty, name)
				}
				if t := inst.TabTerminal(i); t != nil && t.Running() {
					addTerm(t.Tool(), t.Label(), t.IsCommand())
				}
			}
		}
	}
	if extras, ok := w.Aux.(wsExtras); ok && extras.dbg != nil && extras.dbg.sess != nil {
		a.running = append(a.running, "debug session")
	}
	return a
}

// pendingWsClose is the close-from-list guard state (#821).
type pendingWsClose struct {
	root string
	act  wsActivity
}

// openWsClosePrompt shows the busy close-from-list guard (#821) for root.
func (m *Model) openWsClosePrompt(root string, act wsActivity) {
	m.wsClosePending = &pendingWsClose{root: root, act: act}
	body := project.CompactPath(root) + " still has:\n  " +
		strings.Join(act.summary(), "\n  ") + "\n\n"
	if len(act.dirty) > 0 {
		body += "  [s]   save all, then close it\n"
	}
	body += "  [d]   close it — stop processes, discard unsaved changes\n" +
		"  [esc] cancel — keep it running"
	m.shell.SetContent(ui.ModelContent{
		Heading: "Close background workspace?",
		Body:    func() string { return body },
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// finishWorkspaceClose drops and tears the background workspace down and
// refreshes the palette (the ● badge disappears in place). The returned cmd
// carries the workspace-closed hooks' async work (#825).
func (m *Model) finishWorkspaceClose(root string) tea.Cmd {
	cmd := m.closeWorkspace(m.ws.Drop(root))
	m.palette.Refresh()
	m.host.Notify(host.Info, "closed background workspace "+project.CompactPath(root))
	return cmd
}

// wsClosePromptOpen reports whether the guard currently owns the keyboard.
func (m Model) wsClosePromptOpen() bool { return m.wsClosePending != nil && m.shell.IsOpen() }

// updateWsClosePrompt consumes every key while the guard is open: s saves the
// workspace's dirty buffers then closes it (staying open when a write
// fails), d closes discarding, esc cancels with the workspace untouched.
func (m Model) updateWsClosePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	pending := m.wsClosePending
	switch msg.String() {
	case "s":
		if len(pending.act.dirty) == 0 {
			return m, nil
		}
		m.wsClosePending = nil
		m.shell.Close()
		w := m.ws.Peek(pending.root)
		cmds := saveWorkspaceDirty(w)
		if len(collectActivity(w).dirty) > 0 {
			m.host.Notify(host.Error, "not closed: save failed")
			return m, tea.Batch(cmds...)
		}
		cmds = append(cmds, m.finishWorkspaceClose(pending.root))
		return m, tea.Batch(cmds...)
	case "d":
		m.wsClosePending = nil
		m.shell.Close()
		return m, m.finishWorkspaceClose(pending.root)
	case "esc":
		m.wsClosePending = nil
		m.shell.Close()
		return m, nil
	}
	return m, nil
}

// saveWorkspaceDirty writes every dirty buffer of w (background workspaces
// included — the editor write path does not depend on focus or rendering).
func saveWorkspaceDirty(w *workspace.Workspace) []tea.Cmd {
	if w == nil {
		return nil
	}
	var cmds []tea.Cmd
	for _, key := range w.Panes.Keys() {
		inst := w.Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			if ed := inst.TabEditor(i); ed != nil && ed.Dirty() && ed.HasFile() {
				// Raw write (#1148): the workspace teardown proceeds
				// synchronously, so the save must not defer behind the chain.
				if cmd := inst.UpdateTab(i, editor.ActionMsg{Action: "write_raw"}); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
	}
	return cmds
}

// quitActivity aggregates the quit-relevant state across every in-memory
// workspace (#821): the active one plus all parked ones. Idle shells are
// excluded (see wsActivity.noisy) — every session has a shell open.
func (m Model) quitActivity() (dirty, running []string) {
	label := func(root string, s string) string {
		if root == "" {
			return s
		}
		return s + " (" + project.CompactPath(root) + ")"
	}
	collect := func(w *workspace.Workspace) {
		if w == nil {
			return
		}
		act := collectActivity(w)
		for _, d := range act.dirty {
			dirty = append(dirty, label(bgRoot(m, w), d))
		}
		for _, r := range act.running {
			running = append(running, label(bgRoot(m, w), r))
		}
	}
	collect(m.ws.Active())
	for _, root := range m.ws.Background() {
		collect(m.ws.Peek(root))
	}
	return dirty, running
}

// bgRoot labels a workspace's entries with its root — but only for parked
// workspaces; the active one needs no disambiguation.
func bgRoot(m Model, w *workspace.Workspace) string {
	if m.ws.Active() == w {
		return ""
	}
	return w.Root
}
