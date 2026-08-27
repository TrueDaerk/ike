package app

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/dap"
	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/ui"
)

// debugeval.go is the evaluate popup (#2174): `debug.evaluate` (alt+f8, the
// JetBrains chord) runs the editor's visual selection — or, without one, an
// expression typed into a shell prompt — through DAP `evaluate` on the frame
// the debugger is paused in, and shows the answer in a cursor-anchored popup
// (editor/evalpopup.go) whose structured children page in through `variables`
// requests.
//
// Everything here needs a paused session and an adapter that implements
// evaluate; both are checked before the prompt opens, so the expression is
// never typed for nothing.

// Messages carrying the async evaluate work back into Update.
type (
	// debugEvalMsg is one finished evaluate; sess guards against a goroutine
	// outliving the session (#1523) like debugStoppedMsg.
	debugEvalMsg struct {
		sess *dap.Session
		expr string
		res  dap.EvaluateResult
		err  error
	}
	// debugEvalVarsMsg carries one variablesReference's children for the open
	// popup. It is deliberately not debugVarsMsg: the panel's tree and the
	// popup's tree expand independently, and a shared message would cross
	// them.
	debugEvalVarsMsg struct {
		sess *dap.Session
		ref  int
		vars []dap.Variable
	}
)

// evalPromptHeading titles the shell prompt.
const evalPromptHeading = "Evaluate expression"

// startDebugEvaluate opens the evaluate flow. A selection evaluates straight
// away; otherwise the prompt asks for an expression.
func (m *Model) startDebugEvaluate() {
	if !m.evalReady() {
		return
	}
	if ed := m.focusedEditor(); ed != nil {
		if text, ok := ed.SelectionText(); ok {
			if expr := flattenExpr(text); expr != "" {
				m.evaluateExpr(expr, m.evalContext(true))
				return
			}
		}
	}
	m.evalOpen = true
	m.evalInput = ""
	m.evalPos = 0
	m.renderEvalPrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// evalReady reports whether an evaluate can run right now, notifying why it
// cannot: no session, a running debuggee, or an adapter without evaluate
// (#2174 — the capability gate; the notice replaces a request that would only
// come back as an error).
func (m *Model) evalReady() bool {
	dbg := m.dbg
	if dbg == nil || dbg.sess == nil {
		m.host.Notify(host.Info, "evaluate: no debug session")
		return false
	}
	if !dbg.paused {
		m.host.Notify(host.Info, "evaluate: the debuggee is running — pause it first")
		return false
	}
	if !dbg.sess.SupportsEvaluate() {
		dbg.evalNoticed = true
		m.host.Notify(host.Warn, "evaluate: this debug adapter does not support evaluate")
		return false
	}
	return true
}

// evalContext picks the DAP evaluate context hint: "hover" for a selection
// when the adapter advertised supportsEvaluateForHovers (it promises a
// side-effect-free evaluation there), "repl" otherwise — the context every
// adapter understands.
func (m Model) evalContext(fromSelection bool) string {
	if fromSelection && m.dbg != nil && m.dbg.sess != nil && m.dbg.sess.SupportsEvaluateForHovers() {
		return "hover"
	}
	return "repl"
}

// flattenExpr turns a (possibly multi-line) selection into one expression:
// lines are joined with spaces and the whitespace collapses, so selecting a
// wrapped call still evaluates.
func flattenExpr(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// evaluateExpr sends the request off the UI goroutine; the answer arrives as
// a debugEvalMsg.
func (m *Model) evaluateExpr(expr, context string) {
	dbg := m.dbg
	if dbg == nil {
		return
	}
	sess, frameID := dbg.sess, dbg.curFrameID
	send := m.host.Send
	go func() {
		res, err := sess.Evaluate(expr, frameID, context)
		send(debugEvalMsg{sess: sess, expr: expr, res: res, err: err})
	}()
}

// applyEvalResult opens (or refuses to open) the popup for a finished
// evaluate. A stale session's answer is dropped (#1523); an adapter that
// refused evaluate outright disables the feature with one notice instead of
// showing the raw protocol error.
func (m *Model) applyEvalResult(msg debugEvalMsg) {
	if m.dbg == nil || msg.sess != m.dbg.sess {
		return
	}
	if msg.err != nil {
		if errors.Is(msg.err, dap.ErrEvaluateUnsupported) {
			if !m.dbg.evalNoticed {
				m.dbg.evalNoticed = true
				m.host.Notify(host.Warn, "evaluate: this debug adapter does not support evaluate")
			}
			m.pushWatches() // the watches section picks up the notice too
			return
		}
		m.host.Notify(host.Warn, "evaluate: "+msg.err.Error())
		return
	}
	ed := m.focusedEditor()
	if ed == nil {
		return
	}
	ed.OpenEvalResult(msg.expr, editor.EvalVar{
		Name:  msg.expr,
		Value: msg.res.Result,
		Type:  msg.res.Type,
		Ref:   msg.res.VariablesReference,
	})
}

// fetchEvalChildren answers the popup's expand intent: one variables request
// off the UI goroutine, back as a debugEvalVarsMsg.
func (m *Model) fetchEvalChildren(ref int) {
	dbg := m.dbg
	if dbg == nil || dbg.sess == nil || ref == 0 {
		return
	}
	sess := dbg.sess
	send := m.host.Send
	go func() {
		vars, err := sess.Variables(ref)
		if err != nil {
			return // a failed expansion leaves the row collapsed, not broken
		}
		send(debugEvalVarsMsg{sess: sess, ref: ref, vars: vars})
	}()
}

// applyEvalChildren pushes fetched children into the open popup.
func (m *Model) applyEvalChildren(msg debugEvalVarsMsg) {
	if m.dbg == nil || msg.sess != m.dbg.sess {
		return
	}
	ed := m.focusedEditor()
	if ed == nil || !ed.EvalOpen() {
		return
	}
	vars := make([]editor.EvalVar, 0, len(msg.vars))
	for _, v := range msg.vars {
		vars = append(vars, editor.EvalVar{
			Name: v.Name, Value: v.Value, Type: v.Type, Ref: v.VariablesReference,
		})
	}
	ed.SetEvalChildren(msg.ref, vars)
}

// dismissEvalPopups closes the evaluate popup wherever it is open — the
// result describes a paused frame and must not outlive it (resume, step,
// session end), the same rule the paused marker and the inline values follow.
func (m *Model) dismissEvalPopups() {
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			ed.DismissEval()
		}
	}
}

