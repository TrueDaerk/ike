package app

import (
	"path/filepath"
	"sort"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/dap"
	"ike/internal/debug"
	"ike/internal/host"
	"ike/internal/ui"
)

// runtocursor.go is "run to cursor" (#2405): instead of pressing F8 eight
// times to reach the interesting line, the debuggee resumes and stops there.
// The mechanism is one temporary breakpoint that never enters the persisted
// store — it lives on the session state, merges into the file's breakpoint
// list on the wire (DAP setBreakpoints replaces a file's whole list, so the
// temporary line has to travel with the real ones), and is dropped again on
// the next stop. A user breakpoint on the same line is therefore untouchable
// by definition: the store never learns about the temporary one.
//
// debug.runToCursor takes the focused editor's cursor line; debug.runToLine
// asks for a line number in the shell prompt, for the palette.

// tempBreakpoint is the live run-to-cursor breakpoint. key is the store key
// (project-relative, like every breakpoint), abs the path pushed to the
// adapter, line the 0-based buffer line.
type tempBreakpoint struct {
	key  string
	abs  string
	line int
}

// runToLinePromptHeading titles the shell prompt of debug.runToLine.
const runToLinePromptHeading = "Run to line"

// tempBreakpointAt returns the live session's temporary breakpoint, nil when
// there is none (or no session).
func (m Model) tempBreakpointAt() *tempBreakpoint {
	if m.dbg == nil {
		return nil
	}
	return m.dbg.tempBP
}

// sessionSpecs returns the breakpoints an adapter should hold for the store
// key: the enabled persisted ones plus the run-to-cursor line while one is
// pending (#2405). The temporary line carries no refinements — it stops
// unconditionally, once.
func (m Model) sessionSpecs(key string) []debug.Spec {
	specs := m.bpts.EnabledSpecs(key)
	t := m.tempBreakpointAt()
	if t == nil || t.key != key {
		return specs
	}
	for _, sp := range specs {
		if sp.Line == t.line {
			return specs // a user breakpoint already covers the line
		}
	}
	specs = append(specs, debug.Spec{Line: t.line})
	sort.Slice(specs, func(i, j int) bool { return specs[i].Line < specs[j].Line })
	return specs
}

// runToCursor is the debug.runToCursor handler: the focused editor's file at
// the cursor line.
func (m *Model) runToCursor() {
	ed := m.focusedEditor()
	if ed == nil {
		ed = m.activeEditor()
	}
	if ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, "run to cursor: needs an open file")
		return
	}
	line, _ := ed.CursorPos()
	m.runToBufferLine(ed.Path(), line)
}

