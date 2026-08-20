package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/jqplay"
	"ike/internal/lang"
	"ike/internal/pane"
	"ike/internal/scratch"
)

// jqplayground_test.go covers the jq playground dialog (#1936): live
// evaluation over a JSON buffer and over an HTTP response body, inline
// errors, the debounce/generation cancellation, the result actions and the
// session program history.

// noDebounce makes evaluation fire on the next tick instead of after the
// human-scale delay, so a test drives the dialog without sleeping.
func noDebounce(t *testing.T) {
	t.Helper()
	prev := jqDebounce
	jqDebounce = 0
	t.Cleanup(func() { jqDebounce = prev })
}

// jqApp opens body as a .json file in the focused editor and returns the
// model, which is the "open JSON buffer" the playground is written for.
func jqApp(t *testing.T, body string) Model {
	t.Helper()
	noDebounce(t)
	m := newSized()
	path := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.openPath(path, false)
	return drainCmd(tm.(Model), cmd)
}

// openJQ opens the playground through the real command message.
func openJQ(t *testing.T, m Model) Model {
	t.Helper()
	tm, cmd := m.Update(OpenJQPlaygroundMsg{})
	m = drainCmd(tm.(Model), cmd)
	if !m.jqPlayOpen() {
		t.Fatal("json.jqPlayground must open the playground")
	}
	return m
}

// setProgram replaces the query line and evaluates, the way enter does.
func setProgram(m Model, program string) Model {
	m.jqPlay.program = program
	m.jqPlay.pos = len([]rune(program))
	return drainCmd(m, m.runJQNow())
}

// TestJQPlaygroundEvaluatesLive is the issue's acceptance case: a select
// program against an open JSON buffer shows the matching values.
func TestJQPlaygroundEvaluatesLive(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"foo":[{"bar":1},{"bar":4},{"bar":9}]}`))
	m = setProgram(m, ".foo[] | select(.bar > 3)")

	s := m.jqPlay
	if s.result.Err != "" {
		t.Fatalf("valid program reported %q", s.result.Err)
	}
	if len(s.result.Outputs) != 2 {
		t.Fatalf("got %d outputs, want 2: %v", len(s.result.Outputs), s.result.Outputs)
	}
	v := ansi.Strip(m.render())
	if !strings.Contains(v, "2 value(s)") {
		t.Errorf("the dialog should summarise the value count, got:\n%s", v)
	}
	if !strings.Contains(v, `"bar": 4`) || !strings.Contains(v, `"bar": 9`) {
		t.Errorf("the result window should show the matching values, got:\n%s", v)
	}
	if !strings.Contains(v, "data.json") {
		t.Errorf("the input line should name the queried buffer, got:\n%s", v)
	}
}

// TestJQPlaygroundTypingReEvaluates: the query line is live — typing runs the
// program without an explicit enter.
func TestJQPlaygroundTypingReEvaluates(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"name":"ike"}`))
	m.jqPlay.program, m.jqPlay.pos = "", 0
	m = typeInto(m, ".name")
	if got := m.jqPlay.result.Text(); got != `"ike"` {
		t.Fatalf("live result = %q, want the field value", got)
	}
}

