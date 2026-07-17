package app

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ike/internal/dap"
	"ike/internal/debugpanel"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/layout"
	"ike/internal/lsp/transport"
	"ike/internal/pane"
	"ike/internal/run"
	"ike/internal/terminal"
)

// debugsession.go orchestrates one live DAP session (0350, #579): debug.start
// launches the active file's run configuration through the language's
// adapter, the session stops at the stored breakpoints, and the stepping
// commands (F7/F8/F9/shift+F8) drive it while it is paused. One session at a
// time — starting a new one stops the old.

// debugState is the live session's UI-relevant state. It hangs off the root
// model behind a pointer so Update's value copies share it.
type debugState struct {
	sess    *dap.Session
	cfgName string
	root    string

	threadID   int
	paused     bool
	frames     []dap.StackFrame
	pausedPath string // file carrying the paused-line marker
	termKey    string // debuggee terminal pane (runInTerminal, #625), "" if none

	// pendingOut buffers debuggee output that arrived before the tool window
	// opened (output can precede the first stop); openDebugPanel flushes it into
	// the panel so nothing is lost from the live console (#624). Capped at
	// maxPendingOut chunks, oldest dropped (#637).
	pendingOut []debugOut

	// panelOpened records that the tool window opened once this session (#637):
	// the first output event opens it so a program that never pauses is still
	// visible, but a panel the user closed afterwards stays closed.
	panelOpened bool
}

// maxPendingOut caps the pre-panel output buffer, the same order as the
// panel's maxOutputLines, so a chatty debuggee cannot grow it without bound
// (#637). Chunks, not lines — close enough for a memory bound.
const maxPendingOut = 5000

// appendPendingOut buffers one pre-panel output chunk, dropping the oldest
// past the cap.
func appendPendingOut(buf []debugOut, o debugOut) []debugOut {
	buf = append(buf, o)
	if len(buf) > maxPendingOut {
		buf = buf[len(buf)-maxPendingOut:]
	}
	return buf
}

// debugOut is one buffered output chunk with its stream.
type debugOut struct {
	stderr bool
	text   string
}

// Messages carrying async session activity back into Update.
type (
	// debugEventMsg is one raw adapter event.
	debugEventMsg struct{ ev dap.Event }
	// debugStoppedMsg carries the fetched stop context (thread + frames).
	debugStoppedMsg struct {
		threadID int
		frames   []dap.StackFrame
	}
	// debugErrMsg surfaces an async session error.
	debugErrMsg struct{ err error }
	// debugEndedMsg reports session termination (exit code when known).
	debugEndedMsg struct {
		exitCode int
		hasCode  bool
	}
	// debugScopesMsg carries a frame's fetched scopes for the panel (#580).
	debugScopesMsg struct{ scopes []dap.Scope }
	// debugVarsMsg carries one variablesReference's fetched children.
	debugVarsMsg struct {
		ref  int
		vars []dap.Variable
	}
	// debugInstallResultMsg reports the adapter-runtime auto-install (#589);
	// success relaunches the pending debug configuration.
	debugInstallResultMsg struct {
		cfg  run.Config
		root string
		err  error
		// gen is the launch generation the install was started under (#636);
		// a mismatch on arrival means the launch was cancelled meanwhile.
		gen int
	}
	// debugRunInTerminalMsg carries the adapter's runInTerminal reverse request
	// (#625) from the read-loop goroutine onto the Update loop, where the
	// debuggee terminal pane is spawned and the request answered.
	debugRunInTerminalMsg struct {
		seq  int
		args dap.RunInTerminalArgs
	}
)

