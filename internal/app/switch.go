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
	"ike/internal/lang"
	"ike/internal/pane"
	"ike/internal/project"
	"ike/internal/telemetry"
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
func (m *Model) saveAllDirty() []tea.Cmd { return m.writeDirtyTabs("write_raw") }

// writeDirtyTabs runs action on every dirty editor buffer in the workspace,
// background tabs included, and returns the editors' follow-up cmds — the one
// walk both save-everything callers share (#2463). editor.saveAll passes
// "write" (the full save chain), the quit/switch/close guards "write_raw"
// (#1148), whose write lands synchronously inside UpdateTab so the caller can
// check Dirty() in the same step.
func (m *Model) writeDirtyTabs(action string) []tea.Cmd {
	var cmds []tea.Cmd
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			if ed := inst.TabEditor(i); ed != nil && ed.Dirty() {
				cmds = append(cmds, inst.UpdateTab(i, editor.ActionMsg{Action: action}))
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
	// closing marks the switch as the tail of a project close (#1355), so the
	// departing project's project.leave event says "close" rather than
	// "switch" (#2408) — the same transaction, a different intent.
	closing bool
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
	// The switch is a timed operation (#2403): telemetry only ever held the
	// layout marker and the session marker, so the seconds between them —
	// chdir, state persistence, layout restore, LSP warm-up — were invisible.
	// The op pair brackets exactly the transaction below; a failed chdir ends
	// it as "error", the ready model as "ok".
	switchStart := time.Now()
	// A previous switch's warm-up wait still armed can never resolve once this
	// switch discards the model holding it — close it as "superseded" (#2492)
	// before the new op starts, so its phase lands on the right side of the
	// next "start" in the export.
	m.noteSwitchLSPSkipped("superseded")
	endOp := m.usage.OpTimer(telemetry.OpProjectSwitch)
	// Whether the target is a workspace coming back from the background is
	// known before the rebuild consumes it, and it is the first thing that
	// explains a fast switch from a slow one.
	parked := m.ws.Peek(root) != nil
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
	// The departing project's token must be taken before the chdir — it hashes
	// the working directory (#2408).
	leaveToken := telemetryProjectToken()
	if err := os.Chdir(root); err != nil {
		endOp("error", map[string]string{"parked": strconv.FormatBool(parked)})
		return m, func() tea.Msg { return project.SwitchFailedMsg{Path: root, Err: err} }
	}
	// The departing project's time budget closes here: after the chdir
	// committed the switch — a failed one leaves nothing to report — and
	// before the fresh model below starts its own clock, so the spans of two
	// projects never overlap.
	leaveReason := "switch"
	if opts.closing {
		leaveReason = "close"
	}
	m.recordProjectLeave(leaveToken, leaveReason)
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
	// The documentSymbol cache rides along (#2401): it is keyed by this
	// project's file paths, so parking it lets a resumed project refeed the
	// Structure panel, breadcrumbs and sticky scopes from memory instead of
	// re-asking the language server for every unchanged buffer.
	m.activeWS().Aux = wsExtras{dbg: m.dbg, dbgLaunching: m.dbgLaunching, dbgLaunchGen: m.dbgLaunchGen,
		popup: m.popup, floats: projectFloatTerms(m.floatTerms), docSymbols: m.docSymbols}
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
	// With terminal.popup_scope = "global" (#2406) the popup terminal is app
	// state, not project state: it is lifted back out of the parking payload
	// here — the incoming project's settings layer decides, so the scope is
	// read from the config just loaded — and handed to the fresh model below
	// with its shells, tabs and scrollback intact, the deal global floating
	// panels (#1793) get. The parked workspace keeps no popup, so eviction and
	// close can never end the shell the whole app shares.
	globalPopup, carryPopup := popupTerm{}, cfg.Terminal.PopupScope == "global"
	if carryPopup {
		globalPopup = m.popup
		if w := m.ws.Peek(parkedRoot); w != nil {
			if extras, ok := w.Aux.(wsExtras); ok {
				extras.popup = popupTerm{}
				w.Aux = extras
			}
		}
	}
	fresh := buildModel(m.reg, host.FromConfig(cfg), m.host, m.ws)
	// The usage log is session state (#2235): the recorder rides across the
	// switch — one JSONL file per run, not per project — and the switch
	// itself is a layout event. buildModel's fresh recorder is discarded
	// unstarted (it opens its file lazily, so it never existed on disk).
	fresh.usage = m.usage
	fresh.usage.Layout("project.switch", nil)
	// Re-emit the session marker (#2348) with the new project's token — the
	// cwd changed above, so events from here on attribute to the right state
	// directory. buildModel emitted one on the discarded fresh recorder; this
	// is the one that lands in the session file.
	recordTelemetrySession(fresh.usage)
	// The notification history is session state, not workspace state (#1514):
	// it rides across the switch (entries carry their project root, so the
	// history view can label foreign ones), as does the unseen counter.
	fresh.history = m.history
	fresh.notifUnseen = m.notifUnseen
	// The all-projects search (#2394) is session state on the same terms: its
	// scan spans projects, so the service (an in-flight scan keeps streaming
	// into the same host), the results overlay — the result set outlives the
	// switch a hit caused, so the user walks on through the other projects'
	// hits (#2413) — and a pending match-open, the very reason for this
	// switch, finished by the SwitchedMsg handler, ride across. Only the
	// palette re-threads; the form does not carry (it is modal and closed
	// before any switch can run).
	fresh.allSearch = m.allSearch
	fresh.allResults = m.allResults
	fresh.allResults.SetPalette(fresh.pal())
	fresh.allFindGen = m.allFindGen
	fresh.allPendingOpen = m.allPendingOpen
	fresh.allFindRecent = m.allFindRecent
	// Deep-link state (#2396) is session state on the same terms: the socket
	// endpoint serves the whole run, and a link's parked payload — the very
	// reason for this switch, finished by the SwitchedMsg handler — rides
	// across, as does a link waiting on a running clone.
	fresh.dlServer = m.dlServer
	fresh.dlPending = m.dlPending
	fresh.dlAfterClone = m.dlAfterClone
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
			// A blurred layer (#2309) stays blurred: the switch was driven
			// from the panes, so the keyboard stays with them.
			fresh.popup.blurred = m.popup.blurred
		}
	}
	// A global popup terminal (#2406) rides across on the same terms: the box
	// the departing model held becomes the fresh model's, with its running
	// shells and scrollback. A popup the incoming project parked under an
	// earlier project scope would be a second box for the one global slot, so
	// its sessions end here — one popup is the whole promise of the scope.
	if carryPopup {
		for _, inst := range fresh.popup.instances() {
			inst.CloseTerminalTabs()
		}
		fresh.popup = globalPopup
		for _, inst := range fresh.popup.instances() {
			inst.SetPalette(fresh.pal())
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
	// A carried global popup follows the project (#2406): every one of its
	// shells sitting idle at a prompt is asked to cd into the new root, so the
	// one terminal that spans projects is never left in the project you just
	// walked away from. A shell with a foreground job keeps it — its stdin
	// belongs to that job — and picks the new root up on the next switch it is
	// idle for. After the size pass, so the line is typed into a sized PTY.
	if carryPopup {
		sized.cdPopupShellsTo(root)
	}
	// With terminal.popup_on_switch = "always-open" (#2362) the incoming
	// project's popup terminal opens no matter how it was left: the parked
	// instance this switch just resumed comes back with its tabs and running
	// shells, a project without a box gets a fresh shell. Run after the
	// un-park above so a resumed popup is already live, and after the size
	// pass so a freshly spawned box is measured against the real bounds.
	// "restore" — the default — leaves the state #1407 restored untouched.
	if sized.popupOnSwitchAlways() {
		sized.ensurePopupTerminalOpen()
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
	// The switch transaction ends here: the new model is built, sized,
	// reconciled and ready to render (#2403). panes counts what had to be
	// restored or resumed — the main cost driver next to the parked flag. The
	// language servers are not part of this span: their first publish for the
	// new root arrives asynchronously long after the model is ready, so lsp is
	// -1 here and the armed wait reports the real number as its own "lsp"
	// phase on the same op id (telemetry.go). Every ok is followed by exactly
	// one such phase (#2492): a model with no server-language document open
	// reports skipped=no_server_docs on the spot, an armed wait that never
	// sees a publish is closed by the quiet fallback timer.
	sized.switchLSPWait = &switchLSPWait{start: switchStart}
	endOp("ok", map[string]string{
		"parked": strconv.FormatBool(parked),
		"panes":  strconv.Itoa(len(sized.activeWS().Panes.Keys())),
		"lsp":    "-1",
	})
	var lspQuietCmd tea.Cmd
	if sized.switchHasServerDocs() {
		lspQuietCmd = armSwitchLSPQuiet(sized.switchLSPWait)
	} else {
		sized.noteSwitchLSPSkipped("no_server_docs")
	}
	return sized, tea.Batch(
		fresh.Init(),
		sizeCmd,
		imgCmd,
		capCmd,
		reconcile,
		resync,
		idleCmd,
		lspQuietCmd,
		recordCmd,
		func() tea.Msg { return project.SwitchedMsg{Root: root} },
	)
}

// switchHasServerDocs reports whether any open editor document of the freshly
// built model belongs to a language with a server (#2492) — the same walk
// Init's EventFileOpened announcement does. When none does, no didOpen will
// ever fire and no publish can close the warm-up wait, so the switch reports
// the "lsp" phase as skipped instead of arming it.
func (m *Model) switchHasServerDocs() bool {
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			if !ed.HasFile() {
				continue
			}
			if l, ok := lang.ByPath(ed.Path()); ok && l.HasServer() {
				return true
			}
		}
	}
	return false
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

// handleSwitchLastProject routes project.switchLast (#2398): resume the most
// recently used background workspace — the last element of the manager's MRU
// order, the same pick project.close uses. Because the departing project
// becomes the MRU parked one, invoking the command again switches straight
// back: an alt+tab between the two projects a session ping-pongs between.
//
// The switch goes through handleSwitchProject, so it is a full switch and not
// a peek (#2136): history is recorded and the auto-save gate (#2186) runs
// exactly as for a palette-driven switch. With no background workspace nothing
// changes and a notification says so.
func (m Model) handleSwitchLastProject() (tea.Model, tea.Cmd) {
	bg := m.ws.Background()
	if len(bg) == 0 {
		m.host.Notify(host.Info, "no previous project")
		return m, nil
	}
	return m.handleSwitchProject(project.SwitchProjectMsg{Root: bg[len(bg)-1]})
}
