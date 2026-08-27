package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/dap"
	"ike/internal/debug"
	"ike/internal/debugpanel"
	"ike/internal/editor"
)

// debugeval_test.go covers watch persistence, stop re-evaluation, the
// evaluate popup and the adapter capability gate (#2174), all against the
// scripted stub adapter of debugsession_test.go.

// pausedAt drives one stop at path:line so the model is paused with a frame
// to evaluate against.
func pausedAt(t *testing.T, m Model, path string, line int) Model {
	t.Helper()
	frames := []dap.StackFrame{
		{ID: 11, Name: "inner", Source: dap.Source{Path: path}, Line: line, Column: 1},
	}
	tm, _ := m.Update(debugStoppedMsg{sess: m.dbg.sess, threadID: 1, frames: frames})
	m = tm.(Model)
	if m.dbg == nil || !m.dbg.paused {
		t.Fatal("the stop must leave the model paused")
	}
	return m
}

// notices returns the notification texts recorded so far, newest first —
// Update already drained the host queue into the history ring.
func notices(m Model) []string {
	var out []string
	for _, n := range m.history {
		out = append(out, n.text)
	}
	for _, n := range m.host.DrainNotifications() {
		out = append(out, n.Text)
	}
	return out
}

// clearNotices empties the history ring, so a later assertion sees only what
// happened after it.
func clearNotices(m *Model) {
	m.history = nil
	m.host.DrainNotifications()
}