// startDebug is the debug.start handler: it resolves the active file's run
// configuration and launches it under the language's DAP adapter.
func (m *Model) startDebug() {
	path := m.activeFilePath()
	if path == "" {
		m.host.Notify(host.Info, "debug: focus a file tab first")
		return
	}
	root := projectRoot()
	store := run.Load()
	cfg, _, ok := store.EnsureFor(root, path)
	if !ok {
		m.host.Notify(host.Info, "debug: no run command for this file type")
		return
	}
	if !lang.SupportsDebug(cfg.Lang) {
		m.host.Notify(host.Info, "debug: "+cfg.Lang+" has no debug adapter yet")
		return
	}
	if m.dbg != nil || m.dbgLaunching {
		m.host.Notify(host.Info, "debug: a session is already running")
		return
	}
	store.Touch(cfg.Name)
	_ = run.Save(store)
	m.dbgLaunching = true
	m.launchOrInstall(root, *cfg, false)
}

// launchOrInstall preflights the adapter runtime (#589): a missing runtime
// (debugpy) auto-installs asynchronously and the launch retries once after;
// a runtime still missing then surfaces the manual command instead of
// looping.
func (m *Model) launchOrInstall(root string, cfg run.Config, afterInstall bool) {
	explicit := m.explicitInterpreter(cfg.Lang)
	missing, reason := lang.DebugAdapterMissing(cfg.Lang, root, explicit)
	if !missing {
		m.launchDebug(root, cfg)
		return
	}
	candidates := lang.DebugAdapterInstallCommands(cfg.Lang, root, explicit)
	if afterInstall || len(candidates) == 0 {
		hint := ""
		if len(candidates) > 0 {
			hint = " — install manually: " + strings.Join(candidates[len(candidates)-1], " ")
		}
		m.host.Notify(host.Error, "debug: "+reason+hint)
		m.dbgLaunching = false
		return
	}
	m.host.Notify(host.Info, "debug: "+reason+" — installing…")
	send := m.host.Send
	gen := m.dbgLaunchGen
	go func() {
		err := runAdapterInstall(candidates)
		send(debugInstallResultMsg{cfg: cfg, root: root, err: err, gen: gen})
	}()
}

// adapterInstallTimeout bounds one install attempt.
const adapterInstallTimeout = 3 * time.Minute

// runAdapterInstall tries the candidates in order until one succeeds. A
// candidate whose program is not on PATH (e.g. uv on a machine without it) is
// skipped rather than reported, so the surfaced error is the real install
// failure and not a misleading "executable not found". The returned error
// leads with the failure cause — the command follows — because the
// notification renderer truncates on width and the cause is what matters.
func runAdapterInstall(candidates [][]string) error {
	var lastErr error
	ran := false
	for _, argv := range candidates {
		if len(argv) == 0 {
			continue
		}
		if _, err := exec.LookPath(argv[0]); err != nil {
			continue // installer tool absent: skip, don't report it as the cause
		}
		ran = true
		ctx, cancel := context.WithTimeout(context.Background(), adapterInstallTimeout)
		out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
		cancel()
		if err == nil {
			return nil
		}
		cause := tailOf(string(out), 300)
		if cause == "" {
			cause = err.Error()
		}
		lastErr = fmt.Errorf("%s (%s)", cause, strings.Join(argv, " "))
	}
	if !ran {
		return errors.New("no installer available — need pip in the interpreter or uv on PATH")
	}
	return lastErr
}

// tailOf clips s to its last n bytes, single-line.
func tailOf(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		s = "…" + s[len(s)-n:]
	}
	return strings.ReplaceAll(s, "\n", " · ")
}

