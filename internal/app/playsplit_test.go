package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/layout"
	"ike/internal/pane"
)

// playsplit_test.go covers the playground's detached result pane (#2797):
// the split into a sibling pane, the focus round trip across the two panes,
// the mouse mapping of both, re-attaching when the pane closes, removal when
// the playground closes, the result actions in the detached state, the
// layout persistence that never records the pane, and the park/resume across
// a project switch.

// splitJQ opens the jq playground over body, runs program and splits the
// result out, asserting the split landed.
func splitJQ(t *testing.T, body, program string) Model {
	t.Helper()
	m := openJQ(t, dismissOnboarding(playApp(t, body)))
	m = setProgram(m, program)
	tm, cmd := m.Update(SplitPlayResultMsg{})
	m = drainCmd(tm.(Model), cmd)
	if m.play == nil || m.play.resultKey == "" {
		t.Fatal("playground.splitResult must detach the result into a pane")
	}
	return m
}

// TestPlaygroundSplitResultDetaches is the acceptance case: the result moves
// into a sibling pane to the right of the source, which keeps the query
// header pinned over its own document.
func TestPlaygroundSplitResultDetaches(t *testing.T) {
	m := splitJQ(t, `{"foo":[1,2,3]}`, ".foo[]")
	s := m.play
	ws := m.activeWS()
	inst := ws.Panes.Get(s.resultKey)
	if inst == nil || inst.Kind() != pane.KindPlayResult {
		t.Fatalf("the result pane must be a KindPlayResult instance, got %v", inst)
	}
	if !layout.Panes(ws.Tree)[s.resultKey] {
		t.Fatal("the result pane must be a leaf of the workspace tree")
	}
	src, ok := m.lay.Panes[s.paneKey]
	if !ok {
		t.Fatal("the source pane must be laid out")
	}
	res, ok := m.lay.Panes[s.resultKey]
	if !ok {
		t.Fatal("the result pane must be laid out")
	}
	if res.X <= src.X {
		t.Errorf("the result pane (x=%d) must sit to the right of the source (x=%d)", res.X, src.X)
	}
	if got := ws.Panes.Focused(); got != s.paneKey {
		t.Errorf("focus = %q, want the source pane's query line to keep it", got)
	}
	if s.bufFocus {
		t.Error("the keyboard must stay on the query line")
	}
	// The source pane: the query header over the document itself.
	srcView := ansi.Strip(m.renderPane(s.paneKey, src))
	if !strings.Contains(srcView, "JQ:") || !strings.Contains(srcView, ".foo[]") {
		t.Errorf("the source pane must keep the query header, got:\n%s", srcView)
	}
	if !strings.Contains(srcView, `"foo"`) {
		t.Errorf("the source pane must show its document under the header, got:\n%s", srcView)
	}
	if strings.Contains(srcView, "JQ RESULT") {
		t.Error("the source pane must not carry the result pane's title")
	}
	// The result pane: the title and the values.
	resView := ansi.Strip(m.renderPane(s.resultKey, res))
	if !strings.Contains(resView, "JQ RESULT") {
		t.Errorf("the result pane must be titled as the result, got:\n%s", resView)
	}
	for _, want := range []string{"1", "2", "3"} {
		if !strings.Contains(resView, want) {
			t.Errorf("the result pane must show %q, got:\n%s", want, resView)
		}
	}
	// The header rows are the source pane's chrome (mouse translation), the
	// document is sized under them, and the result buffer fills its pane.
	if rows := m.playHeaderRowsFor(s.paneKey); rows != m.playQueryRowCount()+playInfoRows {
		t.Errorf("header rows = %d, want query + info rows", rows)
	}
	if m.playHeaderRowsFor(s.resultKey) != 0 {
		t.Error("the result pane has no query header")
	}
	if got, want := s.resultEd.Height(), paneInterior(res.H, paneChromeH); got != want {
		t.Errorf("result buffer height = %d, want the result pane's interior %d", got, want)
	}
	if got := m.render(); !strings.Contains(got, "JQ RESULT") {
		t.Errorf("the whole frame must show the result pane, got:\n%s", got)
	}
}