// TestJQPlaygroundSeedsFromCaretPath: the caret's jq path (#1660) prefills
// the query line, so "the value I was looking at" is already a program.
func TestJQPlaygroundSeedsFromCaretPath(t *testing.T) {
	noDebounce(t)
	// The seed reads the caret's document path, which needs the buffer's
	// language id — and internal/app does not pull in the language plugins
	// the shipped binary registers. A bare entry under a test-only extension
	// is enough: it keeps every other .json-opening test in this package on
	// the unregistered path it was written against, and avoids the real
	// plugin's LSP server popping the install prompt over the dialog under
	// test.
	lang.Register(lang.Language{ID: "json", Extensions: []string{"jqseed"}})
	m := newSized()
	path := filepath.Join(t.TempDir(), "seed.jqseed")
	if err := os.WriteFile(path, []byte("{\n  \"spec\": {\n    \"name\": \"ike\"\n  }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Park the caret on the "name" key, whose path is .spec.name.
	tm, cmd := m.openPathAt(path, 2, 6)
	m = openJQ(t, drainCmd(tm.(Model), cmd))
	if got := m.jqPlay.program; got != ".spec.name" {
		t.Errorf("seeded program = %q, want the caret's jq path", got)
	}
}

// TestJQPlaygroundInvalidProgramShowsError: a program that does not compile
// paints an error line in the dialog instead of crashing or clearing it.
func TestJQPlaygroundInvalidProgramShowsError(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"a":1}`))
	m = setProgram(m, ".foo[")
	if m.jqPlay.result.Err == "" {
		t.Fatal("an invalid program must report an error")
	}
	v := ansi.Strip(m.render())
	if !strings.Contains(v, "E: ") || !strings.Contains(v, "unexpected") {
		t.Errorf("the dialog should show the jq error inline, got:\n%s", v)
	}
}

// TestJQPlaygroundInvalidInputShowsError: a buffer that is not JSON is an
// inline message on the input line, not a failed open.
func TestJQPlaygroundInvalidInputShowsError(t *testing.T) {
	m := openJQ(t, jqApp(t, "this is not json\n"))
	if m.jqPlay.inputErr == "" {
		t.Fatal("a non-JSON buffer must report an input error")
	}
	v := ansi.Strip(m.render())
	if !strings.Contains(v, "not valid JSON") {
		t.Errorf("the dialog should say the input is not JSON, got:\n%s", v)
	}
}

// TestJQPlaygroundCapsHugeResult: an unbounded program stops at the cap and
// says so, and the dialog stays responsive (the run is off the event loop).
func TestJQPlaygroundCapsHugeResult(t *testing.T) {
	m := openJQ(t, jqApp(t, "null"))
	m = setProgram(m, "range(infinite)")
	if !m.jqPlay.result.Truncated {
		t.Fatal("an infinite program must report a truncated result")
	}
	v := ansi.Strip(m.render())
	if !strings.Contains(v, "stopped at") {
		t.Errorf("the dialog should mark the capped result, got:\n%s", v)
	}
}

// TestJQPlaygroundDropsSupersededEvaluation: typing while a run is in flight
// abandons it — the stale generation's result must not overwrite the current
// one, which is the whole point of stamping the runs.
func TestJQPlaygroundDropsSupersededEvaluation(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"a":1,"b":2}`))
	s := m.jqPlay

	// Two program changes: the first tick is stale by the time it arrives.
	s.result = jqplay.Result{} // drop the seed program's output
	s.program, s.pos = ".a", 2
	stale := m.scheduleJQEval()
	s.program, s.pos = ".b", 2
	fresh := m.scheduleJQEval()

	m = drainCmd(m, stale)
	if len(s.result.Outputs) != 0 {
		t.Fatalf("the superseded tick must not evaluate, got %v", s.result.Outputs)
	}
	m = drainCmd(m, fresh)
	if got := s.result.Text(); got != "2" {
		t.Fatalf("result = %q, want the current program's value", got)
	}

	// A late result carrying an old generation is dropped outright.
	m.finishJQEval(jqEvalDoneMsg{st: s, gen: s.gen - 1, res: jqplay.Result{Outputs: []string{"999"}}})
	if got := s.result.Text(); got != "2" {
		t.Errorf("a stale result overwrote the current one: %q", got)
	}
}