// evalPromptOpen reports whether the shell shows the expression prompt.
func (m Model) evalPromptOpen() bool { return m.evalOpen && m.shell.IsOpen() }

// renderEvalPrompt (re)fills the shell for the current input.
func (m *Model) renderEvalPrompt() {
	avail := m.width - 10
	if avail < 20 {
		avail = 20
	}
	line := "> " + windowedInput(m.evalInput, m.evalPos, avail)
	m.shell.SetContent(ui.ModelContent{
		Heading: evalPromptHeading,
		Body: func() string {
			return line + "\n\nenter evaluate · esc cancel"
		},
	})
}

// updateEvalPrompt consumes every key while the prompt is open, like the
// other single-field shell prompts.
func (m Model) updateEvalPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	closePrompt := func() {
		m.evalOpen = false
		m.evalInput = ""
		m.evalPos = 0
		m.shell.Close()
	}
	switch {
	case msg.Code == tea.KeyEscape:
		closePrompt()
		return m, nil
	case msg.Code == tea.KeyEnter:
		expr := flattenExpr(m.evalInput)
		closePrompt()
		if expr == "" {
			return m, nil
		}
		if !m.evalReady() {
			return m, nil
		}
		m.evaluateExpr(expr, m.evalContext(false))
		return m, nil
	default:
		if out, pos, handled, _ := ui.EditKey(msg, m.evalInput, m.evalPos); handled {
			m.evalInput, m.evalPos = out, pos
		}
	}
	m.renderEvalPrompt()
	return m, nil
}

// pasteEvalPrompt inserts a paste into the expression input at its cursor;
// the field is one line, so the block flattens like every other single-field
// prompt (#1936).
func (m *Model) pasteEvalPrompt(text string) bool {
	out, pos, changed := ui.PasteText(m.evalInput, m.evalPos, flattenExpr(text))
	if !changed {
		return false
	}
	m.evalInput, m.evalPos = out, pos
	m.renderEvalPrompt()
	return true
}
