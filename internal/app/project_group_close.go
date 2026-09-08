package app

import (
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/project"
	"ike/internal/telemetry"
	"ike/internal/ui"
	"ike/internal/workspace"
)

// project_group_close.go closes the active project group in one action (Epic
// 0510, #2572): every member workspace — the active one and the parked ones —
// tears down through the #820/#825 path, behind one busy guard that
// aggregates the project.close and close-from-list probes across all
// members. The IDE lands on the most recently used parked *non-member*
// workspace; with none the close degrades to the quit guard, like
// project.close on the last project. History entries stay; the active-group
// marker clears in the model and on disk.
//
// Order: the switch away from the active member runs first (it persists the
// member's session and layout and parks its workspace, exactly like
// project.close), then every parked member is dropped and torn down. A
// failed switch (chdir error) closes nothing.

// memberActivity is one busy member's inventory for the prompt body.
type memberActivity struct {
	root string
	act  wsActivity
}

// pendingGroupClose is the aggregated busy guard state: the group, the MRU
// non-member root to land on ("" when the active workspace is not a member —
// nothing to switch away from), and the busy members the close would hit.
type pendingGroupClose struct {
	name   string
	target string
	busy   []memberActivity
}

// anyDirty reports whether any busy member has unsaved buffers — the save
// option is offered only then.
func (p *pendingGroupClose) anyDirty() bool {
	for _, b := range p.busy {
		if len(b.act.dirty) > 0 {
			return true
		}
	}
	return false
}

// handleCloseGroup routes project.group.close.
func (m Model) handleCloseGroup() (tea.Model, tea.Cmd) {
	g, ok := m.activeGroupForAction()
	if !ok {
		return m, nil
	}
	target := ""
	if isGroupMember(g, m.currentRoot()) {
		target = m.mruNonMember(g)
		if target == "" {
			// No non-member workspace to land on: the close is a quit, guarded
			// like any quit — the marker clears when the quit goes through.
			return m.closeGroupByQuit(g.Name)
		}
	}
	busy := m.collectGroupActivity(g)
	if len(busy) > 0 {
		m.openGroupClosePrompt(&pendingGroupClose{name: g.Name, target: target, busy: busy})
		return m, nil
	}
	return m.performCloseGroup(g.Name, target)
}

// activeGroupForAction resolves the marker to its group for close / cycle /
// warm, notifying when there is none (or a chain is still running).
func (m Model) activeGroupForAction() (project.Group, bool) {
	if m.groupOpening != nil {
		m.host.Notify(host.Info, "group "+m.groupOpening.name+" is still "+m.groupOpening.verb()+"ing")
		return project.Group{}, false
	}
	if m.activeGroup == "" {
		m.host.Notify(host.Info, "no project group open")
		return project.Group{}, false
	}
	g, ok := project.FindGroup(config.Get(), m.activeGroup)
	if !ok {
		m.host.Notify(host.Warn, "group \""+m.activeGroup+"\" no longer exists")
		return project.Group{}, false
	}
	return g, true
}

// isGroupMember reports whether root is one of g's members, symlinks resolved
// (the manager keys by os.Getwd's spelling, the group stores roots as typed).
func isGroupMember(g project.Group, root string) bool {
	for _, r := range g.Roots {
		if sameGroupRoot(r, root) {
			return true
		}
	}
	return false
}

// mruNonMember returns the most recently used parked root outside the group,
// or "" when every parked workspace is a member.
func (m Model) mruNonMember(g project.Group) string {
	bg := m.ws.Background()
	for i := len(bg) - 1; i >= 0; i-- {
		if !isGroupMember(g, bg[i]) {
			return bg[i]
		}
	}
	return ""
}

// collectGroupActivity aggregates the busy probes over the in-memory members:
// the active workspace with its popup terminal and project-owned floating
// panels (the project.close probe) and every parked member (the
// close-from-list probe, whose popup shells sit in Aux). Idle members are
// left out; the order is active first, then the parked ones LRU-first.
func (m Model) collectGroupActivity(g project.Group) []memberActivity {
	var busy []memberActivity
	if w := m.activeWS(); w != nil && isGroupMember(g, w.Root) {
		act := collectActivity(w)
		if !m.popupScopeGlobal() {
			for _, inst := range m.popup.instances() {
				act.addPopup(inst)
			}
		}
		for _, f := range projectFloatTerms(m.floatTerms) {
			act.addPopup(f.inst)
		}
		if act.busy() {
			busy = append(busy, memberActivity{root: w.Root, act: act})
		}
	}
	for _, root := range m.ws.Background() {
		if !isGroupMember(g, root) {
			continue
		}
		if act := collectActivity(m.ws.Peek(root)); act.busy() {
			busy = append(busy, memberActivity{root: root, act: act})
		}
	}
	return busy
}