// TestPlaygroundSplitFocusRoundTrip: tab from the query line focuses the
// result pane, tab from there the source pane again — and a plain pane focus
// move into the result pane is the same as a tab into the result.
func TestPlaygroundSplitFocusRoundTrip(t *testing.T) {
	m := splitJQ(t, `{"foo":[1,2,3]}`, ".foo[]")
	s := m.play
	ws := m.activeWS()

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if got := ws.Panes.Focused(); got != s.resultKey {
		t.Fatalf("after tab focus = %q, want the result pane %q", got, s.resultKey)
	}
	if !s.bufFocus {
		t.Fatal("tab must hand the keyboard to the result buffer")
	}
	if !m.playFocused() {
		t.Fatal("the focused result pane is the playground's")
	}
	// Keys route into the result buffer: G goes to the last line.
	m = drainKey(m, tea.KeyPressMsg{Code: 'G', Text: "G"})
	if line, _ := s.resultEd.Cursor(); line != 3 {
		t.Errorf("G in the result pane put the cursor on line %d, want 3", line)
	}

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if got := ws.Panes.Focused(); got != s.paneKey {
		t.Fatalf("after the second tab focus = %q, want the source pane %q", got, s.paneKey)
	}
	if s.bufFocus {
		t.Fatal("tab from the result pane must return the keyboard to the query line")
	}
	// Typing lands on the query line again.
	m = typeInto(m, "1")
	m = dismissJQPopup(m)
	if !strings.HasSuffix(s.program.Text, "1") {
		t.Errorf("program = %q, want the typed rune on the query line", s.program.Text)
	}

	// A focus move by the workspace's own machinery (ctrl+arrows, the
	// switcher) into the result pane is a tab into the result.
	m.setFocus(s.resultKey)
	if !s.bufFocus {
		t.Error("focusing the result pane must put the keyboard in the result buffer")
	}
	m.setFocus(s.paneKey)
	if s.bufFocus {
		t.Error("focusing the source pane must put the keyboard on the query line")
	}
	// And a bufFocus the mode moved on its own drags the pane focus after
	// it on the settled pass.
	s.setBufFocus(true)
	m = drainKey(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := ws.Panes.Focused(); got != s.resultKey {
		t.Errorf("focus = %q after the mode moved the keyboard into the result, want %q", got, s.resultKey)
	}
}

// TestPlaygroundSplitToggleReattaches: the command is a toggle — the second
// run puts the result back under the header and removes the pane.
func TestPlaygroundSplitToggleReattaches(t *testing.T) {
	m := splitJQ(t, `{"foo":[1,2,3]}`, ".foo[]")
	s := m.play
	key := s.resultKey
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab}) // keyboard in the result pane
	tm, cmd := m.Update(SplitPlayResultMsg{})
	m = drainCmd(tm.(Model), cmd)
	ws := m.activeWS()
	if s.resultKey != "" {
		t.Fatal("the second split must re-attach the result")
	}
	if ws.Panes.Has(key) || layout.Panes(ws.Tree)[key] {
		t.Error("the result pane must leave the registry and the tree")
	}
	if got := ws.Panes.Focused(); got != s.paneKey {
		t.Errorf("focus = %q, want the source pane, where the result now is", got)
	}
	if !m.playInlineActive(s.paneKey) {
		t.Fatal("the playground must render inline again")
	}
	r := m.lay.Panes[s.paneKey]
	view := ansi.Strip(m.renderPane(s.paneKey, r))
	if !strings.Contains(view, ".foo[]") || !strings.Contains(view, "3") {
		t.Errorf("the inline pane must show the header and the result again, got:\n%s", view)
	}
	if got, want := s.resultEd.Height(), paneInterior(r.H, paneChromeH+m.playHeaderRowsFor(s.paneKey)); got != want {
		t.Errorf("result buffer height = %d, want the interior under the header %d", got, want)
	}
}