// TestJQPlaygroundDropsResultAfterClose: a run outliving its dialog must not
// resurrect it, and must not panic on the nil state.
func TestJQPlaygroundDropsResultAfterClose(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"a":1}`))
	s := m.jqPlay
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.jqPlayOpen() {
		t.Fatal("esc must close the playground")
	}
	m.finishJQEval(jqEvalDoneMsg{st: s, gen: s.gen, res: jqplay.Result{Outputs: []string{"1"}}})
	if _, cmd := m.Update(jqParseDoneMsg{st: s}); cmd != nil {
		t.Error("a parse finishing after the close must do nothing")
	}
}

// TestJQPlaygroundCopiesResult: ctrl+y writes the whole result — not just the
// visible window — to the system clipboard.
func TestJQPlaygroundCopiesResult(t *testing.T) {
	copied := ""
	prev := clipboardWrite
	clipboardWrite = func(s string) { copied = s }
	t.Cleanup(func() { clipboardWrite = prev })

	m := openJQ(t, jqApp(t, `{"foo":[1,2,3]}`))
	m = setProgram(m, ".foo[]")
	m = drainKey(m, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if copied != "1\n2\n3" {
		t.Fatalf("clipboard = %q, want every output value", copied)
	}
	if !strings.Contains(ansi.Strip(m.render()), "copied the result") {
		t.Error("the dialog should confirm the copy")
	}
}

// TestJQPlaygroundOpensResultAsScratch: ctrl+o writes the result into a fresh
// .json scratch and opens it, so a multi-step jq session can keep going.
func TestJQPlaygroundOpensResultAsScratch(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"foo":[1,2,3]}`))
	m = setProgram(m, "[.foo[] | . * 2]")
	m = drainKey(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})

	if m.jqPlayOpen() {
		t.Error("opening the result as a scratch should close the playground")
	}
	ed := m.activeEditor()
	if ed == nil || filepath.Ext(ed.Path()) != ".json" {
		t.Fatalf("the scratch must land in an editor as .json, got %v", ed)
	}
	if dir, _ := scratch.Dir(); !strings.HasPrefix(ed.Path(), dir) {
		t.Errorf("the result must open as a scratch, got %q", ed.Path())
	}
	body, err := os.ReadFile(ed.Path())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(strings.Fields(string(body)), ""); got != "[2,4,6]" {
		t.Errorf("scratch content = %q, want the result", got)
	}
}

// TestJQPlaygroundEmptyResultActions: copy and open-as-scratch on an empty
// result say so instead of clobbering the clipboard or creating a stub file.
func TestJQPlaygroundEmptyResultActions(t *testing.T) {
	copied := "untouched"
	prev := clipboardWrite
	clipboardWrite = func(s string) { copied = s }
	t.Cleanup(func() { clipboardWrite = prev })

	m := openJQ(t, jqApp(t, `{"a":1}`))
	m = setProgram(m, "empty")
	m = drainKey(m, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if copied != "untouched" {
		t.Errorf("an empty result must not write to the clipboard, got %q", copied)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if !m.jqPlayOpen() {
		t.Error("an empty result must not open a scratch")
	}
}

// TestJQPlaygroundHistory: enter records the program, up/down walk the
// session history, and the history survives closing the dialog.
func TestJQPlaygroundHistory(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"a":1,"b":2}`))
	m.jqPlay.program, m.jqPlay.pos = "", 0
	m = typeInto(m, ".a")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m.jqPlay.program, m.jqPlay.pos = "", 0
	m = typeInto(m, ".b")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	// enter left the query line holding the newest entry, so the first ↑
	// skips it (#1973) — a step that changed nothing would read as a dead key.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.jqPlay.program; got != ".a" {
		t.Fatalf("first ↑ = %q, want the program before the one on the line", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.jqPlay.program; got != ".a" {
		t.Fatalf("↑ at the oldest program = %q, want it to stay", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.jqPlay.program; got != ".b" {
		t.Fatalf("↓ = %q, want back to the newer program", got)
	}

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	m = openJQ(t, m)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.jqPlay.program; got != ".b" {
		t.Errorf("the history must outlive the dialog, got %q", got)
	}
}

// TestJQPlaygroundHistoryKeepsDraft: browsing away from a half-written
// program and back restores it — ↓ to the live slot must not clear the query
// line (#1973).
func TestJQPlaygroundHistoryKeepsDraft(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"a":1,"b":2}`))
	m.jqPlay.program, m.jqPlay.pos = "", 0
	m = typeInto(m, ".a")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m.jqPlay.program, m.jqPlay.pos = "", 0
	m = typeInto(m, ".b")

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.jqPlay.program; got != ".a" {
		t.Fatalf("↑ = %q, want the recorded program", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.jqPlay.program; got != ".b" {
		t.Fatalf("↓ = %q, want the draft back", got)
	}
	if got := m.jqPlay.pos; got != len(".b") {
		t.Errorf("the caret should sit at the end of the restored draft, got %d", got)
	}
	if got := m.jqPlay.result.Text(); got != "2" {
		t.Errorf("the restored draft must be evaluated again, got %q", got)
	}
	// A ↓ with nothing to come back from leaves the line alone.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.jqPlay.program; got != ".b" {
		t.Errorf("↓ at the live slot = %q, want the line untouched", got)
	}
}