// openGroupClosePrompt shows the aggregated busy guard in the #821 shape: the
// body lists the busy members, one line each, then save-all (only with
// something dirty) / discard / cancel.
func (m *Model) openGroupClosePrompt(p *pendingGroupClose) {
	m.groupClosePending = p
	body := "group " + p.name + " still has:\n"
	for _, b := range p.busy {
		body += "  " + filepath.Base(b.root) + " — " + strings.Join(b.act.summary(), ", ") + "\n"
	}
	body += "\n"
	dirty := p.anyDirty()
	if dirty {
		body += guardLine("s", "save all, then close the group", true)
	}
	body += guardLine("d", "close the group — stop processes, discard unsaved changes", !dirty) +
		guardCancel("cancel — keep the group open")
	m.shell.SetContent(ui.ModelContent{
		Heading: "Close project group?",
		Body:    func() string { return body },
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// groupClosePromptOpen reports whether the guard currently owns the keyboard.
func (m Model) groupClosePromptOpen() bool {
	return m.groupClosePending != nil && m.shell.IsOpen()
}

// updateGroupClosePrompt consumes every key while the guard is open: s saves
// every busy member's dirty buffers then closes the group — a failed write
// keeps that member open and cancels the rest — d closes discarding, esc
// cancels with every member untouched. Enter takes the primary option
// (#1356): save when there is anything to save, otherwise the plain close.
func (m Model) updateGroupClosePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	pending := m.groupClosePending
	primary := "s"
	if !pending.anyDirty() {
		primary = "d"
	}
	switch guardAnswer(msg, primary) {
	case "s":
		if !pending.anyDirty() {
			return m, nil
		}
		m.groupClosePending = nil
		m.shell.Close()
		// Editor writes apply synchronously inside UpdateTab (the returned
		// cmds only carry follow-up events), so the close can proceed in the
		// same step when everything saved.
		var cmds []tea.Cmd
		for _, b := range pending.busy {
			if len(b.act.dirty) == 0 {
				continue
			}
			if w := m.activeWS(); w != nil && w.Root == b.root {
				cmds = append(cmds, m.saveAllDirty()...)
			} else {
				cmds = append(cmds, saveWorkspaceDirty(m.ws.Peek(b.root))...)
			}
		}
		if failed := m.groupSaveFailed(pending.busy); failed != "" {
			m.host.Notify(host.Error, "group not closed: save failed in "+filepath.Base(failed))
			return m, tea.Batch(cmds...)
		}
		next, cmd := m.performCloseGroup(pending.name, pending.target)
		return next, tea.Batch(append(cmds, cmd)...)
	case "d":
		m.groupClosePending = nil
		m.shell.Close()
		return m.performCloseGroup(pending.name, pending.target)
	case "esc":
		m.groupClosePending = nil
		m.shell.Close()
		return m, nil
	}
	return m, nil
}

// groupSaveFailed re-probes the members that had dirty buffers and returns
// the first root still dirty after the save, or "".
func (m Model) groupSaveFailed(busy []memberActivity) string {
	for _, b := range busy {
		if len(b.act.dirty) == 0 {
			continue
		}
		var w *workspace.Workspace
		if a := m.activeWS(); a != nil && a.Root == b.root {
			w = a
		} else {
			w = m.ws.Peek(b.root)
		}
		if len(collectActivity(w).dirty) > 0 {
			return b.root
		}
	}
	return ""
}

// performCloseGroup runs the close: with the active workspace a member, the
// seamless switch to target first (session and layout persisted, the
// workspace parked — project.close's transaction, with the leave reason
// "close"), then every parked member is dropped and torn down (#820/#825).
// The marker clears in the model at once and on disk off the loop. A failed
// switch closes nothing; its SwitchFailedMsg toast names the reason.
func (m Model) performCloseGroup(name, target string) (tea.Model, tea.Cmd) {
	g, _ := project.FindGroup(config.Get(), name)
	endOp := m.usage.OpTimer(telemetry.OpProjectGroupClose)
	var cmds []tea.Cmd
	sized := m
	if target != "" {
		oldRoot := m.activeWS().Root
		next, cmd := m.performSwitchOpts(target, switchOpts{record: true, closing: true})
		s, ok := next.(Model)
		if !ok {
			endOp("error", map[string]string{"members": "0"})
			return next, cmd
		}
		cmds = append(cmds, cmd)
		if s.activeWS() != nil && s.activeWS().Root == oldRoot {
			endOp("error", map[string]string{"members": "0"})
			return s, cmd // switch failed; nothing parked, nothing closed
		}
		sized = s
	}
	closed := 0
	for _, root := range sized.ws.Background() {
		if !isGroupMember(g, root) {
			continue
		}
		if w := sized.ws.Drop(root); w != nil {
			cmds = append(cmds, sized.closeWorkspace(w))
			closed++
		}
	}
	sized.activeGroup = ""
	// members counts the member workspaces the close actually tore down — the
	// one switched away from included, as the switch parks it into the
	// background set the loop above drains (#2578).
	endOp("ok", map[string]string{"members": strconv.Itoa(closed)})
	sized.host.Notify(host.Info, "closed group "+name+" · "+pluralProjects(closed))
	cmds = append(cmds, project.ClearActiveGroupCmd(sized.cfgOpts))
	return sized, tea.Batch(cmds...)
}

// closeGroupByQuit is the degraded close: every parked workspace is a member
// (or there is none), so closing the group empties the IDE. The quit guard
// (#287/#821) aggregates the same probes over every workspace; the marker
// clears synchronously when the quit goes through — a cmd would never run.
func (m Model) closeGroupByQuit(name string) (tea.Model, tea.Cmd) {
	dirty, running := m.quitActivity()
	if len(dirty) > 0 || len(running) > 0 {
		m.openQuitPrompt(dirty, running)
		m.closePending.groupClose = name
		return m, nil
	}
	m.clearGroupMarkerNow()
	return m.quit()
}

// clearGroupMarkerNow clears the marker in the model and on disk synchronously
// (the quit paths, where no cmd would run).
func (m *Model) clearGroupMarkerNow() {
	m.activeGroup = ""
	if err := project.ClearActiveGroup(m.cfgOpts); err != nil {
		m.host.Notify(host.Warn, "could not clear project.active_group: "+err.Error())
	}
}