// launchDebug spawns the adapter and runs the DAP handshake asynchronously.
func (m *Model) launchDebug(root string, cfg run.Config) {
	m.stopDebugSession(false) // one session at a time (MVP)

	explicit := m.explicitInterpreter(cfg.Lang)
	argv, ok := lang.DebugAdapter(cfg.Lang, root, explicit)
	if !ok {
		m.host.Notify(host.Error, "debug: no adapter for "+cfg.Lang)
		m.dbgLaunching = false
		return
	}
	absFile := cfg.File
	if !filepath.IsAbs(absFile) {
		absFile = filepath.Join(root, absFile)
	}
	spec := lang.RunSpec{File: absFile, Module: cfg.Module, Args: cfg.Args}
	launchArgs, ok := lang.DebugLaunchArgs(cfg.Lang, root, spec, cfg.Dir(root), cfg.Env)
	if !ok {
		m.host.Notify(host.Error, "debug: no launch template for "+cfg.Lang)
		m.dbgLaunching = false
		return
	}

	send := m.host.Send
	sess, err := dap.Start(transport.Spec{Command: argv[0], Args: argv[1:], Dir: root, Detached: true}, func(ev dap.Event) {
		send(debugEventMsg{ev: ev})
	})
	if err != nil {
		m.host.Notify(host.Error, "debug: adapter failed to start: "+err.Error())
		m.dbgLaunching = false
		return
	}
	m.dbg = &debugState{sess: sess, cfgName: cfg.Name, root: root}
	m.dbgLaunching = false
	logDebugSessionStart(cfg.Name) // delimit consecutive sessions in the transcript (#637)
	// integratedTerminal (#625): debugpy asks the client to launch the debuggee
	// in a terminal it owns. Hand the reverse request to the Update loop.
	sess.OnRunInTerminal(func(seq int, args dap.RunInTerminalArgs) {
		send(debugRunInTerminalMsg{seq: seq, args: args})
	})
	m.host.Notify(host.Info, "debug: "+cfg.Name+" starting")
	go func() {
		if err := sess.Initialize(); err != nil {
			send(debugErrMsg{err: withAdapterStderr(err, sess)})
			return
		}
		if err := <-sess.LaunchAsync(launchArgs); err != nil {
			send(debugErrMsg{err: withAdapterStderr(err, sess)})
		}
	}()
}

// withAdapterStderr appends the adapter's captured stderr tail to a
// handshake error, so a dead adapter (missing module, wrong binary) is
// diagnosable from the notification alone (#589).
func withAdapterStderr(err error, sess *dap.Session) error {
	if tail := tailOf(sess.Stderr(), 200); tail != "" {
		return fmt.Errorf("%v — adapter: %s", err, tail)
	}
	return err
}

// handleDebugEvent routes one adapter event.
func (m *Model) handleDebugEvent(ev dap.Event) {
	dbg := m.dbg
	if dbg == nil {
		// Trailing output after the session finished (the adapter flushes the
		// debuggee's last writes past `terminated`) still reaches the
		// transcript, even though the panel is gone (#637).
		if ev.Name == "output" {
			if o := ev.Output(); o.Category != "telemetry" {
				logDebugOutput(o.Category == "stderr", o.Output)
			}
		}
		return
	}
	send := m.host.Send
	sess := dbg.sess
	switch ev.Name {
	case "initialized":
		// Configuration phase: push every stored breakpoint, then finish.
		files := m.bpts.All()
		root := dbg.root
		go func() {
			for file, lines := range files {
				abs := file
				if !filepath.IsAbs(abs) {
					abs = filepath.Join(root, abs)
				}
				if _, err := sess.SetBreakpoints(abs, lines); err != nil {
					send(debugErrMsg{err: err})
				}
			}
			if err := sess.ConfigurationDone(); err != nil {
				send(debugErrMsg{err: err})
			}
		}()
	case "stopped":
		st := ev.Stopped()
		go func() {
			threadID := st.ThreadID
			if threadID == 0 {
				if threads, err := sess.Threads(); err == nil && len(threads) > 0 {
					threadID = threads[0].ID
				}
			}
			frames, err := sess.StackTrace(threadID)
			if err != nil {
				send(debugErrMsg{err: err})
				return
			}
			send(debugStoppedMsg{threadID: threadID, frames: frames})
		}()
	case "continued":
		// A spontaneous resume (another client, a conditional breakpoint the
		// adapter continued past) blanks the panel like debugStep does, so no
		// stale rows stay visible — or editable — while running (#640).
		m.clearPausedMarker()
		dbg.paused = false
		if p := m.debugPanel(); p != nil {
			p.SetRunning()
		}
	case "output":
		o := ev.Output()
		// Adapter/telemetry categories aren't program output; skip them.
		if o.Category == "telemetry" {
			break
		}
		stderr := o.Category == "stderr"
		logDebugOutput(stderr, o.Output) // persist the transcript (#624)
		p := m.debugPanel()
		if p == nil && !dbg.panelOpened {
			// First output with the panel closed opens it (#637): a program
			// that never hits a breakpoint is otherwise invisible. Once per
			// session — a panel the user closes stays closed.
			m.openDebugPanel()
			p = m.debugPanel()
		}
		if p != nil {
			p.AppendOutput(stderr, o.Output)
		} else {
			dbg.pendingOut = appendPendingOut(dbg.pendingOut, debugOut{stderr: stderr, text: o.Output})
		}
	case "exited":
		x := ev.Exited()
		go func() { send(debugEndedMsg{exitCode: x.ExitCode, hasCode: true}) }()
	case "terminated":
		go func() { send(debugEndedMsg{}) }()
	}
}