// TestPlaygroundSplitClosePaneReattaches: closing the result pane with the
// pane's own close (pane.close) re-attaches the result inline; the
// playground stays open.
func TestPlaygroundSplitClosePaneReattaches(t *testing.T) {
	m := splitJQ(t, `{"foo":[1,2,3]}`, ".foo[]")
	s := m.play
	key := s.resultKey
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab}) // focus the result pane
	tm, cmd := m.Update(ClosePaneMsg{})
	m = drainCmd(tm.(Model), cmd)
	if m.play != s {
		t.Fatal("closing the result pane must not close the playground")
	}
	if s.resultKey != "" {
		t.Fatal("closing the result pane must re-attach the result")
	}
	ws := m.activeWS()
	if ws.Panes.Has(key) || layout.Panes(ws.Tree)[key] {
		t.Error("the closed pane must be gone")
	}
	if got := ws.Panes.Focused(); got != s.paneKey {
		t.Errorf("focus = %q, want the source pane", got)
	}
	if view := ansi.Strip(m.renderPane(s.paneKey, m.lay.Panes[s.paneKey])); !strings.Contains(view, "3") {
		t.Errorf("the result must render inline again, got:\n%s", view)
	}
}

// TestPlaygroundSplitCloseRemovesPane: esc closes the playground and takes
// the result pane with it — from either pane.
func TestPlaygroundSplitCloseRemovesPane(t *testing.T) {
	for _, fromResult := range []bool{false, true} {
		m := splitJQ(t, `{"foo":[1,2,3]}`, ".foo[]")
		s := m.play
		key := s.resultKey
		if fromResult {
			m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
		}
		m = closeJQ(m)
		if m.play != nil {
			t.Fatalf("fromResult=%v: esc must close the playground", fromResult)
		}
		ws := m.activeWS()
		if ws.Panes.Has(key) || layout.Panes(ws.Tree)[key] {
			t.Errorf("fromResult=%v: the result pane must go with the playground", fromResult)
		}
		if got := ws.Panes.Focused(); got != s.paneKey {
			t.Errorf("fromResult=%v: focus = %q, want the source pane", fromResult, got)
		}
	}
}

// TestPlaygroundSplitSourceCloseRemovesPane: the mode dies with its source
// pane (#1980), and so does the detached result pane.
func TestPlaygroundSplitSourceCloseRemovesPane(t *testing.T) {
	m := splitJQ(t, `{"foo":[1,2,3]}`, ".foo[]")
	s := m.play
	key := s.resultKey
	m.closePane(s.paneKey)
	tm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // a pass, for the settled hooks
	m = drainCmd(tm.(Model), cmd)
	if m.play != nil {
		t.Fatal("closing the source pane must close the playground")
	}
	ws := m.activeWS()
	if ws.Panes.Has(key) || layout.Panes(ws.Tree)[key] {
		t.Error("the result pane must not outlive the playground")
	}
}

