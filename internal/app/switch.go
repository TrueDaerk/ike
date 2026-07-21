package app

import (
	"os"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/project"
	"ike/internal/ui"
)

// switch.go is the root-model side of project switching (Roadmap 0090, #3).
// internal/project validates the candidate root and emits SwitchProjectMsg;
// this file guards it against unsaved buffers and performs the re-root as one
// transaction: persist the old project's session/layout, chdir, rebuild the
// model exactly like a fresh start (the whole IDE is anchored at "."), and
// record the open into the recent-projects history.

// handleSwitchProject routes a validated switch request: a root equal to the
// current one is a friendly no-op, otherwise the seamless switch runs
// immediately. Since #777 dirty buffers no longer gate the switch — the whole
// workspace (buffers included) parks in the background and comes back on the
// next switch; the unsaved-changes prompt returns as the M4 eviction guard
// (#780).
func (m Model) handleSwitchProject(msg project.SwitchProjectMsg) (tea.Model, tea.Cmd) {
	if cwd, err := os.Getwd(); err == nil && cwd == msg.Root {
		m.host.Notify(host.Info, "already in "+msg.Root)
		return m, nil
	}
	return m.performSwitch(msg.Root)
}

// dirtyEditorCount counts dirty editor buffers across every pane and tab.
func (m Model) dirtyEditorCount() int {
	n := 0
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			if ed := inst.TabEditor(i); ed != nil && ed.Dirty() {
				n++
			}
		}
	}
	return n
}

// openSwitchPrompt shows the unsaved-changes guard for a pending switch to
// root: save all and switch, discard and switch, or cancel (the current
// project stays untouched).
func (m *Model) openSwitchPrompt(root string) {
	m.switchPending = root
	dirty := m.dirtyEditorCount()
	m.shell.SetContent(ui.ModelContent{
		Heading: "Unsaved changes",
		Body: func() string {
			// CompactPath bounds the line width: the shell drops a box wider
			// than the terminal, which a raw absolute root can force.
			return plural(dirty, "buffer has", "buffers have") + " unsaved changes; switching to\n" +
				project.CompactPath(root) + " closes every open file.\n\n" +
				"  [s]   save all, then switch\n" +
				"  [d]   discard changes and switch\n" +
				"  [esc] cancel — stay in the current project"
		},
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// plural renders "1 buffer has" / "3 buffers have" style phrases.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// switchPromptOpen reports whether the shell currently shows the guard.
func (m Model) switchPromptOpen() bool { return m.switchPending != "" && m.shell.IsOpen() }

// updateSwitchPrompt consumes every key while the guard is open: s saves all
// dirty buffers and switches, d discards them and switches, esc cancels.
// Other keys are swallowed so nothing leaks past a modal decision.
func (m Model) updateSwitchPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	root := m.switchPending
	switch msg.String() {
	case "s":
		m.switchPending = ""
		m.shell.Close()
		// Editor writes apply synchronously inside UpdateTab (the returned
		// cmds only carry follow-up events), so the switch can proceed in the
		// same step; the batch keeps the events flowing.
		saves := m.saveAllDirty()
		next, cmd := m.performSwitch(root)
		return next, tea.Batch(append(saves, cmd)...)
	case "d":
		m.switchPending = ""
		m.shell.Close()
		return m.performSwitch(root)
	case "esc":
		m.switchPending = ""
		m.shell.Close()
		return m, nil
	}
	return m, nil
}

// saveAllDirty writes every dirty editor buffer (background tabs included) and
// returns the editors' follow-up cmds — the same walk editor.saveAll performs.
func (m *Model) saveAllDirty() []tea.Cmd {
	var cmds []tea.Cmd
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			if ed := inst.TabEditor(i); ed != nil && ed.Dirty() {
				cmds = append(cmds, inst.UpdateTab(i, editor.ActionMsg{Action: "write"}))
			}
		}
	}
	return cmds
}

// performSwitch is the seamless project switch (#777). The old project's
// session and layout persist first, then the process chdirs and the live
// workspace — panes, split tree, running terminals, run panes and the debug
// session (stashed in Aux) — parks in the manager's background set instead of
// being torn down (the old #96 terminal adoption is retired: terminals now
// stay with their project and keep running in the background). The model is
// rebuilt through the fresh-start path with the manager carried over: a
// previously parked workspace for the target root resumes exactly as left,
// a first visit builds panes from the saved layout as before. Everything not
// part of the workspace unit (config layer, theme, watcher, MRU,
// breakpoints) re-resolves against the new root. Nothing is mutated when the
// chdir fails.
func (m Model) performSwitch(root string) (tea.Model, tea.Cmd) {
	saveSession(m.snapshotSession())
	if m.activeWS().Tree != nil {
		saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	}
	if err := os.Chdir(root); err != nil {
		return m, func() tea.Msg { return project.SwitchFailedMsg{Path: root, Err: err} }
	}
	invalidateCwd() // the render hot path caches the working directory (#608)
	m.watcher.Stop()

	// Park the live workspace under its root; the debug session rides along
	// in Aux (its bridge goroutines keep running while parked, though events
	// arriving in the background are not applied until re-attach).
	m.activeWS().Aux = wsExtras{dbg: m.dbg, dbgLaunching: m.dbgLaunching, dbgLaunchGen: m.dbgLaunchGen}
	m.ws.Park()

	cfg, diags := config.Load(config.Discover("."))
	config.Set(cfg)
	fresh := buildModel(m.reg, host.FromConfig(cfg), m.host, m.ws)
	// The incoming project's settings layer just applied (0380): surface its
	// load diagnostics like any reload (#793).
	fresh.notifyConfigDiags(diags)
	fresh.StartWatcher(".")

	// Size the fresh model like the first WindowSizeMsg would, then run its
	// Init and the post-switch effects: record the open (success only — we are
	// past every failure point) and announce the switch.
	sizedTM, sizeCmd := fresh.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	sized := sizedTM.(Model)
	// Hold the background set to project.max_workspaces (#780): idle LRU
	// workspaces drop silently, a busy one asks first.
	capCmd := sized.enforceWorkspaceCap()
	return sized, tea.Batch(
		fresh.Init(),
		sizeCmd,
		capCmd,
		project.RecordOpenCmd(config.Discover("."), root, time.Now()),
		func() tea.Msg { return project.SwitchedMsg{Root: root} },
	)
}