// applyDebugStop records the stop context and returns the top frame to
// navigate to (nil when there is nothing to show).
func (m *Model) applyDebugStop(msg debugStoppedMsg) *dap.StackFrame {
	dbg := m.dbg
	if dbg == nil {
		return nil
	}
	dbg.threadID = msg.threadID
	dbg.paused = true
	dbg.frames = msg.frames
	if len(msg.frames) == 0 {
		return nil
	}
	top := msg.frames[0]
	if top.Source.Path == "" {
		return nil
	}
	return &top
}

// markPausedLine sets the gutter marker on every view of path.
func (m *Model) markPausedLine(path string, line int) {
	m.clearPausedMarker()
	for _, ed := range m.editorViewsForPath(path) {
		ed.SetPausedLine(line)
	}
	if m.dbg != nil {
		m.dbg.pausedPath = path
	}
}

// clearPausedMarker removes the marker from the file that carried it.
func (m *Model) clearPausedMarker() {
	if m.dbg == nil || m.dbg.pausedPath == "" {
		return
	}
	for _, ed := range m.editorViewsForPath(m.dbg.pausedPath) {
		ed.ClearPausedLine()
	}
	m.dbg.pausedPath = ""
}

// debugStep dispatches one stepping request while paused; kind is one of
// "over", "into", "out", "continue".
func (m *Model) debugStep(kind string) {
	dbg := m.dbg
	if dbg == nil {
		m.host.Notify(host.Info, "debug: no session — start one with debug.start")
		return
	}
	if !dbg.paused {
		m.host.Notify(host.Info, "debug: not paused")
		return
	}
	sess := dbg.sess
	var do func(threadID int) error
	switch kind {
	case "over":
		do = sess.Next
	case "into":
		do = sess.StepIn
	case "out":
		do = sess.StepOut
	default:
		do = sess.Continue
	}
	m.clearPausedMarker()
	dbg.paused = false
	if p := m.debugPanel(); p != nil {
		p.SetRunning()
	}
	send := m.host.Send
	threadID := dbg.threadID
	go func() {
		if err := do(threadID); err != nil {
			send(debugErrMsg{err: err})
		}
	}()
}

// debugPanel returns the singleton panel model, nil while it is not open.
func (m Model) debugPanel() *debugpanel.Model {
	if !m.panes.Has(pane.DebugKey) {
		return nil
	}
	return m.panes.Get(pane.DebugKey).Debug()
}

// debugPanelEditing reports whether the focused pane is the debug panel with an
// open inline value editor, so the app routes every key straight to it (#627).
func (m Model) debugPanelEditing() bool {
	inst := m.panes.FocusedInstance()
	return inst != nil && inst.Kind() == pane.KindDebug && inst.Debug().Editing()
}

