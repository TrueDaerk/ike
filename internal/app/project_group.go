package app

import (
	"path/filepath"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/project"
)

// project_group.go is the root-model side of project groups (Epic 0510,
// #2571): the picker entry point, the warm-up switch chain behind
// project.group.open, the active-group marker the model carries, and the
// status segment that says where in the group one is standing.
//
// Mechanics: a group open is a chain of ordinary switches. The model rebuild
// is chdir-based, so members are warmed by *visiting* them — the chain runs
// handleSwitchProject for members N…2 in reverse and finally member 1, each
// through the normal transaction (auto-save gate, history record, seamless
// resume of an already-parked member). The remaining chain rides every
// rebuild in the groupOpening carry-over (the allfind session-state block in
// performSwitchOpts), and each hop's SwitchedMsg / SwitchFailedMsg advances
// it. A failed hop is skipped with one notification and the chain continues,
// so the landing is the first *available* member.

// groupOpen is the in-flight open chain: the group, the hops still to run in
// open order, and the tally the status segment and the landing toast read.
type groupOpen struct {
	name string
	// total is the number of present members — the hops the chain runs.
	total int
	// queue holds the hops still to run: members N…2 in reverse, then 1.
	queue []string
	// inFlight is the root of the hop whose switch is running; "" between
	// hops. A SwitchedMsg for any other root is not the chain's.
	inFlight string
	// done counts finished hops — landed, resumed, already active, or
	// skipped — so the segment reads "opening web 2/3" while the chain runs.
	done int
	// skipped lists the members whose hop failed (root gone, chdir error).
	skipped []string
	// warm marks a project.group.warm chain (#2572): the hops re-park the
	// cold members and the last one returns to the root the warm started
	// from; the landing neither moves the marker nor announces an open.
	warm bool
}

// verb is the chain's word for notifications and the status segment:
// "open" for group.open, "warm" for group.warm.
func (o *groupOpen) verb() string {
	if o.warm {
		return "warm"
	}
	return "open"
}

// handleOpenGroupPicker routes project.group.open: the palette locked to the
// group picker mode (internal/project/grouppicker.go); a row lands as
// project.OpenGroupMsg.
func (m Model) handleOpenGroupPicker() (tea.Model, tea.Cmd) {
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(m.paletteContext(), project.GroupPickerPrefix)
	return m, nil
}

// handleOpenGroup starts the warm-up chain for the named group: the members
// are resolved (the missing ones reported once, the stored group untouched),
// then the hops run N…2 and finally 1. Opening while another group is active
// replaces the marker on landing; re-opening the active group re-warms the
// members that are not parked and lands on member 1. Opening from a peek
// escalates it — the first hop is a normal switch away from the peek, which
// records the peeked root (#2136), the existing rule.
func (m Model) handleOpenGroup(msg project.OpenGroupMsg) (tea.Model, tea.Cmd) {
	g, ok := project.FindGroup(config.Get(), msg.Name)
	if !ok {
		m.host.Notify(host.Warn, "group \""+msg.Name+"\" not found")
		return m, nil
	}
	present, missing := project.ResolveGroupRoots(g)
	if len(missing) > 0 {
		m.host.Notify(host.Warn, "group \""+g.Name+"\": "+strconv.Itoa(len(missing))+" of "+
			strconv.Itoa(len(g.Roots))+" roots missing")
	}
	if len(present) == 0 {
		m.host.Notify(host.Error, "group \""+g.Name+"\": no member exists on disk")
		return m, nil
	}
	// Members N…2 in reverse, then member 1 — the landing.
	queue := make([]string, 0, len(present))
	for i := len(present) - 1; i >= 1; i-- {
		queue = append(queue, present[i])
	}
	queue = append(queue, present[0])
	m.groupOpening = &groupOpen{name: g.Name, total: len(present), queue: queue}
	return m.advanceGroupOpen()
}

// advanceGroupOpen runs the next hop of the chain, or finishes it when none is
// left. A hop onto the root one is already standing in is a friendly no-op
// (handleSwitchProject would only notify "already in"), counted as done.
func (m Model) advanceGroupOpen() (tea.Model, tea.Cmd) {
	o := m.groupOpening
	for o != nil && len(o.queue) > 0 {
		next := o.queue[0]
		o.queue = o.queue[1:]
		if sameGroupRoot(m.currentRoot(), next) {
			o.done++
			continue
		}
		o.inFlight = next
		return m.handleSwitchProject(project.SwitchProjectMsg{Root: next})
	}
	return m.finishGroupOpen()
}

// groupHopLanded is the SwitchedMsg side of the chain: a switch that was the
// hop in flight counts as done and the next hop runs. handled=false for any
// other switch, so the caller keeps its own handling (toast, pending opens).
func (m Model) groupHopLanded(root string) (tea.Model, tea.Cmd, bool) {
	o := m.groupOpening
	if o == nil || o.inFlight == "" || !sameGroupRoot(o.inFlight, root) {
		return m, nil, false
	}
	o.inFlight = ""
	o.done++
	next, cmd := m.advanceGroupOpen()
	return next, cmd, true
}

