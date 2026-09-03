package app

import (
	"path/filepath"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/breakpanel"
	"ike/internal/dap"
	"ike/internal/debug"
	"ike/internal/host"
	"ike/internal/pane"
)

// breakpoints_panel.go wires the Breakpoints tool window (#1377): a singleton
// bottom-split pane over the app-owned breakpoint store (#577), mirroring the
// Problems panel's toggle state machine. The panel is a pure consumer — every
// mutation (enable/disable, delete, delete-all) routes back here, so the
// gutter, the store, the persistence file and a live debug session stay in
// one line.

// BreakpointsToggleMsg runs debug.breakpoints.
type BreakpointsToggleMsg struct{}

// toggleBreakpointsPanel is the debug.breakpoints state machine, mirroring
// toggleProblemsPanel: no panel → open at the bottom; unfocused → focus it;
// focused → return focus to the remembered pane.
func (m *Model) toggleBreakpointsPanel() {
	if !m.activeWS().Panes.Has(pane.BreakpointsKey) {
		m.breakpointsReturnFocus = m.activeWS().Panes.Focused()
		m.openBreakpointsPanel()
		return
	}
	if m.activeWS().Panes.Focused() != pane.BreakpointsKey {
		m.breakpointsReturnFocus = m.activeWS().Panes.Focused()
		m.setFocus(pane.BreakpointsKey)
		return
	}
	target := m.breakpointsReturnFocus
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = m.activeEditorKey()
	}
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = pane.ExplorerKey
	}
	m.setFocus(target)
}

// breakpointsPanel returns the singleton panel model, or nil when it is not open.
func (m Model) breakpointsPanel() *breakpanel.Model {
	if !m.activeWS().Panes.Has(pane.BreakpointsKey) {
		return nil
	}
	return m.activeWS().Panes.Get(pane.BreakpointsKey).Breakpoints()
}

// openBreakpointsPanel splits the active editor (fallback: focused leaf) at
// the adaptive placement (auxZone, #1588) with the singleton panel, seeded
// from the live breakpoint store.
func (m *Model) openBreakpointsPanel() {
	target := m.activeEditorKey()
	if target == "" {
		target = m.activeWS().Panes.Focused()
	}
	if target == "" || m.activeWS().Tree == nil {
		return
	}
	key := m.activeWS().Panes.AddBreakpoints()
	if !m.insertToolPane(key, target, m.auxZone(target)) {
		m.activeWS().Panes.Close(key)
		return
	}
	m.wireBreakpointsPanel(m.activeWS().Panes.Get(key).Breakpoints())
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// wireBreakpointsPanel seeds a panel instance from the live store; layout
// restore uses it too.
func (m *Model) wireBreakpointsPanel(p *breakpanel.Model) {
	p.SetDisplayPath(displayPath)
	p.SetPreview(func(path string, line int) string {
		return markPreview(fileLine(m.bpAbsPath(path), line))
	})
	// The refinement fields are gated on the live adapter's capabilities
	// (#2245); without a session every field is editable.
	p.SetCaps(m.breakpointCaps())
	p.SetStore(m.bpts)
}

// refreshBreakpointsPanel re-derives an open panel's rows after a store
// mutation — gutter toggles included — so list and gutter never disagree; a
// closed panel costs nothing.
func (m *Model) refreshBreakpointsPanel() {
	if p := m.breakpointsPanel(); p != nil {
		p.SetCaps(m.breakpointCaps())
		p.Refresh()
	}
}

// bpAbsPath resolves a store key (project-relative when the file lives under
// the project root) back to an openable absolute path.
func (m Model) bpAbsPath(key string) string {
	if filepath.IsAbs(key) {
		return key
	}
	return filepath.Join(projectRoot(), key)
}

// handleBreakpanelMsg routes the panel's action messages; handled reports
// whether msg was one of them.
func (m Model) handleBreakpanelMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case breakpanel.OpenLocationMsg:
		// A row activation: open the file with the cursor on the breakpoint
		// line (already 0-based, like definition targets).
		model, cmd := m.openPathAt(m.bpAbsPath(msg.Path), msg.Line, 0)
		return model, cmd, true
	case breakpanel.ToggleEnabledMsg:
		on := !m.bpts.Enabled(msg.Path, msg.Line)
		m.bpts.SetEnabled(msg.Path, msg.Line, on)
		m.saveBreakpoints()
		state := "disabled"
		if on {
			state = "enabled"
		}
		m.host.Notify(host.Info, "breakpoint "+state+" — "+displayPath(m.bpAbsPath(msg.Path))+":"+strconv.Itoa(msg.Line+1))
		m.refreshBreakpointsPanel()
		return m, m.syncSessionBreakpoints(m.bpAbsPath(msg.Path)), true
	case breakpanel.RemoveMsg:
		if m.bpts.Remove(msg.Path, msg.Line) {
			m.saveBreakpoints()
			m.host.Notify(host.Info, "breakpoint removed — "+displayPath(m.bpAbsPath(msg.Path))+":"+strconv.Itoa(msg.Line+1))
		}
		m.refreshBreakpointsPanel()
		return m, m.syncSessionBreakpoints(m.bpAbsPath(msg.Path)), true
	case breakpanel.SetMetaMsg:
		// Condition / hit count / log message edited in the panel (#1914):
		// store, persist, refresh and push to a live session like every other
		// breakpoint mutation.
		m.bpts.SetMeta(msg.Path, msg.Line, msg.Meta)
		m.saveBreakpoints()
		m.refreshBreakpointsPanel()
		return m, m.syncSessionBreakpoints(m.bpAbsPath(msg.Path)), true
	case breakpanel.RemoveAllMsg:
		paths := make([]string, 0, len(m.bpts.All()))
		for p := range m.bpts.All() {
			paths = append(paths, p)
		}
		n := m.bpts.RemoveAll()
		m.saveBreakpoints()
		m.host.Notify(host.Info, "removed "+strconv.Itoa(n)+" breakpoints")
		m.refreshBreakpointsPanel()
		var cmds []tea.Cmd
		for _, p := range paths {
			if cmd := m.syncSessionBreakpoints(m.bpAbsPath(p)); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...), true
	}
	return m, nil, false
}

// saveBreakpoints persists the store, surfacing a failure as a warning like
// the gutter toggle does.
func (m *Model) saveBreakpoints() {
	if err := m.bpts.Save(); err != nil {
		m.host.Notify(host.Warn, "breakpoints not saved: "+err.Error())
	}
}

// syncSessionBreakpoints pushes abs's enabled breakpoint lines to a live
// debug session (#579); without one it is free.
func (m *Model) syncSessionBreakpoints(abs string) tea.Cmd {
	dbg := m.dbg
	if dbg == nil {
		return nil
	}
	bps := dapBreakpoints(m.sessionSpecs(bpKey(abs)))
	send := m.host.Send
	sess := dbg.sess
	go func() {
		if _, err := sess.SetBreakpoints(abs, bps); err != nil {
			send(debugErrMsg{err: err})
		}
	}()
	return nil
}

// dapBreakpoints maps the store's enabled breakpoints onto the wire shape,
// carrying the #1914 refinements (condition, hit count, log message).
func dapBreakpoints(specs []debug.Spec) []dap.SourceBreakpoint {
	out := make([]dap.SourceBreakpoint, len(specs))
	for i, sp := range specs {
		out[i] = dap.SourceBreakpoint{
			Line:         sp.Line,
			Condition:    sp.Condition,
			HitCondition: sp.HitCondition,
			LogMessage:   sp.LogMessage,
		}
	}
	return out
}
