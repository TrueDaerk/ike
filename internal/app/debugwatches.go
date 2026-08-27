package app

import (
	"ike/internal/dap"
	"ike/internal/debug"
	"ike/internal/debugpanel"
	"ike/internal/editor"
	"ike/internal/host"

	tea "charm.land/bubbletea/v2"
)

// debugwatches.go wires watch expressions and inline variable values (#1914).
// The expression list lives on the root model in a per-project store
// (debug.Watches, .ike/watches.json — #2174), so it survives debug sessions
// and restarts alike, and re-evaluates against the current frame on every
// stop (DAP evaluate, context "watch"). Inline values ride the same stop: the
// selected frame's Locals render as line-end annotations in that frame's file
// (editor.SetDebugLocals), cleared whenever the paused marker clears.

// debugWatchesMsg carries evaluated watch results; sess guards against a
// goroutine outliving the session like debugStoppedMsg (#1523). unsupported
// reports that the adapter refused evaluate outright (#2174) — the values are
// meaningless then, and the section says so instead of showing errors.
type debugWatchesMsg struct {
	sess        *dap.Session
	results     []debugpanel.WatchResult
	unsupported bool
}

// debugLocalsMsg carries the selected frame's Locals for the inline values;
// path is the frame's source file, sess guards like debugWatchesMsg.
type debugLocalsMsg struct {
	sess *dap.Session
	path string
	vars []dap.Variable
}

// watchStore returns the watch-expression store, materializing it for models
// built without NewWith (zero-value test models).
func (m *Model) watchStore() *debug.Watches {
	if m.watches == nil {
		m.watches = debug.NewWatches()
	}
	return m.watches
}

// watchExprs returns the watched expressions in order.
func (m *Model) watchExprs() []string { return m.watchStore().List() }

// saveWatches persists the store, surfacing a failure as a warning like
// saveBreakpoints does — never fatal, the list still works this session.
func (m *Model) saveWatches() {
	if err := m.watchStore().Save(); err != nil {
		m.host.Notify(host.Warn, "watches not saved: "+err.Error())
	}
}

// evaluateSupported reports whether the live session can evaluate (#2174);
// without a session there is nothing to gate, so it reads true.
func (m Model) evaluateSupported() bool {
	if m.dbg == nil || m.dbg.sess == nil {
		return true
	}
	return m.dbg.sess.SupportsEvaluate()
}

// pushWatches feeds the panel the expression list without values — the shape
// while nothing is paused (a fresh add, a running debuggee, panel attach) and
// the shape an adapter without evaluate never leaves (#2174).
func (m *Model) pushWatches() {
	p := m.debugPanel()
	if p == nil {
		return
	}
	p.SetEvaluateSupported(m.evaluateSupported())
	exprs := m.watchExprs()
	results := make([]debugpanel.WatchResult, len(exprs))
	for i, e := range exprs {
		results[i] = debugpanel.WatchResult{Expr: e}
	}
	p.SetWatches(results)
}

// refreshWatches re-evaluates against the current frame while paused, and
// falls back to the pending shape otherwise.
func (m *Model) refreshWatches() {
	if dbg := m.dbg; dbg != nil && dbg.paused && m.evaluateSupported() {
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
	exprs := m.watchExprs()
	if dbg == nil || len(exprs) == 0 {
		m.pushWatches()
		return
	}
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
		// The first round is also where an adapter without evaluate is
		// discovered (#2174): the session latches the verdict, so ask it
		// rather than pattern-matching the per-row errors.
		send(debugWatchesMsg{sess: sess, results: results, unsupported: !sess.SupportsEvaluate()})
	}()
}

// handleWatchMsg applies one panel watch mutation; handled reports whether
// msg was one of them. The list mutates here (the panel is a pure consumer),
// then the panel refreshes — evaluated immediately when paused.
func (m *Model) handleWatchMsg(msg tea.Msg) bool {
	w := m.watchStore()
	var changed bool
	switch msg := msg.(type) {
	case debugpanel.AddWatchMsg:
		changed = w.Add(msg.Expr)
	case debugpanel.EditWatchMsg:
		changed = w.Replace(msg.Index, msg.Expr)
	case debugpanel.RemoveWatchMsg:
		changed = w.Remove(msg.Index)
	default:
		return false
	}
	if changed {
		m.saveWatches()
	}
	m.refreshWatches()
	return true
}

// applyWatchResults installs one evaluated round in the panel (#1914). An
// adapter that refused evaluate falls back to the bare expression list with
// the section's notice, and says so once per session (#2174).
func (m *Model) applyWatchResults(msg debugWatchesMsg) {
	if m.dbg == nil || msg.sess != m.dbg.sess {
		return // a stale session's results (#1523)
	}
	if msg.unsupported {
		if !m.dbg.evalNoticed {
			m.dbg.evalNoticed = true
			m.host.Notify(host.Info, "watches: this debug adapter does not support evaluate")
		}
		m.pushWatches()
		return
	}
	if p := m.debugPanel(); p != nil {
		p.SetEvaluateSupported(true)
		p.SetWatches(msg.results)
	}
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