// openDebugPanel splits the active editor (fallback: focused leaf) at the
// bottom with the singleton panel — without stealing focus; the stop already
// moved the caret to the paused line.
func (m *Model) openDebugPanel() {
	if m.panes.Has(pane.DebugKey) {
		// The panel already exists — restored from a saved layout, or left
		// open across stops. The session still attaches to it: the editable
		// gate and any buffered output must reach the panel too (#640).
		m.attachDebugPanel(m.panes.Get(pane.DebugKey).Debug())
		return
	}
	target := m.activeEditorKey()
	if target == "" {
		target = m.panes.Focused()
	}
	if target == "" || m.tree == nil {
		return
	}
	key := m.panes.AddDebug()
	tree, ok := layout.SplitLeaf(m.tree, target, key, layout.ZoneBottom)
	if !ok {
		m.panes.Close(key)
		return
	}
	m.tree = tree
	m.attachDebugPanel(m.panes.Get(key).Debug())
	m.layout()
	saveLayout(m.tree, m.panes)
}

// attachDebugPanel binds the live session to the panel: it gates the
// variable-edit affordance on the adapter's setVariable capability (#627 —
// the handshake has completed by the first stop) and flushes output captured
// before the panel existed (#624). Runs on every openDebugPanel, including
// when the panel pre-exists from a restored layout (#640).
func (m *Model) attachDebugPanel(p *debugpanel.Model) {
	if p == nil || m.dbg == nil {
		return
	}
	p.SetEditable(m.dbg.sess.SupportsSetVariable())
	for _, o := range m.dbg.pendingOut {
		p.AppendOutput(o.stderr, o.text)
	}
	m.dbg.pendingOut = nil
	m.dbg.panelOpened = true
}

// closeDebugPanel removes the panel when the session ends.
func (m *Model) closeDebugPanel() {
	if !m.panes.Has(pane.DebugKey) {
		return
	}
	if m.closeKey(pane.DebugKey) {
		if !m.panes.Has(m.panes.Focused()) {
			m.setFocus(m.focusAfterClose())
		}
		m.layout()
		saveLayout(m.tree, m.panes)
	}
}

// fetchScopes loads a frame's scopes plus the first scope's variables and
// feeds the panel via messages.
func (m *Model) fetchScopes(frameID int) {
	dbg := m.dbg
	if dbg == nil {
		return
	}
	sess := dbg.sess
	send := m.host.Send
	go func() {
		scopes, err := sess.Scopes(frameID)
		if err != nil {
			send(debugErrMsg{err: err})
			return
		}
		send(debugScopesMsg{scopes: scopes})
		if len(scopes) > 0 && scopes[0].VariablesReference > 0 {
			if vars, err := sess.Variables(scopes[0].VariablesReference); err == nil {
				send(debugVarsMsg{ref: scopes[0].VariablesReference, vars: vars})
			}
		}
	}()
}

// fetchVariables expands one variablesReference for the panel.
func (m *Model) fetchVariables(ref int) {
	dbg := m.dbg
	if dbg == nil {
		return
	}
	sess := dbg.sess
	send := m.host.Send
	go func() {
		vars, err := sess.Variables(ref)
		if err != nil {
			send(debugErrMsg{err: err})
			return
		}
		send(debugVarsMsg{ref: ref, vars: vars})
	}()
}