// groupHopFailed is the SwitchFailedMsg side: the failed member is skipped
// with one notification and the chain continues; the model was left
// untouched by the failed transaction, so the previous hop stays active.
func (m Model) groupHopFailed(msg project.SwitchFailedMsg) (tea.Model, tea.Cmd, bool) {
	o := m.groupOpening
	if o == nil || o.inFlight == "" || !sameGroupRoot(o.inFlight, msg.Path) {
		return m, nil, false
	}
	o.inFlight = ""
	o.done++
	o.skipped = append(o.skipped, msg.Path)
	m.host.Notify(host.Warn, "group \""+o.name+"\": skipped "+filepath.Base(msg.Path)+" — "+msg.Err.Error())
	next, cmd := m.advanceGroupOpen()
	return next, cmd, true
}

// finishGroupOpen lands the chain: the marker moves to the group — in the
// model right away, persisted as project.active_group off the loop — and the
// one final toast counts the members that are open. With every hop failed
// nothing changes and the marker stays where it was.
func (m Model) finishGroupOpen() (tea.Model, tea.Cmd) {
	o := m.groupOpening
	m.groupOpening = nil
	if o == nil {
		return m, nil
	}
	opened := o.total - len(o.skipped)
	if o.warm {
		// A warm (#2572) only re-parks: the marker already names the group,
		// and the return hop put the starting root back in front.
		if opened <= 0 {
			m.host.Notify(host.Error, "group \""+o.name+"\": no member could be warmed")
			return m, nil
		}
		m.host.Notify(host.Info, "group "+o.name+" warm · re-parked "+pluralProjects(opened))
		return m, nil
	}
	if opened == 0 {
		m.host.Notify(host.Error, "group \""+o.name+"\": no member could be opened")
		return m, nil
	}
	m.activeGroup = o.name
	m.host.Notify(host.Info, "group "+o.name+" open · "+pluralProjects(opened))
	return m, project.SetActiveGroupCmd(m.cfgOpts, o.name)
}

// abortGroupOpen drops a chain whose hop was cancelled by the user (the
// unsaved-changes prompt's esc): the members visited so far stay parked, the
// marker is untouched, and one notification says where it stopped.
func (m *Model) abortGroupOpen() {
	o := m.groupOpening
	if o == nil {
		return
	}
	m.groupOpening = nil
	m.host.Notify(host.Info, "group "+o.name+" "+o.verb()+" cancelled after "+strconv.Itoa(o.done)+"/"+strconv.Itoa(o.total))
}

// handleActiveGroupWritten is the ActiveGroupMsg handler: a failed marker
// write is worth a toast; a success reloads the config so config.Get() — the
// cap, the picker badge — sees the marker the model already carries.
func (m Model) handleActiveGroupWritten(msg project.ActiveGroupMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.host.Notify(host.Warn, "could not write project.active_group: "+msg.Err.Error())
		return m, nil
	}
	return m, config.Reload(m.cfgOpts)
}

// capGroup is the group the background cap protects (workspace_evict.go):
// the group being opened while the chain runs — its members must not be
// evicted by the very hops that park them, before the marker is persisted —
// then the marker the model carries, then the persisted marker.
func (m Model) capGroup() (project.Group, bool) {
	cfg := config.Get()
	if o := m.groupOpening; o != nil {
		if g, ok := project.FindGroup(cfg, o.name); ok {
			return g, true
		}
	}
	if m.activeGroup != "" {
		if g, ok := project.FindGroup(cfg, m.activeGroup); ok {
			return g, true
		}
	}
	return project.ActiveGroup(cfg)
}

// groupSegment is the status line's group slot: `⦿ web/api` (group / current
// root name) while a group is active, `opening web 2/3` while the chain runs
// (`warming web 1/2` for a group.warm chain, #2572), nothing without a group.
func (m Model) groupSegment() string {
	if o := m.groupOpening; o != nil {
		hop := o.done + 1
		if hop > o.total {
			hop = o.total
		}
		return o.verb() + "ing " + o.name + " " + strconv.Itoa(hop) + "/" + strconv.Itoa(o.total)
	}
	if m.activeGroup == "" {
		return ""
	}
	return "⦿ " + m.activeGroup + "/" + filepath.Base(m.currentRoot())
}

// groupStatusSegment adapts groupSegment to the statusSegment shape.
func groupStatusSegment(m Model, _ *editor.Model) string { return m.groupSegment() }

// currentRoot is the root the IDE is anchored at: the active workspace's
// root, falling back to the working directory before a workspace exists.
func (m Model) currentRoot() string {
	if w := m.activeWS(); w != nil && w.Root != "" {
		return w.Root
	}
	if cwd, err := cachedGetwd(); err == nil {
		return cwd
	}
	return ""
}

// canonicalRoot resolves a root for identity comparisons: symlinks followed
// where the path exists (macOS temp dirs live behind /var -> /private/var),
// cleaned otherwise. Workspace roots come from os.Getwd — the resolved form —
// while group members are stored as typed, so the two only meet here.
func canonicalRoot(root string) string {
	if root == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(root); err == nil {
		return filepath.Clean(r)
	}
	return filepath.Clean(root)
}

// sameGroupRoot reports whether two roots name the same directory, symlinks
// resolved (the recentlocations sameRoot only makes them absolute).
func sameGroupRoot(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return a == b || canonicalRoot(a) == canonicalRoot(b)
}

// pluralProjects renders "1 project" / "3 projects" for the landing toast.
func pluralProjects(n int) string {
	if n == 1 {
		return "1 project"
	}
	return strconv.Itoa(n) + " projects"
}