// runToBufferLine resumes the debuggee towards path:line (0-based): the
// temporary breakpoint is installed, the file's merged list goes to the
// adapter and only then does the continue follow — both in one goroutine, so
// the resume can never overtake the breakpoint it depends on.
func (m *Model) runToBufferLine(path string, line int) {
	dbg := m.dbg
	if dbg == nil || dbg.sess == nil {
		m.host.Notify(host.Info, "run to cursor: no debug session")
		return
	}
	if !dbg.paused {
		m.host.Notify(host.Info, "run to cursor: the debuggee is running — pause it first")
		return
	}
	if line < 0 {
		m.host.Notify(host.Info, "run to cursor: no such line")
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	key := bpKey(path)
	// The previous run-to-cursor's file (if any) has to be re-pushed too when
	// the new target sits elsewhere: its temporary line leaves the adapter.
	var stale string
	if prev := dbg.tempBP; prev != nil && prev.abs != abs {
		stale = prev.abs
	}
	if m.bpts.Enabled(key, line) {
		// An enabled user breakpoint already stops here: nothing temporary is
		// needed, and nothing must be cleaned up afterwards.
		dbg.tempBP = nil
	} else {
		dbg.tempBP = &tempBreakpoint{key: key, abs: abs, line: line}
	}
	files := []string{abs}
	if stale != "" {
		files = append(files, stale)
	}
	m.host.Notify(host.Info, "run to "+displayPath(abs)+":"+strconv.Itoa(line+1))
	m.resumeAfterBreakpoints(files)
}

// resumeAfterBreakpoints pushes the given files' merged breakpoint lists and
// continues the debuggee, in that order on one goroutine. The UI side of the
// resume (marker, panel, paused flag) mirrors debugStep.
func (m *Model) resumeAfterBreakpoints(files []string) {
	dbg := m.dbg
	if dbg == nil {
		return
	}
	type push struct {
		abs string
		bps []dap.SourceBreakpoint
	}
	pushes := make([]push, 0, len(files))
	for _, abs := range files {
		pushes = append(pushes, push{abs: abs, bps: dapBreakpoints(m.sessionSpecs(bpKey(abs)))})
	}
	m.clearPausedMarker()
	dbg.paused = false
	if p := m.debugPanel(); p != nil {
		p.SetRunning()
	}
	sess, threadID, send := dbg.sess, dbg.threadID, m.host.Send
	go func() {
		for _, p := range pushes {
			if _, err := sess.SetBreakpoints(p.abs, p.bps); err != nil {
				send(debugErrMsg{err: err})
				return
			}
		}
		if err := sess.Continue(threadID); err != nil {
			send(debugErrMsg{err: err})
		}
	}()
}

// clearTempBreakpoint drops the run-to-cursor breakpoint and re-pushes its
// file's real list, so the temporary line leaves the adapter as well. Called
// on every stop — the run-to-cursor line was reached, or another breakpoint
// won the race, and either way the temporary one has done its job. Reports
// whether one was pending.
func (m *Model) clearTempBreakpoint() bool {
	dbg := m.dbg
	if dbg == nil || dbg.tempBP == nil {
		return false
	}
	abs := dbg.tempBP.abs
	dbg.tempBP = nil
	m.syncSessionBreakpoints(abs)
	return true
}

// runToLinePromptOpen reports whether the shell shows the line prompt.
func (m Model) runToLinePromptOpen() bool { return m.runToLineOpen && m.shell.IsOpen() }

// startRunToLine opens debug.runToLine's prompt — the palette flavour of
// run-to-cursor, for a line the caret is not on. The session preconditions
// are checked before it opens, so the number is never typed for nothing.
func (m *Model) startRunToLine() {
	dbg := m.dbg
	if dbg == nil || dbg.sess == nil {
		m.host.Notify(host.Info, "run to line: no debug session")
		return
	}
	if !dbg.paused {
		m.host.Notify(host.Info, "run to line: the debuggee is running — pause it first")
		return
	}
	if ed := m.activeEditor(); ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, "run to line: needs an open file")
		return
	}
	m.runToLineOpen = true
	m.runToLineInput = ""
	m.runToLinePos = 0
	m.renderRunToLinePrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// renderRunToLinePrompt (re)fills the shell for the current input.
func (m *Model) renderRunToLinePrompt() {
	avail := m.width - 10
	if avail < 20 {
		avail = 20
	}
	line := "line: " + windowedInput(m.runToLineInput, m.runToLinePos, avail)
	m.shell.SetContent(ui.ModelContent{
		Heading: runToLinePromptHeading,
		Body: func() string {
			return line + "\n\nenter run · esc cancel"
		},
	})
}

// updateRunToLinePrompt consumes every key while the prompt is open, like the
// other single-field shell prompts.
func (m Model) updateRunToLinePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	closePrompt := func() {
		m.runToLineOpen = false
		m.runToLineInput = ""
		m.runToLinePos = 0
		m.shell.Close()
	}
	switch {
	case msg.Code == tea.KeyEscape:
		closePrompt()
		return m, nil
	case msg.Code == tea.KeyEnter:
		text := m.runToLineInput
		closePrompt()
		ed := m.activeEditor()
		if ed == nil || !ed.HasFile() {
			m.host.Notify(host.Info, "run to line: needs an open file")
			return m, nil
		}
		n, err := strconv.Atoi(text)
		if err != nil || n < 1 {
			m.host.Notify(host.Info, "run to line: not a line number: "+text)
			return m, nil
		}
		m.runToBufferLine(ed.Path(), n-1)
		return m, nil
	default:
		if out, pos, handled, _ := ui.EditKey(msg, m.runToLineInput, m.runToLinePos); handled {
			m.runToLineInput, m.runToLinePos = out, pos
		}
	}
	m.renderRunToLinePrompt()
	return m, nil
}

// pasteRunToLinePrompt inserts a paste into the line input at its cursor,
// like every other single-field prompt (#1936).
func (m *Model) pasteRunToLinePrompt(text string) bool {
	out, pos, changed := ui.PasteText(m.runToLineInput, m.runToLinePos, flattenExpr(text))
	if !changed {
		return false
	}
	m.runToLineInput, m.runToLinePos = out, pos
	m.renderRunToLinePrompt()
	return true
}