// runDebuggeeInTerminal answers a runInTerminal reverse request (#625): it
// spawns the debuggee command in a bottom-split terminal pane — giving it a
// real tty so input() works — and replies with the process id. The debuggee
// connects back to the adapter on its own; the pid is for process tracking.
func (m *Model) runDebuggeeInTerminal(msg debugRunInTerminalMsg) {
	dbg := m.dbg
	if dbg == nil {
		return
	}
	refuse := func(reason string) {
		go func() { _ = dbg.sess.RefuseReverse(msg.seq, "runInTerminal", reason) }()
	}
	if len(msg.args.Args) == 0 {
		refuse("no command")
		return
	}
	target := m.activeEditorKey()
	if target == "" {
		target = m.panes.Focused()
	}
	if target == "" || m.tree == nil {
		refuse("no pane to place the debuggee terminal")
		return
	}
	dir := msg.args.Cwd
	if dir == "" {
		dir = dbg.root
	}
	env := terminal.MergeEnv(terminalEnv(), envMapToSlice(msg.args.Env))
	key := m.panes.AddCommandTerminal(msg.args.Args, "debug: "+dbg.cfgName, dir, env, m.host.Send)
	tree, ok := layout.SplitLeaf(m.tree, target, key, layout.ZoneBottom)
	if !ok {
		m.panes.Close(key)
		refuse("layout split failed")
		return
	}
	m.tree = tree
	m.layout()
	saveLayout(m.tree, m.panes)
	dbg.termKey = key

	pid := 0
	if inst := m.panes.Get(key); inst != nil {
		pid = inst.Terminal().Pid()
	}
	if pid == 0 {
		refuse("debuggee failed to start")
		return
	}
	seq := msg.seq
	sess := dbg.sess
	go func() { _ = sess.RespondRunInTerminal(seq, pid) }()
}

// envMapToSlice converts a runInTerminal env map into "K=V" entries.
func envMapToSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

// setDebugVariable pushes an edited value to the adapter (setVariable) and, on
// success, refetches the containing reference so the panel shows the new value
// (#627). Refused while the debuggee runs — the DAP request is only valid
// paused, and the rows on screen would be stale (#640). A failure surfaces as
// a notification; the tree is left unchanged. A refetch failure after a
// successful set surfaces too: the panel would silently keep the old value.
func (m *Model) setDebugVariable(ref int, name, value string) {
	dbg := m.dbg
	if dbg == nil {
		return
	}
	if !dbg.paused {
		m.host.Notify(host.Info, "debug: not paused — cannot set variables")
		return
	}
	sess := dbg.sess
	send := m.host.Send
	go func() {
		if _, err := sess.SetVariable(ref, name, value); err != nil {
			send(debugErrMsg{err: err})
			return
		}
		if vars, err := sess.Variables(ref); err != nil {
			send(debugErrMsg{err: fmt.Errorf("value set, refresh failed: %w", err)})
		} else {
			send(debugVarsMsg{ref: ref, vars: vars})
		}
	}()
}

// stopDebugSession ends the live session; notify controls the toast (a
// restart stays quiet).
func (m *Model) stopDebugSession(notify bool) {
	dbg := m.dbg
	if dbg == nil {
		if m.dbgLaunching {
			// A stop during the install/handshake window cancels the pending
			// launch (#636): the generation bump makes the deferred
			// post-install retry a no-op when its result arrives.
			m.dbgLaunching = false
			m.dbgLaunchGen++
			if notify {
				m.host.Notify(host.Info, "debug: launch cancelled")
			}
		}
		return
	}
	m.clearPausedMarker()
	m.dbg = nil
	m.dbgLaunching = false
	m.closeDebugPanel()
	sess := dbg.sess
	go func() {
		_ = sess.Disconnect()
		sess.Close()
	}()
	if notify {
		m.host.Notify(host.Info, "debug: session stopped")
	}
}

// finishDebugSession handles adapter-reported termination.
func (m *Model) finishDebugSession(msg debugEndedMsg) {
	dbg := m.dbg
	if dbg == nil {
		return
	}
	m.clearPausedMarker()
	m.dbg = nil
	m.dbgLaunching = false
	m.closeDebugPanel()
	go dbg.sess.Close()
	note := "debug: " + dbg.cfgName + " finished"
	if msg.hasCode {
		note += " (exit code " + strconv.Itoa(msg.exitCode) + ")"
	}
	m.host.Notify(host.Info, note)
}