// TestJQPlaygroundHistorySkipsSeededProgram: reopening over the same caret
// seeds the program that was last run, so the first ↑ must offer the one
// before it rather than re-typing what is already on the line (#1973).
func TestJQPlaygroundHistorySkipsSeededProgram(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"a":1,"b":2}`))
	m.jqPlay.program, m.jqPlay.pos = "", 0
	m = typeInto(m, ".a")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m.jqPlay.program, m.jqPlay.pos = "", 0
	m = typeInto(m, ".b")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	m = openJQ(t, m)
	// Reopening seeds "." here; pretend the caret seeded the last program.
	m.jqPlay.program, m.jqPlay.pos = ".b", len(".b")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.jqPlay.program; got != ".a" {
		t.Errorf("↑ over the seeded program = %q, want the entry before it", got)
	}
}

// TestJQPlaygroundResultNavigation: tab moves the keyboard into the result
// buffer, where the full editor keymap navigates — G jumps to the last line
// and scrolls the viewport; tab returns to the query line.
func TestJQPlaygroundResultNavigation(t *testing.T) {
	m := openJQ(t, jqApp(t, "null"))
	m = setProgram(m, "[range(100)]")
	ed := m.jqPlay.resultEd
	if got := ed.LineCount(); got < 100 {
		t.Fatalf("the fixture must overflow the pane, got %d rows", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if !m.jqPlay.bufFocus {
		t.Fatal("tab must move the focus into the result buffer")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: 'G', Text: "G"})
	if line, _ := ed.Cursor(); line != ed.LineCount() {
		t.Fatalf("G moved the cursor to line %d, want %d", line, ed.LineCount())
	}
	if ed.ScrollTop() == 0 {
		t.Error("jumping to the last line must scroll the result viewport")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.jqPlay.bufFocus {
		t.Error("tab must return the focus to the query line")
	}
}

// TestJQPlaygroundInlineEnterExit is #1970's core acceptance case: the mode
// mounts in the queried pane (no floating dialog), and esc restores the
// original buffer unchanged and editable.
func TestJQPlaygroundInlineEnterExit(t *testing.T) {
	const body = `{"foo":[1,2,3]}`
	m := jqApp(t, body)
	m = openJQ(t, m)
	if got, want := m.jqPlay.paneKey, m.activeWS().Panes.Focused(); got != want {
		t.Fatalf("the mode must mount in the focused pane, got %q vs %q", got, want)
	}
	v := ansi.Strip(m.render())
	if !strings.Contains(v, "> jq:") {
		t.Errorf("the query line must render inside the pane, got:\n%s", v)
	}
	if ed := m.activeEditor(); ed.Text() != body {
		t.Fatalf("the original buffer must stay untouched while the mode is open, got %q", ed.Text())
	}

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.jqPlayOpen() {
		t.Fatal("esc must leave the mode")
	}
	ed := m.activeEditor()
	if ed.Text() != body {
		t.Fatalf("esc must restore the original buffer unchanged, got %q", ed.Text())
	}
	if v := ansi.Strip(m.render()); !strings.Contains(v, `"foo"`) {
		t.Errorf("the pane must show the original content again, got:\n%s", v)
	}
	// The buffer is editable again: x deletes the character under the caret.
	m = drainKey(m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if got := m.activeEditor().Text(); got == body {
		t.Error("the buffer must be editable again after esc")
	}
}

// TestJQPlaygroundResultReadOnly: the result buffer refuses every mutation —
// typing in query focus edits only the program, and edit keys in buffer
// focus bounce off the read-only flag.
func TestJQPlaygroundResultReadOnly(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"a":1}`))
	m = setProgram(m, ".")
	ed := m.jqPlay.resultEd
	if !ed.ReadOnly() {
		t.Fatal("the result buffer must be read-only")
	}
	before := ed.Text()
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	for _, k := range []tea.KeyPressMsg{
		{Code: 'x', Text: "x"},
		{Code: 'd', Text: "d"}, {Code: 'd', Text: "d"},
		{Code: 'i', Text: "i"}, {Code: 'z', Text: "z"},
	} {
		m = drainKey(m, k)
	}
	if got := ed.Text(); got != before {
		t.Fatalf("edit keys mutated the read-only result: %q -> %q", before, got)
	}
	if m.jqPlay.result.Text() != before {
		t.Error("the evaluation result itself must be untouched")
	}
}