// TestPlaygroundSplitMouse: the result pane maps clicks onto the result
// buffer and takes the keyboard; the source pane's header returns it to the
// query line, and the rows under the header are the document's.
func TestPlaygroundSplitMouse(t *testing.T) {
	m := splitJQ(t, `{"foo":[1,2,3]}`, "range(60)")
	s := m.play
	ws := m.activeWS()
	res := m.lay.Panes[s.resultKey]
	src := m.lay.Panes[s.paneKey]

	// A click on the result pane's third row.
	tm, cmd := m.Update(tea.MouseClickMsg{X: res.X + paneContentX, Y: res.Y + paneContentY + 2, Button: tea.MouseLeft})
	m = drainCmd(tm.(Model), cmd)
	if got := ws.Panes.Focused(); got != s.resultKey {
		t.Fatalf("a click into the result pane must focus it, got %q", got)
	}
	if !s.bufFocus {
		t.Fatal("a click into the result pane must put the keyboard in the result buffer")
	}
	if line, _ := s.resultEd.Cursor(); line != 3 {
		t.Errorf("the click put the result cursor on line %d, want 3", line)
	}

	// The wheel over the result pane scrolls the result.
	tm, cmd = m.Update(tea.MouseWheelMsg{X: res.X + paneContentX, Y: res.Y + paneContentY + 2, Button: tea.MouseWheelDown})
	m = drainCmd(tm.(Model), cmd)
	if s.resultEd.ScrollTop() == 0 {
		t.Error("the wheel over the result pane must scroll the result buffer")
	}

	// A click on the source pane's query row returns the keyboard to the
	// query line.
	tm, cmd = m.Update(tea.MouseClickMsg{X: src.X + paneContentX + playQueryPrefixW + 2, Y: src.Y + paneContentY, Button: tea.MouseLeft})
	m = drainCmd(tm.(Model), cmd)
	if got := ws.Panes.Focused(); got != s.paneKey {
		t.Fatalf("a click on the query header must focus the source pane, got %q", got)
	}
	if s.bufFocus {
		t.Fatal("a click on the query header must return the keyboard to the query line")
	}
	if s.program.Cur != 2 {
		t.Errorf("the header click put the caret at %d, want 2", s.program.Cur)
	}

	// A click under the header lands in the document: the source editor's
	// caret moves to the clicked row.
	rows := m.playHeaderRowsFor(s.paneKey)
	ed := ws.Panes.Get(s.paneKey).Editor()
	tm, cmd = m.Update(tea.MouseClickMsg{X: src.X + paneContentX + 6, Y: src.Y + paneContentY + rows, Button: tea.MouseLeft})
	m = drainCmd(tm.(Model), cmd)
	if line, _ := ed.Cursor(); line != 1 {
		t.Errorf("a click on the first document row put the document caret on line %d, want 1", line)
	}
	if got := ws.Panes.Focused(); got != s.paneKey {
		t.Errorf("focus = %q, want the source pane", got)
	}
}

