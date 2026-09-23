package app

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/deeplink"
	"ike/internal/host"
	"ike/internal/netlink"
	"ike/internal/telemetry"
)

// netlink_close.go is the root-model side of the network close command
// (#2703). A paired client asks to close a project; the request rides into
// the Update loop — where collectActivity lives — the busy guard is
// evaluated exactly as project.close / the close-from-list would evaluate
// it, and the verdict goes back to the waiting connection. An idle project
// closes through the very paths the UI uses; a busy one answers with the
// guard's summary lines and the server mints the one-time force token; a
// force whose grant still matches runs the UI guard's discard branch. The
// IDE never quits on a network request: the last open project is torn down
// and reopened afresh instead.

// netCloseMsg carries one close request into the Update loop. reply is
// called exactly once with the verdict; the connection goroutine waits on
// it (with a timeout) and answers the client.
type netCloseMsg struct {
	req   netlink.CloseRequest
	reply func(netlink.CloseResult)
}

// handleNetClose resolves, guards and — when allowed — performs one network
// close, answering the connection through msg.reply.
func (m Model) handleNetClose(msg netCloseMsg) (tea.Model, tea.Cmd) {
	reply := msg.reply
	if reply == nil {
		reply = func(netlink.CloseResult) {}
	}
	// A close or quit guard already asking the user owns the decision; a
	// remote verdict landing under it would pull the workspace out from
	// under the prompt.
	if m.projectClosePromptOpen() || m.wsClosePromptOpen() || m.closePromptOpen() {
		reply(netlink.CloseResult{Outcome: netlink.CloseUnavailable,
			Message: "IKE is asking the user about a close right now — try again in a moment"})
		return m, nil
	}
	root, active, ok := m.netCloseTarget(msg.req)
	if !ok {
		reply(netlink.CloseResult{Outcome: netlink.CloseUnknown})
		return m, nil
	}
	name := filepath.Base(root)
	act := m.netCloseActivity(root, active)
	reasons := act.summary()
	if msg.req.Force {
		if !msg.req.Grant.Matches(root, reasons) {
			reply(netlink.CloseResult{Outcome: netlink.CloseStale, Root: root, Project: name})
			return m, nil
		}
	} else if act.busy() {
		// The guard would prompt; the wire gets the very lines the prompt
		// would show, and the UI stays untouched.
		reply(netlink.CloseResult{Outcome: netlink.CloseBlocked, Root: root, Project: name, Reasons: reasons})
		return m, nil
	}
	next, cmd, closed := m.netPerformClose(root, active)
	if !closed {
		reply(netlink.CloseResult{Outcome: netlink.CloseUnavailable, Root: root, Project: name,
			Message: "the close did not go through — the project directory may be gone"})
		return next, cmd
	}
	next.host.Notify(host.Info, netCloseNotice(name, msg.req.Client, act, msg.req.Force))
	reply(netlink.CloseResult{Outcome: netlink.CloseClosed, Root: root, Project: name})
	return next, cmd
}

// netCloseTarget resolves a close request among the open workspaces — the
// active one and every parked one — the way open resolves a link: by the
// root's directory name (case-insensitively) or by the normalised remote
// key of its git remote. No project at all names the active one. A
// project that is not open cannot be closed, so history is not consulted.
func (m Model) netCloseTarget(req netlink.CloseRequest) (root string, active bool, ok bool) {
	activeRoot := ""
	if w := m.ws.Active(); w != nil {
		activeRoot = strings.TrimSpace(w.Root)
	}
	if activeRoot == "" {
		activeRoot = currentProjectRoot()
	}
	roots := []string{}
	if activeRoot != "" {
		roots = append(roots, activeRoot)
	}
	roots = append(roots, m.ws.Background()...)
	if req.Project == "" && req.Remote == "" {
		if activeRoot == "" {
			return "", false, false
		}
		return activeRoot, true, true
	}
	for _, r := range roots {
		match := false
		switch {
		case req.Project != "":
			match = strings.EqualFold(filepath.Base(r), req.Project)
		case req.Remote != "":
			for _, key := range deeplink.Remotes(r) {
				if key == req.Remote {
					match = true
					break
				}
			}
		}
		if match {
			return r, r == activeRoot, true
		}
	}
	return "", false, false
}

// netCloseActivity is the guard's inventory for root: the active workspace
// counts its popup terminal and floating panels like project.close does; a
// parked one is inventoried as the close-from-list guard would.
func (m Model) netCloseActivity(root string, active bool) wsActivity {
	if active {
		return m.activeCloseActivity()
	}
	return collectActivity(m.ws.Peek(root))
}

// netPerformClose runs the close for an already-guarded target through the
// UI's own paths: a parked workspace drops like the close-from-list, the
// active one closes and resumes the MRU parked project like project.close,
// and the last open project restarts afresh (never a quit). closed reports
// whether the workspace is in fact gone — a failed chdir leaves it in place.
func (m Model) netPerformClose(root string, active bool) (Model, tea.Cmd, bool) {
	if !active {
		cmd := m.finishWorkspaceClose(root)
		return m, cmd, true
	}
	before := m.ws.Active()
	var next tea.Model
	var cmd tea.Cmd
	if bg := m.ws.Background(); len(bg) > 0 {
		next, cmd = m.performCloseAndSwitch(bg[len(bg)-1])
	} else {
		next, cmd = m.performCloseRestart()
	}
	nm, ok := next.(Model)
	if !ok {
		return m, cmd, false
	}
	// A close that went through replaced the active workspace unit; a failed
	// switch returned the very same one.
	return nm, cmd, nm.ws.Active() != before
}

// performCloseRestart closes the last open project without quitting
// (#2703): the workspace tears down — sessions end, unsaved buffers are
// discarded — and the same root is rebuilt through the fresh-start path, so
// the IDE stands where `ike` launched in that directory would.
func (m Model) performCloseRestart() (tea.Model, tea.Cmd) {
	endOp := m.usage.OpTimer(telemetry.OpProjectClose)
	root := ""
	if w := m.ws.Active(); w != nil {
		root = w.Root
	}
	if root == "" {
		root = currentProjectRoot()
	}
	exempt := collectActivity(m.ws.Active())
	before := m.ws.Active()
	next, cmd := m.performSwitchOpts(root, switchOpts{record: true, closing: true, restart: true})
	sized, ok := next.(Model)
	if !ok || sized.ws.Active() == before {
		endOp("error", nil)
		return next, cmd
	}
	endOp("ok", nil)
	sized.host.Notify(host.Info, "closed project "+filepath.Base(root)+" — reopened at its start state")
	sized.notifyExemptTools(exempt)
	return sized, cmd
}

// netCloseNotice words the notice every remote close raises, so a close
// never happens invisibly: who asked, and — for a forced one — what it
// discarded and stopped.
func netCloseNotice(project string, c netlink.Client, act wsActivity, forced bool) string {
	who := strings.TrimSpace(c.Name)
	if who == "" {
		who = strings.TrimSpace(c.Addr)
	}
	if who == "" {
		who = "unknown device"
	}
	text := "closed " + project + " via network (" + who + ")"
	if !forced {
		return text
	}
	var parts []string
	if n := len(act.dirty); n > 0 {
		parts = append(parts, "discarded "+plural(n, "unsaved buffer", "unsaved buffers"))
	}
	if n := len(act.running); n > 0 {
		parts = append(parts, "stopped "+plural(n, "running process", "running processes"))
	}
	if len(parts) == 0 {
		return text
	}
	return text + " — " + strings.Join(parts, ", ")
}
