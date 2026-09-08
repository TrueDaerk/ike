package app

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/project"
)

// project_group_cycle.go moves within the active project group (Epic 0510,
// #2572): project.group.next / project.group.prev step through the members in
// list order with wrap, and project.group.warm re-parks the members that
// dropped out of the background set.

// handleCycleGroup routes project.group.next (delta +1) and project.group.prev
// (delta -1): the target is the neighbour of the current root among the
// members present on disk (missing ones are skipped), wrapping at either end.
// From a non-member root next goes to member 1 and prev to the last member.
// The step is a normal switch — a parked member resumes, an unparked one is a
// cold first visit, history is recorded and the auto-save gate runs.
func (m Model) handleCycleGroup(delta int) (tea.Model, tea.Cmd) {
	g, ok := m.activeGroupForAction()
	if !ok {
		return m, nil
	}
	present, _ := project.ResolveGroupRoots(g)
	if len(present) == 0 {
		m.host.Notify(host.Error, "group \""+g.Name+"\": no member exists on disk")
		return m, nil
	}
	cur := m.currentRoot()
	idx := -1
	for i, r := range present {
		if sameGroupRoot(r, cur) {
			idx = i
			break
		}
	}
	n := len(present)
	var target string
	switch {
	case idx < 0 && delta > 0:
		target = present[0]
	case idx < 0:
		target = present[n-1]
	default:
		target = present[((idx+delta)%n+n)%n]
	}
	if sameGroupRoot(target, cur) {
		m.host.Notify(host.Info, "group "+g.Name+" has no other project")
		return m, nil
	}
	return m.handleSwitchProject(project.SwitchProjectMsg{Root: target})
}

// handleWarmGroup routes project.group.warm: the members present on disk but
// neither active nor parked are visited through the open chain (N…1 among
// them), and a final hop returns to the current root. With every member
// already in memory nothing runs.
func (m Model) handleWarmGroup() (tea.Model, tea.Cmd) {
	g, ok := m.activeGroupForAction()
	if !ok {
		return m, nil
	}
	present, missing := project.ResolveGroupRoots(g)
	if len(missing) > 0 {
		m.host.Notify(host.Warn, "group \""+g.Name+"\": "+strconv.Itoa(len(missing))+" of "+
			strconv.Itoa(len(g.Roots))+" roots missing")
	}
	cur := m.currentRoot()
	var cold []string
	for _, r := range present {
		if sameGroupRoot(r, cur) || m.parkedGroupRoot(r) {
			continue
		}
		cold = append(cold, r)
	}
	if len(cold) == 0 {
		m.host.Notify(host.Info, "group "+g.Name+" is warm · every project is parked")
		return m, nil
	}
	queue := make([]string, 0, len(cold)+1)
	for i := len(cold) - 1; i >= 0; i-- {
		queue = append(queue, cold[i])
	}
	queue = append(queue, cur)
	m.groupOpening = &groupOpen{name: g.Name, total: len(cold), queue: queue, warm: true}
	return m.advanceGroupOpen()
}

// parkedGroupRoot reports whether root has a parked workspace, symlinks
// resolved.
func (m Model) parkedGroupRoot(root string) bool {
	for _, bg := range m.ws.Background() {
		if sameGroupRoot(bg, root) {
			return true
		}
	}
	return false
}