func containsSubstr(list []string, want string) bool {
	for _, s := range list {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}

// waitForEvaluates blocks until the stub saw at least n evaluate requests.
func waitForEvaluates(t *testing.T, sa *stubAdapter, n int) []json.RawMessage {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := sa.evaluates(); len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("adapter saw %d evaluate requests, want %d", len(sa.evaluates()), n)
	return nil
}

// TestWatchLifecyclePersists: add, edit and remove through the panel's
// messages mutate the per-project store and hit the disk each time (#2174).
func TestWatchLifecyclePersists(t *testing.T) {
	m, _, _ := debugModel(t)

	tm, _ := m.Update(debugpanel.AddWatchMsg{Expr: "x + 1"})
	m = tm.(Model)
	tm, _ = m.Update(debugpanel.AddWatchMsg{Expr: "obj.field"})
	m = tm.(Model)
	if got := m.watchExprs(); len(got) != 2 || got[0] != "x + 1" || got[1] != "obj.field" {
		t.Fatalf("watch list = %v", got)
	}
	if got := debug.LoadWatches().List(); len(got) != 2 || got[1] != "obj.field" {
		t.Fatalf("watches not persisted, reloaded %v", got)
	}

	tm, _ = m.Update(debugpanel.EditWatchMsg{Index: 0, Expr: "x + 2"})
	m = tm.(Model)
	if got := debug.LoadWatches().List(); got[0] != "x + 2" {
		t.Fatalf("edit not persisted, reloaded %v", got)
	}

	// An emptied expression removes the row, like the panel's inline editor
	// promises.
	tm, _ = m.Update(debugpanel.EditWatchMsg{Index: 0, Expr: "  "})
	m = tm.(Model)
	if got := m.watchExprs(); len(got) != 1 || got[0] != "obj.field" {
		t.Fatalf("emptied edit left %v", got)
	}

	tm, _ = m.Update(debugpanel.RemoveWatchMsg{Index: 0})
	m = tm.(Model)
	if m.watchExprs() != nil && len(m.watchExprs()) != 0 {
		t.Fatalf("remove left %v", m.watchExprs())
	}
	if got := debug.LoadWatches().List(); len(got) != 0 {
		t.Fatalf("removal not persisted, reloaded %v", got)
	}

	// Out-of-range mutations are no-ops, not panics.
	tm, _ = m.Update(debugpanel.RemoveWatchMsg{Index: 7})
	m = tm.(Model)
	tm, _ = m.Update(debugpanel.EditWatchMsg{Index: -1, Expr: "nope"})
	_ = tm.(Model)
}

// TestWatchesReEvaluateOnStop: every stop re-runs the whole list against the
// stopped frame with the DAP "watch" context (#1914/#2174).
func TestWatchesReEvaluateOnStop(t *testing.T) {
	m, sa, path := debugModel(t)
	tm, _ := m.Update(debugpanel.AddWatchMsg{Expr: "x"})
	m = tm.(Model)
	tm, _ = m.Update(debugpanel.AddWatchMsg{Expr: "y"})
	m = tm.(Model)

	m = pausedAt(t, m, path, 3)
	args := waitForEvaluates(t, sa, 2)

	seen := map[string]bool{}
	for _, raw := range args[:2] {
		var a struct {
			Expression string `json:"expression"`
			FrameID    int    `json:"frameId"`
			Context    string `json:"context"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			t.Fatal(err)
		}
		if a.Context != "watch" {
			t.Fatalf("watch evaluated with context %q", a.Context)
		}
		if a.FrameID != 11 {
			t.Fatalf("watch evaluated against frame %d, want the stopped frame 11", a.FrameID)
		}
		seen[a.Expression] = true
	}
	if !seen["x"] || !seen["y"] {
		t.Fatalf("evaluated expressions = %v", seen)
	}

	// A second stop re-evaluates rather than reusing the first round.
	m = pausedAt(t, m, path, 4)
	waitForEvaluates(t, sa, 4)
}

// TestWatchResultsRenderAndSurviveErrors: a failing expression shows its
// error in place of a value and never hides the others (#2174).
func TestWatchResultsRenderAndSurviveErrors(t *testing.T) {
	m, _, path := debugModel(t)
	tm, _ := m.Update(debugpanel.AddWatchMsg{Expr: "ok"})
	m = tm.(Model)
	tm, _ = m.Update(debugpanel.AddWatchMsg{Expr: "boom"})
	m = tm.(Model)
	m = pausedAt(t, m, path, 3)

	tm, _ = m.Update(debugWatchesMsg{
		sess: m.dbg.sess,
		results: []debugpanel.WatchResult{
			{Expr: "ok", Value: "42", Type: "int"},
			{Expr: "boom", Err: "undefined"},
		},
	})
	m = tm.(Model)
	p := m.debugPanel()
	if p == nil {
		t.Fatal("the stop must have opened the debug panel")
	}
	v := p.View()
	if !strings.Contains(v, "ok = 42") || !strings.Contains(v, "boom = ⚠ undefined") {
		t.Fatalf("panel must show value and error side by side:\n%s", v)
	}
	if m.dbg == nil || !m.dbg.paused {
		t.Fatal("a failing watch must not break the session")
	}
}

// TestWatchResultsFromAStaleSessionAreDropped guards the #1523 rule.
func TestWatchResultsFromAStaleSessionAreDropped(t *testing.T) {
	m, _, path := debugModel(t)
	tm, _ := m.Update(debugpanel.AddWatchMsg{Expr: "x"})
	m = tm.(Model)
	m = pausedAt(t, m, path, 3)

	other := dap.NewSession(nil)
	tm, _ = m.Update(debugWatchesMsg{
		sess:    other,
		results: []debugpanel.WatchResult{{Expr: "x", Value: "stale"}},
	})
	m = tm.(Model)
	if v := m.debugPanel().View(); strings.Contains(v, "stale") {
		t.Fatalf("a stale session's results must be dropped:\n%s", v)
	}
}

// TestEvaluateCapabilityGate is the acceptance criterion for an adapter
// without evaluate: the watches section says so, the feature disables itself,
// no request storm and no error spam (#2174).
func TestEvaluateCapabilityGate(t *testing.T) {
	m, sa, path := debugModel(t)
	sa.mu.Lock()
	sa.evalRefuse = "unsupported request: evaluate"
	sa.mu.Unlock()

	tm, _ := m.Update(debugpanel.AddWatchMsg{Expr: "x"})
	m = tm.(Model)
	m = pausedAt(t, m, path, 3)
	waitForEvaluates(t, sa, 1)

	// The session latches the verdict off the evaluation goroutine.
	deadline := time.Now().Add(3 * time.Second)
	for m.dbg.sess.SupportsEvaluate() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if m.dbg.sess.SupportsEvaluate() {
		t.Fatal("the refusal must latch the capability off")
	}
	if m.evaluateSupported() {
		t.Fatal("the model must mirror the latched capability")
	}

	// The round that discovered it degrades to the bare list with a notice.
	tm, _ = m.Update(debugWatchesMsg{
		sess:        m.dbg.sess,
		results:     []debugpanel.WatchResult{{Expr: "x", Err: "unsupported"}},
		unsupported: true,
	})
	m = tm.(Model)
	v := m.debugPanel().View()
	if !strings.Contains(v, "evaluate unsupported") {
		t.Fatalf("the section must carry the capability notice:\n%s", v)
	}
	if !strings.Contains(v, "x") {
		t.Fatalf("the expression must stay listed:\n%s", v)
	}
	if strings.Contains(v, "⚠") {
		t.Fatalf("no protocol errors may leak into the rows:\n%s", v)
	}
	if got := notices(m); !containsSubstr(got, "does not support evaluate") {
		t.Fatalf("notifications = %v", got)
	}

	// Later stops no longer spend requests on it.
	before := len(sa.evaluates())
	m = pausedAt(t, m, path, 4)
	time.Sleep(50 * time.Millisecond)
	if after := len(sa.evaluates()); after != before {
		t.Fatalf("evaluate requests grew from %d to %d after the capability was off", before, after)
	}
	// And the notice is not repeated per stop.
	clearNotices(&m)
	tm, _ = m.Update(debugWatchesMsg{sess: m.dbg.sess, unsupported: true})
	m = tm.(Model)
	if got := notices(m); containsSubstr(got, "does not support evaluate") {
		t.Fatalf("the notice must be shown once per session, got %v", got)
	}
}

// TestEvaluateCommandGatedWithoutCapability: the popup command refuses with a
// clear message instead of opening a prompt for a request that cannot work.
func TestEvaluateCommandGatedWithoutCapability(t *testing.T) {
	m, sa, path := debugModel(t)
	sa.mu.Lock()
	sa.evalRefuse = "unsupported request: evaluate"
	sa.mu.Unlock()
	m = pausedAt(t, m, path, 3)
	if _, err := m.dbg.sess.Evaluate("x", 11, "repl"); err == nil {
		t.Fatal("the scripted adapter must refuse evaluate")
	}

	tm, _ := m.Update(DebugEvaluateMsg{})
	m = tm.(Model)
	if m.evalPromptOpen() {
		t.Fatal("an adapter without evaluate must not open the prompt")
	}
	if got := notices(m); !containsSubstr(got, "does not support evaluate") {
		t.Fatalf("notifications = %v", got)
	}
}

// TestEvaluateNeedsPausedSession: without a session, or while the debuggee
// runs, the command explains itself instead of failing silently.
func TestEvaluateNeedsPausedSession(t *testing.T) {
	m, _, _ := debugModel(t)
	m.dbg = nil
	tm, _ := m.Update(DebugEvaluateMsg{})
	m = tm.(Model)
	if got := notices(m); !containsSubstr(got, "no debug session") {
		t.Fatalf("notifications = %v", got)
	}

	m, _, _ = debugModel(t)
	clearNotices(&m)
	tm, _ = m.Update(DebugEvaluateMsg{}) // session installed, never stopped
	m = tm.(Model)
	if m.evalPromptOpen() {
		t.Fatal("a running debuggee must not open the prompt")
	}
	if got := notices(m); !containsSubstr(got, "pause it first") {
		t.Fatalf("notifications = %v", got)
	}
}

// TestEvaluateSelectionSendsRequest: a visual selection evaluates straight
// away, without the prompt, on the paused frame.
func TestEvaluateSelectionSendsRequest(t *testing.T) {
	m, sa, path := debugModel(t)
	m = pausedAt(t, m, path, 1)
	m = drainKey(m, tea.KeyPressMsg{Code: 'v', Text: "v"})
	ed := m.focusedEditor()
	if ed == nil {
		t.Fatal("no focused editor")
	}
	if _, ok := ed.SelectionText(); !ok {
		t.Fatal("the editor must be in visual mode")
	}

	tm, _ := m.Update(DebugEvaluateMsg{})
	m = tm.(Model)
	if m.evalPromptOpen() {
		t.Fatal("a selection must evaluate without the prompt")
	}
	args := waitForEvaluates(t, sa, 1)
	var a struct {
		Expression string `json:"expression"`
		FrameID    int    `json:"frameId"`
		Context    string `json:"context"`
	}
	if err := json.Unmarshal(args[0], &a); err != nil {
		t.Fatal(err)
	}
	if a.Expression == "" {
		t.Fatal("the selection must become the expression")
	}
	if a.FrameID != 11 {
		t.Fatalf("evaluated against frame %d, want the paused frame", a.FrameID)
	}
	if a.Context != "repl" {
		// The stub advertises no supportsEvaluateForHovers, so the fallback
		// context is the one every adapter understands.
		t.Fatalf("context = %q, want repl", a.Context)
	}
}

// TestEvaluatePromptWithoutSelection: with nothing selected the prompt opens,
// and enter sends the typed expression.
func TestEvaluatePromptWithoutSelection(t *testing.T) {
	m, sa, path := debugModel(t)
	m = pausedAt(t, m, path, 1)

	tm, _ := m.Update(DebugEvaluateMsg{})
	m = tm.(Model)
	if !m.evalPromptOpen() {
		t.Fatal("without a selection the prompt must open")
	}
	for _, r := range "len(s)" {
		tm, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = tm.(Model)
	}
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)
	if m.evalPromptOpen() {
		t.Fatal("enter must close the prompt")
	}
	args := waitForEvaluates(t, sa, 1)
	var a struct {
		Expression string `json:"expression"`
	}
	if err := json.Unmarshal(args[0], &a); err != nil {
		t.Fatal(err)
	}
	if a.Expression != "len(s)" {
		t.Fatalf("evaluated %q, want the typed expression", a.Expression)
	}
}

// TestEvaluatePromptEscapes: esc closes the prompt without a request.
func TestEvaluatePromptEscapes(t *testing.T) {
	m, sa, path := debugModel(t)
	m = pausedAt(t, m, path, 1)
	tm, _ := m.Update(DebugEvaluateMsg{})
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.evalPromptOpen() {
		t.Fatal("esc must close the prompt")
	}
	time.Sleep(30 * time.Millisecond)
	if got := sa.evaluates(); len(got) != 0 {
		t.Fatalf("a cancelled prompt must send nothing, saw %d requests", len(got))
	}
}

// TestEvaluateResultOpensPopupAndExpands: the answer opens the popup, and
// expanding a structured result runs a variables request whose rows land in
// the popup.
func TestEvaluateResultOpensPopupAndExpands(t *testing.T) {
	m, sa, path := debugModel(t)
	sa.mu.Lock()
	sa.varsBody = map[string]any{"variables": []map[string]any{
		{"name": "host", "value": `"localhost"`, "type": "string"},
	}}
	sa.mu.Unlock()
	m = pausedAt(t, m, path, 1)

	tm, _ := m.Update(debugEvalMsg{
		sess: m.dbg.sess,
		expr: "cfg",
		res:  dap.EvaluateResult{Result: "Config{…}", Type: "Config", VariablesReference: 900},
	})
	m = tm.(Model)
	ed := m.focusedEditor()
	if ed == nil || !ed.EvalOpen() {
		t.Fatal("the result must open the evaluate popup")
	}
	if v := ed.EvalView(); !strings.Contains(v, "eval: cfg") || !strings.Contains(v, "Config{…}") {
		t.Fatalf("popup must show the expression and result:\n%s", v)
	}

	// The expand intent reaches the adapter and comes back into the popup.
	tm, _ = m.Update(editor.EvalExpandMsg{Ref: 900})
	m = tm.(Model)
	waitForCommand(t, sa, "variables")
	tm, _ = m.Update(debugEvalVarsMsg{
		sess: m.dbg.sess,
		ref:  900,
		vars: []dap.Variable{{Name: "host", Value: `"localhost"`, Type: "string"}},
	})
	m = tm.(Model)
	if v := m.focusedEditor().EvalView(); !strings.Contains(v, "host") {
		t.Fatalf("fetched children must render:\n%s", v)
	}
}

// TestEvaluateErrorNotifiesWithoutPopup: a bad expression is a notice, not a
// popup — and never disables the feature.
func TestEvaluateErrorNotifiesWithoutPopup(t *testing.T) {
	m, _, path := debugModel(t)
	m = pausedAt(t, m, path, 1)
	tm, _ := m.Update(debugEvalMsg{
		sess: m.dbg.sess,
		expr: "nope",
		err:  errFake("could not find symbol nope"),
	})
	m = tm.(Model)
	if ed := m.focusedEditor(); ed != nil && ed.EvalOpen() {
		t.Fatal("a failed evaluate must not open a popup")
	}
	if got := notices(m); !containsSubstr(got, "could not find symbol nope") {
		t.Fatalf("notifications = %v", got)
	}
	if !m.evaluateSupported() {
		t.Fatal("a bad expression must not disable the feature")
	}
}

// TestEvaluatePopupClosesOnResume: the result describes the frame just left,
// so continuing (which clears the paused marker) closes it too.
func TestEvaluatePopupClosesOnResume(t *testing.T) {
	m, _, path := debugModel(t)
	m = pausedAt(t, m, path, 1)
	tm, _ := m.Update(debugEvalMsg{
		sess: m.dbg.sess, expr: "x", res: dap.EvaluateResult{Result: "42"},
	})
	m = tm.(Model)
	if !m.focusedEditor().EvalOpen() {
		t.Fatal("popup must be open")
	}
	m.clearPausedMarker()
	if m.focusedEditor().EvalOpen() {
		t.Fatal("resuming must close the evaluate popup")
	}
}

// errFake is a minimal error for the notification tests.
type errFake string

func (e errFake) Error() string { return string(e) }