// TestJQPlaygroundResultSelection: visual selection works in the result
// buffer, esc first leaves visual mode like in any buffer, and only a second
// esc — from resting normal mode — closes the playground.
func TestJQPlaygroundResultSelection(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"foo":[1,2,3]}`))
	m = setProgram(m, ".foo[]")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = drainKey(m, tea.KeyPressMsg{Code: 'V', Text: "V"})
	m = drainKey(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	sel, has := m.jqPlay.resultEd.SelectionText()
	if !has || strings.TrimSpace(sel) == "" {
		t.Fatalf("visual selection must work in the result buffer, got %q", sel)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.jqPlayOpen() {
		t.Fatal("esc in visual mode must only leave the selection, not the mode")
	}
	if _, has := m.jqPlay.resultEd.SelectionText(); has {
		t.Error("esc must have collapsed the visual selection")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.jqPlayOpen() {
		t.Error("esc from resting normal mode must close the playground")
	}
}

// TestJQPlaygroundQueriesHTTPResponse: the playground works on `.http`
// response bodies as well as on plain JSON files.
func TestJQPlaygroundQueriesHTTPResponse(t *testing.T) {
	noDebounce(t)
	m := httpApp(t)
	resp := sampleResponse("one")
	resp.Body = []byte(`{"items":[{"id":7},{"id":8}]}`)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: resp})
	m = out.(Model)
	m.setFocus(pane.HTTPKey)

	m = openJQ(t, m)
	if got := m.jqPlay.source; got != "HTTP response" {
		t.Fatalf("source = %q, want the response pane", got)
	}
	if got := m.jqPlay.paneKey; got != pane.HTTPKey {
		t.Fatalf("the mode must mount in the response pane, got %q", got)
	}
	m = setProgram(m, "[.items[].id]")
	if got := strings.Join(strings.Fields(m.jqPlay.result.Text()), ""); got != "[7,8]" {
		t.Errorf("result = %q, want the ids from the response body", got)
	}
}

// TestJQPlaygroundQueriesSelection: with a visual selection open, the
// selected lines are the input — an embedded JSON blob in a log file is
// queryable without extracting it first.
func TestJQPlaygroundQueriesSelection(t *testing.T) {
	m := jqApp(t, "{\"a\":1}\nnot json at all\n")
	// Select the first line in visual-line mode.
	m = drainKey(m, tea.KeyPressMsg{Code: 'V', Text: "V"})
	m = openJQ(t, m)
	if !strings.Contains(m.jqPlay.source, "selection") {
		t.Fatalf("source = %q, want the selection", m.jqPlay.source)
	}
	if m.jqPlay.inputErr != "" {
		t.Fatalf("the selected JSON must parse, got %q", m.jqPlay.inputErr)
	}
	m = setProgram(m, ".a")
	if got := m.jqPlay.result.Text(); got != "1" {
		t.Errorf("result = %q, want the selected object's field", got)
	}
}

// TestJQPlaygroundHighlightsQuery: the query line is colored by the jq
// scanner, so a path and a string literal in the same program do not render
// in one flat color.
func TestJQPlaygroundHighlightsQuery(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"a":"x"}`))
	m = setProgram(m, `.a == "x"`)
	row := m.jqQueryRow(60)
	if ansi.Strip(row) == row {
		t.Fatalf("the query line should carry color, got %q", row)
	}
	styles := m.jqKindStyles()
	for _, want := range []string{
		styles[jqplay.KindPath].Render("a"),
		styles[jqplay.KindString].Render("x"),
	} {
		if !strings.Contains(row, want) {
			t.Errorf("the query line should render %q, got %q", want, row)
		}
	}
}

