package app

import (
	"ike/internal/dap"
	"ike/internal/debugpanel"
	"ike/internal/editor"

	tea "charm.land/bubbletea/v2"
)

// debugwatches.go wires watch expressions and inline variable values (#1914).
// The expression list lives on the root model — in memory, surviving debug
// sessions — and re-evaluates against the current frame on every stop (DAP
// evaluate, context "watch"). Inline values ride the same stop: the selected
// frame's Locals render as line-end annotations in that frame's file
// (editor.SetDebugLocals), cleared whenever the paused marker clears.

// debugWatchesMsg carries evaluated watch results; sess guards against a
// goroutine outliving the session like debugStoppedMsg (#1523).
type debugWatchesMsg struct {
	sess    *dap.Session
	results []debugpanel.WatchResult
}

// debugLocalsMsg carries the selected frame's Locals for the inline values;
// path is the frame's source file, sess guards like debugWatchesMsg.
type debugLocalsMsg struct {
	sess *dap.Session
	path string
	vars []dap.Variable
}

// pushWatches feeds the panel the expression list without values — the shape
// while nothing is paused (a fresh add, a running debuggee, panel attach).
func (m *Model) pushWatches() {
	p := m.debugPanel()
	if p == nil {
		return
	}
	results := make([]debugpanel.WatchResult, len(m.watchExprs))
	for i, e := range m.watchExprs {
		results[i] = debugpanel.WatchResult{Expr: e}
	}
	p.SetWatches(results)
}

// refreshWatches re-evaluates against the current frame while paused, and
// falls back to the pending shape otherwise.
func (m *Model) refreshWatches() {
	if dbg := m.dbg; dbg != nil && dbg.paused {
		m.evaluateWatches(dbg.curFrameID)
		return
	}
	m.pushWatches()
}

// evaluateWatches evaluates every expression against frameID asynchronously;
// results land as one debugWatchesMsg. A failed expression carries its error
// in place of a value — one bad watch never hides the others.
func (m *Model) evaluateWatches(frameID int) {
	dbg := m.dbg
	if dbg == nil || len(m.watchExprs) == 0 {
		m.pushWatches()
		return
	}
	exprs := append([]string(nil), m.watchExprs...)
	sess := dbg.sess
	send := m.host.Send
	go func() {
		results := make([]debugpanel.WatchResult, len(exprs))
		for i, e := range exprs {
			res, err := sess.Evaluate(e, frameID, "watch")
			if err != nil {
				results[i] = debugpanel.WatchResult{Expr: e, Err: err.Error()}
				continue
			}
			results[i] = debugpanel.WatchResult{
				Expr: e, Value: res.Result, Type: res.Type, Ref: res.VariablesReference,
			}
		}
		send(debugWatchesMsg{sess: sess, results: results})
	}()
}

// handleWatchMsg applies one panel watch mutation; handled reports whether
// msg was one of them. The list mutates here (the panel is a pure consumer),
// then the panel refreshes — evaluated immediately when paused.
func (m *Model) handleWatchMsg(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case debugpanel.AddWatchMsg:
		m.watchExprs = append(m.watchExprs, msg.Expr)
	case debugpanel.EditWatchMsg:
		if msg.Index < 0 || msg.Index >= len(m.watchExprs) {
			return true
		}
		m.watchExprs[msg.Index] = msg.Expr
	case debugpanel.RemoveWatchMsg:
		if msg.Index < 0 || msg.Index >= len(m.watchExprs) {
			return true
		}
		m.watchExprs = append(m.watchExprs[:msg.Index], m.watchExprs[msg.Index+1:]...)
	default:
		return false
	}
	m.refreshWatches()
	return true
}

// inlineValuesEnabled reads debug.inline_values (default on).
func (m Model) inlineValuesEnabled() bool {
	v, ok := m.host.Config().Get("debug.inline_values")
	return !ok || v != "false"
}

// applyInlineValues renders the fetched Locals as line-end annotations in the
// frame's file (#1914); a frame in another file moves the annotations there.
func (m *Model) applyInlineValues(msg debugLocalsMsg) {
	dbg := m.dbg
	if dbg == nil || (msg.sess != nil && msg.sess != dbg.sess) {
		return
	}
	if !m.inlineValuesEnabled() || msg.path == "" {
		return
	}
	path := canonicalPath(msg.path)
	if dbg.inlinePath != "" && dbg.inlinePath != path {
		for _, ed := range m.editorViewsForPath(dbg.inlinePath) {
			ed.SetDebugLocals(nil)
		}
	}
	locals := make([]editor.DebugLocal, 0, len(msg.vars))
	for _, v := range msg.vars {
		locals = append(locals, editor.DebugLocal{Name: v.Name, Value: v.Value})
	}
	for _, ed := range m.editorViewsForPath(path) {
		ed.SetDebugLocals(locals)
	}
	dbg.inlinePath = path
}

// clearInlineValues removes the annotations from the file carrying them —
// the resume/step/session-end symmetry to applyInlineValues.
func (m *Model) clearInlineValues() {
	if m.dbg == nil || m.dbg.inlinePath == "" {
		return
	}
	for _, ed := range m.editorViewsForPath(m.dbg.inlinePath) {
		ed.SetDebugLocals(nil)
	}
	m.dbg.inlinePath = ""
}