// TestPlaygroundSplitResultActions: the result actions keep working with the
// result in its own pane — the find round trip crosses panes, copy, the
// table view and chaining act on the same state.
func TestPlaygroundSplitResultActions(t *testing.T) {
	m := splitJQ(t, `[{"a":1,"b":"x"},{"a":2,"b":"y"}]`, ".[]")
	s := m.play
	ws := m.activeWS()

	// The find chord from the query line moves into the result pane, esc
	// brings the keyboard back to the source pane.
	m = drainKey(m, playFindKey())
	if got := ws.Panes.Focused(); got != s.resultKey || !s.bufFocus || !s.findQuery {
		t.Fatalf("the find chord must open the search in the result pane (focus %q, buf %v, findQuery %v)", got, s.bufFocus, s.findQuery)
	}
	m = typeInto(m, "y")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := ws.Panes.Focused(); got != s.paneKey || s.bufFocus {
		t.Fatalf("esc must hand the keyboard back to the query line on the source pane (focus %q, buf %v)", got, s.bufFocus)
	}
	if m.play == nil {
		t.Fatal("esc out of the search must not close the playground")
	}

	// ctrl+y copies the whole result from the query line.
	m = drainKey(m, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if !strings.Contains(s.status, "copied") {
		t.Errorf("ctrl+y status = %q, want a copy confirmation", s.status)
	}

	// The table view draws in the result pane.
	tm, cmd := m.Update(TogglePlayTableMsg{})
	m = drainCmd(tm.(Model), cmd)
	if s.table == nil {
		t.Fatal("the table view must open in the detached state")
	}
	if view := ansi.Strip(m.renderPane(s.resultKey, m.lay.Panes[s.resultKey])); !strings.Contains(view, "a") || !strings.Contains(view, "x") {
		t.Errorf("the result pane must draw the table, got:\n%s", view)
	}
	tm, cmd = m.Update(TogglePlayTableMsg{})
	m = drainCmd(tm.(Model), cmd)

	// Chaining keeps the pane and shows the chained result there.
	tm, cmd = m.Update(ChainPlayResultMsg{})
	m = drainCmd(tm.(Model), cmd)
	if len(s.chain) != 1 || s.resultKey == "" {
		t.Fatalf("chaining must keep the detached pane (chain %d, key %q)", len(s.chain), s.resultKey)
	}
	m = setProgram(m, ".a")
	if view := ansi.Strip(m.renderPane(s.resultKey, m.lay.Panes[s.resultKey])); !strings.Contains(view, "1") || !strings.Contains(view, "2") {
		t.Errorf("the chained result must render in the result pane, got:\n%s", view)
	}
}

// TestPlaygroundSplitNotPersisted: the result pane is session state — the
// saved layout and a named layout snapshot leave its leaf out while the live
// tree keeps it.
func TestPlaygroundSplitNotPersisted(t *testing.T) {
	m := splitJQ(t, `{"foo":[1,2,3]}`, ".foo[]")
	s := m.play
	ws := m.activeWS()
	data, ok := encodeLayoutState(ws.Tree, ws.Panes)
	if !ok {
		t.Fatal("the layout must still encode with the result pane open")
	}
	if strings.Contains(string(data), s.resultKey) {
		t.Errorf("the saved layout must not record the result pane, got:\n%s", data)
	}
	snap, ok := snapshotLayout(ws.Tree, ws.Panes)
	if !ok {
		t.Fatal("a named layout snapshot must succeed with the result pane open")
	}
	if _, has := snap.Panes[s.resultKey]; has || strings.Contains(string(snap.Tree), s.resultKey) {
		t.Error("a named layout must not record the result pane")
	}
	if !layout.Panes(ws.Tree)[s.resultKey] {
		t.Error("pruning for persistence must not touch the live tree")
	}
}

// TestPlaygroundSplitWithoutPlayground: the command without a playground is
// a notification, not a pane.
func TestPlaygroundSplitWithoutPlayground(t *testing.T) {
	m := playApp(t, `{"foo":1}`)
	before := len(m.activeWS().Panes.Keys())
	tm, cmd := m.Update(SplitPlayResultMsg{})
	m = drainCmd(tm.(Model), cmd)
	if got := len(m.activeWS().Panes.Keys()); got != before {
		t.Errorf("panes = %d after the split without a playground, want %d", got, before)
	}
}

// TestPlaygroundSplitSurvivesProjectSwitch: the detached layout parks with
// the workspace and comes back on resume — pane, key and rendering.
func TestPlaygroundSplitSurvivesProjectSwitch(t *testing.T) {
	m, b := playSwitchModel(t, `{"foo":[1,2,3]}`, ".foo[]")
	a := cwd(t)
	tm, cmd := m.Update(SplitPlayResultMsg{})
	m = drainCmd(tm.(Model), cmd)
	if m.play == nil || m.play.resultKey == "" {
		t.Fatal("setup: the split must detach the result")
	}
	key := m.play.resultKey

	m = switchTo(t, m, b)
	if m.playOpen() {
		t.Fatal("project b must not show project a's playground")
	}
	if m.activeWS().Panes.Has(key) {
		t.Fatal("project b must not show project a's result pane")
	}

	m = switchTo(t, m, a)
	s := m.play
	if s == nil {
		t.Fatal("switching back must bring the playground back")
	}
	if s.resultKey != key {
		t.Fatalf("resumed result pane = %q, want %q", s.resultKey, key)
	}
	ws := m.activeWS()
	if !ws.Panes.Has(key) || !layout.Panes(ws.Tree)[key] {
		t.Fatal("the result pane must resume with the workspace")
	}
	r, ok := m.lay.Panes[key]
	if !ok {
		t.Fatal("the resumed result pane must be laid out")
	}
	if got, want := s.resultEd.Height(), paneInterior(r.H, paneChromeH); got != want {
		t.Errorf("resumed result buffer height = %d, want %d", got, want)
	}
	if v := m.render(); !strings.Contains(v, "JQ RESULT") || !strings.Contains(v, "3") {
		t.Errorf("the resumed detached playground does not render, got:\n%s", v)
	}
	// And the focus round trip still crosses the panes (the resumed model may
	// raise this machine's onboarding dialog again, which eats keys).
	m = dismissOnboarding(m)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if got := ws.Panes.Focused(); got != key {
		t.Errorf("after resume tab focus = %q, want the result pane", got)
	}
}