// TestJQPlaygroundWithoutInput: with nothing to query the command notifies
// instead of opening an empty dialog.
func TestJQPlaygroundWithoutInput(t *testing.T) {
	m := newSized()
	tm, cmd := m.Update(OpenJQPlaygroundMsg{})
	m = drainCmd(tm.(Model), cmd)
	if m.jqPlayOpen() {
		t.Fatal("with no JSON at hand the playground must not open")
	}
}

// TestJQPlaygroundPasteFlattens: a bracketed paste lands in the query line as
// one line — a multi-line program must not smuggle newlines into it.
func TestJQPlaygroundPasteFlattens(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"a":1}`))
	m.jqPlay.program, m.jqPlay.pos = "", 0
	tm, cmd := m.Update(tea.PasteMsg{Content: ".a\n| tostring"})
	m = drainCmd(tm.(Model), cmd)
	if strings.Contains(m.jqPlay.program, "\n") {
		t.Fatalf("the query line kept a newline: %q", m.jqPlay.program)
	}
	if m.jqPlay.result.Err != "" {
		t.Errorf("the flattened program should still compile, got %q", m.jqPlay.result.Err)
	}
}

// runJQProgram clears the query line, types program and records it with enter,
// the way a user runs one.
func runJQProgram(m Model, program string) Model {
	m.jqPlay.program, m.jqPlay.pos = "", 0
	m = typeInto(m, program)
	return drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
}

// TestJQPlaygroundHistoryCrossBuffer (#1977): the program history is one
// session-wide list. A program run over file A is offered by ↑ when the
// playground is reopened over a *different* file, and one run over an HTTP
// response body joins the same list.
func TestJQPlaygroundHistoryCrossBuffer(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"a":1}`))
	m = runJQProgram(m, ".a")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	// A second JSON file in the same editor pane.
	pathB := filepath.Join(t.TempDir(), "other.json")
	if err := os.WriteFile(pathB, []byte(`{"b":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.openPath(pathB, false)
	m = drainCmd(tm.(Model), cmd)
	m = openJQ(t, m)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.jqPlay.program; got != ".a" {
		t.Fatalf("↑ over another buffer = %q, want the program run on the first one", got)
	}
	m = runJQProgram(m, ".b")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	// And the HTTP response pane shares the very same list, both ways.
	resp := sampleResponse("one")
	resp.Body = []byte(`{"items":[1,2]}`)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: resp})
	m = out.(Model)
	m.setFocus(pane.HTTPKey)
	m = openJQ(t, m)
	if got := m.jqPlay.paneKey; got != pane.HTTPKey {
		t.Fatalf("the mode must mount in the response pane, got %q", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.jqPlay.program; got != ".b" {
		t.Fatalf("↑ over the response pane = %q, want the newest editor program", got)
	}
	m = runJQProgram(m, ".items")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	m.setFocus(m.recentEditor)
	m = openJQ(t, m)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.jqPlay.program; got != ".items" {
		t.Errorf("↑ back in an editor = %q, want the program run on the response", got)
	}
}

// TestJQPlaygroundHistorySurvivesReopen (#1977): the playground is reachable
// from the Tools menu while one is already open, and reopening replaces the
// mode. The programs the replaced one recorded must not go with it — the
// history is the root model's, not the mode's.
func TestJQPlaygroundHistorySurvivesReopen(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"a":1}`))
	m = runJQProgram(m, ".a")

	m = openJQ(t, m) // reopened without esc: the mode is replaced
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.jqPlay.program; got != ".a" {
		t.Errorf("↑ after reopening = %q, want the program recorded before it", got)
	}
}
