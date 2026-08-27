package app

import (
	"os"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/editor"
	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/project"
	"ike/internal/ui"
	"ike/internal/workspace"
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
//
// With project.auto_save_on_switch on (#2186, the default) the switch first
// runs the auto-save gate (switch_autosave.go): the departing project's dirty
// buffers are written like a manual save, and only buffers with no writable
// home reach a dialog. Off, the switch runs unguarded as before.
func (m Model) handleSwitchProject(msg project.SwitchProjectMsg) (tea.Model, tea.Cmd) {
	if cwd, err := os.Getwd(); err == nil && cwd == msg.Root {
		m.host.Notify(host.Info, "already in "+msg.Root)
		return m, nil
	}
	if m.autoSaveOnSwitch() {
		return m.beginSwitchAutoSave(msg.Root)
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
				guardLine("s", "save all, then switch", true) +
				guardLine("d", "discard changes and switch", false) +
				guardCancel("cancel — stay in the current project")
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

// updateSwitchPrompt consumes every key while the guard is open: s — or enter,
// the primary option (#1356) — saves all dirty buffers and switches, d discards
// them and switches, esc cancels. Other keys are swallowed so nothing leaks
// past a modal decision.
func (m Model) updateSwitchPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	root := m.switchPending
	switch guardAnswer(msg, "s") {
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
// The writes are raw ("write_raw", #1148): the quit/switch/close-guard callers
// proceed (or check Dirty()) synchronously right after, so the writes must not
// defer behind the async format/organize-imports save chain.
func (m *Model) saveAllDirty() []tea.Cmd {
	var cmds []tea.Cmd
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			if ed := inst.TabEditor(i); ed != nil && ed.Dirty() {
				cmds = append(cmds, inst.UpdateTab(i, editor.ActionMsg{Action: "write_raw"}))
			}
		}
	}
	return cmds
}

// switchOpts tunes performSwitchOpts for the peek flavours (#2136); the
// zero value plus record=true is the plain seamless switch.
type switchOpts struct {
	// record appends the target open to project.history after the switch;
	// off for a peek-enter, so a quick look-up neither enters the history
	// nor becomes the startup restore head.
	record bool
	// escalatePeek records the *departing* root too when it is a peek (#2136
	// escalation): a normal switch away from a peek converts it into a
	// normal workspace. Off on peek-enter/-return and on a close, where the
	// departing workspace is being discarded, not kept.
	escalatePeek bool
	// peekEnter marks the fresh model as a peek of the departing root and
	// snapshots the target's session/layout state for the unchanged check.
	peekEnter bool
	// skipUnchangedPeekSave skips persisting the departing peeked project's
	// session/layout when nothing changed since peek-enter, so a ten-second
	// look-up never plants a .ike directory in a repo it only read.
	skipUnchangedPeekSave bool
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
	return m.performSwitchOpts(root, switchOpts{record: true, escalatePeek: true})
}

// performSwitchOpts is performSwitch's tunable core (#2136): the peek flavours
// reuse the whole transaction and only vary history recording, the peek
// marker, and the departing state save.
func (m Model) performSwitchOpts(root string, opts switchOpts) (tea.Model, tea.Cmd) {
	departing := m.activeWS().Root
	// The departing peeked project's state stays unwritten when the peek
	// changed nothing (#2136): a never-visited project keeps no .ike residue
	// from a quick look-up.
	if !(opts.skipUnchangedPeekSave && m.peek != nil && !m.peekStateChanged()) {
		saveSession(m.snapshotSession())
		if m.activeWS().Tree != nil {
			saveLayout(m.activeWS().Tree, m.activeWS().Panes)
		}
	}
	// Settle the departing workspace's crash snapshots through the same seam
	// the quit path uses (#2185), while the departing project is still the
	// working directory. The fresh model below starts with an empty debouncer,
	// so a mark that had not fired yet would simply vanish and the parked
	// dirty buffer would sit unprotected: flush every pending mark to disk
	// now. The snapshots are kept — a crash while the workspace is parked must
	// still be recoverable — and carry this session's ownership token, so
	// coming back does not offer them as crash recovery.
	m.backupFlushWorkspace(m.activeWS())
	if err := os.Chdir(root); err != nil {
		return m, func() tea.Msg { return project.SwitchFailedMsg{Path: root, Err: err} }
	}
	invalidateCwd() // the render hot path caches the working directory (#608)
	// Global tool sessions (#1890) detach from the departing workspace and
	// park on the manager: the parked workspace never holds them, so eviction
	// and project close cannot end them, and the next tool.<name> anywhere
	// re-attaches the same live session. After the chdir on purpose — a failed
	// switch must leave the workspace untouched; the departing layout.json
	// (saved above) may still list the tool, which the restore path resolves
	// by re-attaching the parked session instead of spawning a duplicate.
	m.detachGlobalTools()
	m.watcher.Stop()
	// Stop the old root's scans (#1549): a running find-in-path keeps its
	// goroutine and rg child walking the old tree, sending results into the
	// shared host.Send — the todo-index scan likewise. Cancel also bumps the
	// scan generation, so in-flight result messages are dropped on arrival.
	m.searcher.Cancel()
	m.todoSearch.Cancel()
	// Cut the parking editors loose from the old model's services (#1549):
	// their emitter/hook closures pin the stopped watcher (with its tracked
	// map and sha256 stamps), the dead nav history and the old mru/marks/
	// histories stores — and a save while parked (the busy-close guard's `s`)
	// would write into stores nobody reads. All setters are nil-safe; the
	// resume re-wires fresh services through buildModel's wireEditorEmitters.
	detachWorkspaceServices(m.activeWS())

	// Park the live workspace under its root; the debug session rides along
	// in Aux (its bridge goroutines keep running while parked, though events
	// arriving in the background are not applied until re-attach).
	// The popup terminal (#1398) parks too (#1407): its shells keep running in
	// the background and the whole overlay — tabs, scrollback, open state —
	// comes back when this project resumes. Project-owned floating panels
	// (#1793) park with it; global ones stay out of Aux and are carried into
	// the fresh model below. The fresh model starts with a zero popup of its
	// own.
	m.activeWS().Aux = wsExtras{dbg: m.dbg, dbgLaunching: m.dbgLaunching, dbgLaunchGen: m.dbgLaunchGen,
		popup: m.popup, floats: projectFloatTerms(m.floatTerms)}
	parkedRoot := m.activeWS().Root
	m.ws.Park()
	// Arm the background LSP idle shutdown for the workspace just parked
	// (#1521): its servers stop after project.background_lsp_timeout in the
	// background, unless it resumes first.
	idleCmd := armWorkspaceIdle(parkedRoot)
	// Its terminals stop requesting repaints and batch their PTY ingest
	// (#1522) — output-heavy background jobs must not cost an Update pass
	// per quiet interval for grids nobody renders.
	setWorkspaceTerminalsParked(m.ws.Peek(parkedRoot), true)
	// The parked debug session's output events batch the same way (#1557):
	// a chatty background debuggee must not cost the active workspace one
	// Update+render pass per DAP output event.
	if m.dbg != nil && m.dbg.coal != nil {
		m.dbg.coal.SetParked(true)
	}
	// Its image panes' Kitty placements leave the terminal (#1547): the fresh
	// model starts with an empty liveImages, so nothing else would ever emit
	// the deletes and every parked PNG stayed resident in the host terminal's
	// graphics memory for the process lifetime. The reset transmission state
	// makes the resume retransmit.
	imgCmd := m.releaseWorkspaceImages(m.ws.Peek(parkedRoot))

	cfg, diags := config.Load(config.Discover("."))
	config.Set(cfg)
	fresh := buildModel(m.reg, host.FromConfig(cfg), m.host, m.ws)
	// The usage log is session state (#2235): the recorder rides across the
	// switch — one JSONL file per run, not per project — and the switch
	// itself is a layout event. buildModel's fresh recorder is discarded
	// unstarted (it opens its file lazily, so it never existed on disk).
	fresh.usage = m.usage
	fresh.usage.Layout("project.switch", nil)
	// The notification history is session state, not workspace state (#1514):
	// it rides across the switch (entries carry their project root, so the
	// history view can label foreign ones), as does the unseen counter.
	fresh.history = m.history
	fresh.notifUnseen = m.notifUnseen
	// Global floating terminals (#1793) are app state too: they ride across
	// with their live sessions — process, scrollback, CWD — stacked above
	// whatever popup layer the incoming project restores. A layer that was
	// visible stays visible, so the pinned terminal never silently vanishes.
	if globals := globalFloatTerms(m.floatTerms); len(globals) > 0 {
		fresh.floatTerms = append(fresh.floatTerms, globals...)
		for _, f := range globals {
			f.inst.SetPalette(fresh.pal())
		}
		if m.popup.open {
			fresh.popup.open = true
		}
	}
	// The incoming project's settings layer just applied (0380): surface its
	// load diagnostics like any reload (#793).
	fresh.notifyConfigDiags(diags)
	fresh.StartWatcher(".")
	// The incoming project polls its own forge (#2085): `fresh` carries a new
	// poller for the new root, so its first fetch seeds that project's
	// snapshot silently instead of replaying its backlog as "new".
	fresh.StartForgePoll()

	// Size the fresh model like the first WindowSizeMsg would, then run its
	// Init and the post-switch effects: record the open (success only — we are
	// past every failure point) and announce the switch.
	sizedTM, sizeCmd := fresh.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	sized := sizedTM.(Model)
	// While this workspace was parked its watcher was stopped (#1515), so
	// files edited externally in that window (a coding agent, git operations)
	// never produced events. Reconcile every resumed buffer against disk:
	// clean buffers reload in place, dirty ones arm the conflict guard.
	reconcile := sized.reconcileEditors()
	// A resumed workspace's terminals go live again (#1522): the parked flag
	// drops and each session with pending output delivers its one owed
	// repaint. The resumed popup moved into the model (Aux is consumed), so
	// it un-parks through the model's own reference; a first visit has no
	// terminals and both calls no-op.
	setWorkspaceTerminalsParked(sized.activeWS(), false)
	// The resumed debug session goes back to per-event delivery (#1557);
	// un-parking flushes any batch still buffered.
	if sized.dbg != nil && sized.dbg.coal != nil {
		sized.dbg.coal.SetParked(false)
	}
	for _, inst := range sized.popupLayerInstances() {
		for i := 0; i < inst.TabCount(); i++ {
			if t := inst.TabTerminal(i); t != nil {
				t.SetParked(false)
			}
		}
	}
	// An open global tool follows the switch (#1903): any session still
	// parked after the rebuild — the target's layout restore did not
	// re-attach it — splices into the workspace through the normal open path
	// (slot rule with tab-join, home placement, adaptive split) without
	// moving focus. A session that exited while parked arrives showing the
	// #810 exited overlay with its Restart/Close actions.
	// The attach appends its tabs and activates each one, which would hand
	// this project the tab selection of the project just left (#1906): the
	// pre-attach selection is snapshotted here and re-applied right after,
	// preferring the global tool tab this project itself had active.
	preAttach := sized.snapshotActiveTabs()
	sized.attachOpenGlobalTools()
	sized.restoreActiveToolTabs(preAttach)
	// Same catch-up for the explorer tree (#1520): the auto-refresh poll would
	// get there eventually (and not at all with explorer.auto_refresh off), so
	// re-stat the loaded tree once right now. A first visit's explorer is
	// freshly scanned anyway; its Update ignores the resync until loaded.
	var resync tea.Cmd
	if inst := sized.activeWS().Panes.Get(pane.ExplorerKey); inst != nil {
		resync = inst.Update(explorer.ResyncMsg{})
	}
	// Hold the background set to project.max_workspaces (#780): idle LRU
	// workspaces drop silently, a busy one asks first.
	capCmd := sized.enforceWorkspaceCap()
	// Entering a peek (#2136): mark the fresh model and snapshot its restored
	// session/layout, so the return can tell an untouched peek from one the
	// user edited and skip the state write for the former.
	if opts.peekEnter {
		sized.peek = &peekState{origin: departing}
		sized.peek.enterSession, sized.peek.enterLayout = sized.peekStateSnapshot()
	}
	// History recording (success only — we are past every failure point): a
	// peek-enter records nothing, a normal switch away from a peek records the
	// peeked root first (escalation, #2136) — sequentially in one cmd, two
	// concurrent RecordOpenCmds would race on the config write.
	var recordCmd tea.Cmd
	switch {
	case opts.record && opts.escalatePeek && m.peek != nil:
		peeked := departing
		recordCmd = func() tea.Msg {
			cfgOpts := config.Discover(".")
			_ = project.RecordOpen(cfgOpts, peeked, time.Now())
			return project.RecordedMsg{Root: root, Err: project.RecordOpen(cfgOpts, root, time.Now())}
		}
	case opts.record:
		recordCmd = project.RecordOpenCmd(config.Discover("."), root, time.Now())
	}
	return sized, tea.Batch(
		fresh.Init(),
		sizeCmd,
		imgCmd,
		capCmd,
		reconcile,
		resync,
		idleCmd,
		recordCmd,
		func() tea.Msg { return project.SwitchedMsg{Root: root} },
	)
}

// reconcileEditors sends every editor tab a ReconcileMsg (#1515): the
// buffer-vs-disk catch-up for changes that happened while the workspace was
// parked without a watcher. Same pane/tab walk as saveAllDirty.
func (m *Model) reconcileEditors() tea.Cmd {
	var cmds []tea.Cmd
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			if ed := inst.TabEditor(i); ed != nil && ed.HasFile() {
				if cmd := inst.UpdateTab(i, editor.ReconcileMsg{}); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// detachWorkspaceServices strips the parking workspace's editors of every
// closure over the old model's services (#1549): emitter (watcher + nav
// history), breakpoint and mark hooks (their stores), query histories and the
// completion MRU. Without this a parked workspace pins the stopped watcher,
// a dead nav history and stale store pointers for as long as it stays parked,
// and a save while parked writes into stores nobody reads. Every setter is
// nil-safe; resuming re-wires the fresh model's services through
// wireEditorEmitters.
func detachWorkspaceServices(w *workspace.Workspace) {
	if w == nil || w.Panes == nil {
		return
	}
	for _, key := range w.Panes.Keys() {
		inst := w.Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			ed.SetEmitter(nil)
			ed.SetBreakpointSource(nil)
			ed.SetBreakpointDisabledSource(nil)
			ed.SetBreakpointConditionalSource(nil)
			ed.SetBreakpointLogpointSource(nil)
			ed.SetBreakpointAdjuster(nil)
			ed.SetMarkHooks(nil, nil, nil)
			ed.SetBookmarkHooks(nil, nil)
			ed.SetHistories(nil)
			ed.SetCompletionMRU(nil)
			// In-flight .http markers (#1746) belong to the model that
			// dispatched: a parked workspace must not keep a spinner that
			// nothing refreshes any more.
			ed.SetHTTPFlight(nil)
			// Registers deliberately stay: the store is manager-owned and
			// app-wide (#1540) — the parked editors keep pointing at the same
			// store the fresh model uses, which is exactly the cross-workspace
			// sharing the feature is about, and it pins no per-model service.
		}
	}
}
