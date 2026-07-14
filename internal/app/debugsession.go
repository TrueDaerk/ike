package app

import (
	"path/filepath"
	"strconv"
	"strings"

	"ike/internal/dap"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/lsp/transport"
	"ike/internal/run"
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

	// output collects the debuggee's DAP output events; the debug tool
	// window (#580) renders it.
	output strings.Builder
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
	store.Touch(cfg.Name)
	_ = run.Save(store)
	m.launchDebug(root, *cfg)
}

// launchDebug spawns the adapter and runs the DAP handshake asynchronously.
func (m *Model) launchDebug(root string, cfg run.Config) {
	m.stopDebugSession(false) // one session at a time (MVP)

	explicit := m.explicitInterpreter(cfg.Lang)
	argv, ok := lang.DebugAdapter(cfg.Lang, root, explicit)
	if !ok {
		m.host.Notify(host.Error, "debug: no adapter for "+cfg.Lang)
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
		return
	}

	send := m.host.Send
	sess, err := dap.Start(transport.Spec{Command: argv[0], Args: argv[1:], Dir: root}, func(ev dap.Event) {
		send(debugEventMsg{ev: ev})
	})
	if err != nil {
		m.host.Notify(host.Error, "debug: adapter failed to start: "+err.Error())
		return
	}
	m.dbg = &debugState{sess: sess, cfgName: cfg.Name, root: root}
	m.host.Notify(host.Info, "debug: "+cfg.Name+" starting")
	go func() {
		if err := sess.Initialize(); err != nil {
			send(debugErrMsg{err: err})
			return
		}
		if err := <-sess.LaunchAsync(launchArgs); err != nil {
			send(debugErrMsg{err: err})
		}
	}()
}

// handleDebugEvent routes one adapter event.
func (m *Model) handleDebugEvent(ev dap.Event) {
	dbg := m.dbg
	if dbg == nil {
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
		m.clearPausedMarker()
		dbg.paused = false
	case "output":
		dbg.output.WriteString(ev.Output().Output)
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
	send := m.host.Send
	threadID := dbg.threadID
	go func() {
		if err := do(threadID); err != nil {
			send(debugErrMsg{err: err})
		}
	}()
}

// stopDebugSession ends the live session; notify controls the toast (a
// restart stays quiet).
func (m *Model) stopDebugSession(notify bool) {
	dbg := m.dbg
	if dbg == nil {
		return
	}
	m.clearPausedMarker()
	m.dbg = nil
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
	go dbg.sess.Close()
	note := "debug: " + dbg.cfgName + " finished"
	if msg.hasCode {
		note += " (exit code " + strconv.Itoa(msg.exitCode) + ")"
	}
	m.host.Notify(host.Info, note)
}
