package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/editor"
	"ike/internal/jqplay"
	"ike/internal/lang"
	"ike/internal/layout"
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

// jqCaretApp opens a JSON file with the caret parked on the "name" key, whose
// document path is .spec.name — the input the two path-seeding cases need.
func jqCaretApp(t *testing.T) Model {
	t.Helper()
	noDebounce(t)
	// The path seed reads the caret's document path, which needs the buffer's
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
	tm, cmd := m.openPathAt(path, 2, 6)
	return drainCmd(tm.(Model), cmd)
}

// TestJQPlaygroundAtPathSeedsFromCaret: json.jqPlaygroundAtPath prefills the
// query line with the caret's jq path (#1660), which is what the separate
// command exists for (#1982).
func TestJQPlaygroundAtPathSeedsFromCaret(t *testing.T) {
	m := jqCaretApp(t)
	tm, cmd := m.Update(OpenJQPlaygroundAtPathMsg{})
	m = drainCmd(tm.(Model), cmd)
	if !m.jqPlayOpen() {
		t.Fatal("json.jqPlaygroundAtPath must open the playground")
	}
	if got := m.jqPlay.program; got != ".spec.name" {
		t.Errorf("seeded program = %q, want the caret's jq path", got)
	}
}

// TestJQPlaygroundOpensOnIdentity: the ordinary open does *not* prefill the
// caret's path any more (#1982) — a fresh file starts on `.`, so checking
// something needs no deleting first.
func TestJQPlaygroundOpensOnIdentity(t *testing.T) {
	m := openJQ(t, jqCaretApp(t))
	if got := m.jqPlay.program; got != "." {
		t.Errorf("seeded program = %q, want the identity program", got)
	}
}

// TestJQPlaygroundRecallsLastProgramPerFile: a program that ran cleanly over a
// file is what the next open over that same file starts on, while another file
// still opens on `.` (#1982).
func TestJQPlaygroundRecallsLastProgramPerFile(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"foo":{"bar":1}}`))
	m = runJQProgram(m, ".foo.bar")
	m = closeJQ(m)

	m = openJQ(t, m)
	if got := m.jqPlay.program; got != ".foo.bar" {
		t.Errorf("reopened on %q, want the file's last valid program", got)
	}
	m = closeJQ(m)

	// A second file in the same session has its own memory — and none yet, so
	// it opens on the identity program even though the history is shared.
	pathB := filepath.Join(t.TempDir(), "other.json")
	if err := os.WriteFile(pathB, []byte(`{"b":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.openPath(pathB, false)
	m = openJQ(t, drainCmd(tm.(Model), cmd))
	if got := m.jqPlay.program; got != "." {
		t.Errorf("fresh file opened on %q, want the identity program", got)
	}
}

// closeJQ leaves the playground with esc, dismissing the completion popup
// first when typing left one open (#1979) — esc would only close that.
func closeJQ(m Model) Model {
	m = dismissJQPopup(m)
	return drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
}

// TestJQPlaygroundDoesNotRecallBrokenProgram: only a program that actually ran
// is remembered — a compile error leaves the last good one in place (#1982).
func TestJQPlaygroundDoesNotRecallBrokenProgram(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"foo":1}`))
	m = runJQProgram(m, ".foo")
	m = setProgram(m, ".foo[")
	if m.jqPlay.result.Err == "" {
		t.Fatal("the broken program must report a compile error")
	}
	m = closeJQ(m)

	m = openJQ(t, m)
	if got := m.jqPlay.program; got != ".foo" {
		t.Errorf("reopened on %q, want the last program that ran", got)
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
	// The cap is a caveat, not decoration: it renders in the Warning slot,
	// not the row's dim Hint (#1978).
	warn := lipgloss.NewStyle().Foreground(m.pal().Warning)
	if row := m.jqInfoRow(120); !strings.Contains(row, warn.Render(fmt.Sprintf(" (stopped at %d)", jqplay.MaxOutputs))) {
		t.Errorf("the cap should render in the Warning color, got %q", row)
	}
}

// TestJQPlaygroundErrorKeepsValueCount (#1978): a runtime error arriving
// after values were produced must not hide that count — the values sit in
// the buffer below the error line.
func TestJQPlaygroundErrorKeepsValueCount(t *testing.T) {
	m := openJQ(t, jqApp(t, `[{"x":1},3]`))
	m = setProgram(m, ".[] | .x")
	s := m.jqPlay
	if s.result.Err == "" || len(s.result.Outputs) != 1 {
		t.Fatalf("want one value then an error, got err=%q outputs=%v", s.result.Err, s.result.Outputs)
	}
	row := ansi.Strip(m.jqInfoRow(200))
	if !strings.HasPrefix(row, "E: ") {
		t.Errorf("the error must render, got %q", row)
	}
	if !strings.Contains(row, "1 value(s) before the error") {
		t.Errorf("the error row should keep the produced value count, got %q", row)
	}
}

// TestJQPlaygroundZeroValuesWarn (#1978): a program yielding nothing blanks
// the result buffer, so the zero count is the only signal — it renders in
// Warning, not the row's dim Hint.
func TestJQPlaygroundZeroValuesWarn(t *testing.T) {
	m := openJQ(t, jqApp(t, `[1,2,3]`))
	m = setProgram(m, ".[] | select(. > 9)")
	if got := m.jqPlay.result.Text(); got != "" {
		t.Fatalf("result = %q, want empty", got)
	}
	warn := lipgloss.NewStyle().Foreground(m.pal().Warning)
	if row := m.jqInfoRow(120); !strings.Contains(row, warn.Render("Result — 0 value(s)")) {
		t.Errorf("a zero-value summary should render in Warning, got %q", row)
	}
}

// TestJQPlaygroundNarrowInfoRowDropsWholeHints (#1978): on a narrow pane the
// key hints are dropped as whole segments, never clipped mid-word, and the
// input/result summary always survives.
func TestJQPlaygroundNarrowInfoRowDropsWholeHints(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"a":1}`))
	m = setProgram(m, ".")
	wide := ansi.Strip(m.jqInfoRow(200))
	if !strings.Contains(wide, "esc close") {
		t.Fatalf("a wide row should hold every hint, got %q", wide)
	}
	narrow := ansi.Strip(m.jqInfoRow(60))
	if !strings.Contains(narrow, "Result — 1 value(s)") {
		t.Errorf("the summary must survive a narrow row, got %q", narrow)
	}
	if strings.Contains(narrow, "esc close") {
		t.Errorf("a 60-cell row cannot hold every hint, got %q", narrow)
	}
	// Whatever fit is whole segments: the row ends with a complete hint, not
	// a mid-word cut marked by the truncation ellipsis.
	if strings.Contains(narrow, "…") {
		t.Errorf("hints should be dropped whole, not clipped, got %q", narrow)
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
	m = runJQProgram(m, ".a")
	m = runJQProgram(m, ".b")

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
	// The reopen seeds this file's last valid program, ".b" (#1982), so the
	// first ↑ skips it and offers the entry before it — the history is still
	// there, and it is still the session-wide one.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.jqPlay.program; got != ".a" {
		t.Errorf("the history must outlive the dialog, got %q", got)
	}
}

// TestJQPlaygroundHistoryKeepsDraft: browsing away from a half-written
// program and back restores it — ↓ to the live slot must not clear the query
// line (#1973).
func TestJQPlaygroundHistoryKeepsDraft(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"a":1,"b":2}`))
	m = runJQProgram(m, ".a")
	m.jqPlay.program, m.jqPlay.pos = "", 0
	m = typeInto(m, ".b")
	m = dismissJQPopup(m) // ↑ must reach the history, not the popup (#1979)

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
	m = runJQProgram(m, ".a")
	m.jqPlay.program, m.jqPlay.pos = "", 0
	m = typeInto(m, ".b")
	m = dismissJQPopup(m) // the first esc would only dismiss the popup (#1979)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	m = openJQ(t, m)
	// Reopening seeds the file's last valid program, ".b" (#1982) — which is
	// exactly the newest history entry the first ↑ has to step over.
	if got := m.jqPlay.program; got != ".b" {
		t.Fatalf("reopen seeded %q, want the last program run on the file", got)
	}
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

// TestJQPlaygroundGlobalChords guards #1983: Global-scope chords the
// playground doesn't claim keep working while it owns the keyboard —
// cmd+shift+a opens Search Everywhere and cmd+e opens Recent Files, from the
// query line and from the result buffer alike — while the playground's own
// keys keep priority.
func TestJQPlaygroundGlobalChords(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"name":"ike"}`))

	m = drainKey(m, tea.KeyPressMsg{Code: 'a', Mod: tea.ModSuper | tea.ModShift})
	if !m.palette.IsOpen() {
		t.Fatal("cmd+shift+a in the playground must open Search Everywhere")
	}
	if !m.jqPlayOpen() {
		t.Fatal("a global chord must not close the playground")
	}
	m.palette.Close()

	m = drainKey(m, tea.KeyPressMsg{Code: 'e', Mod: tea.ModSuper})
	if !m.palette.IsOpen() {
		t.Fatal("cmd+e in the playground must open Recent Files")
	}
	m.palette.Close()

	// The same chords escape the result buffer after tab moves the focus.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if !m.jqPlay.bufFocus {
		t.Fatal("tab must move the focus into the result buffer")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: 'a', Mod: tea.ModSuper | tea.ModShift})
	if !m.palette.IsOpen() {
		t.Fatal("cmd+shift+a from the result buffer must open Search Everywhere")
	}
	m.palette.Close()

	// The playground's own keys keep priority: typing still edits the query
	// line, and esc still closes the mode.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab}) // back to the query line
	m.jqPlay.program, m.jqPlay.pos = "", 0
	m = typeInto(m, ".name")
	if m.jqPlay.program != ".name" {
		t.Fatalf("typing must stay with the query line, got %q", m.jqPlay.program)
	}
	if m.palette.IsOpen() {
		t.Fatal("plain typing must not dispatch global bindings")
	}
	m = dismissJQPopup(m) // typing opened the completion popup (#1979)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.jqPlayOpen() {
		t.Fatal("esc must keep closing the playground")
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

// jqTestClip is a fake system clipboard for the result buffer's `+` register.
type jqTestClip struct{ text string }

func (c *jqTestClip) Read() (string, error) { return c.text, nil }
func (c *jqTestClip) Write(s string) error  { c.text = s; return nil }

// jqCopyKey is the app keymap's editor.copy chord as a key event: cmd+c on
// macOS, folded to ctrl+c everywhere else (keymap.NormalizeKey).
func jqCopyKey() tea.KeyPressMsg {
	if runtime.GOOS == "darwin" {
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModMeta}
	}
	return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
}

// TestJQPlaygroundCopyChordCopiesSelection (#1980): the copy chord writes the
// result buffer's visual selection to the system clipboard, like in a normal
// read-only buffer — the modal routing must not swallow it.
func TestJQPlaygroundCopyChordCopiesSelection(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"foo":[1,2,3]}`))
	m = setProgram(m, ".foo[]")
	clip := &jqTestClip{}
	m.jqPlay.resultEd.SetClipboard(clip)

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = drainKey(m, tea.KeyPressMsg{Code: 'V', Text: "V"})
	m = drainKey(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if _, has := m.jqPlay.resultEd.SelectionText(); !has {
		t.Fatal("the fixture needs a visual selection")
	}
	m = drainKey(m, jqCopyKey())
	if !strings.Contains(clip.text, "1") || !strings.Contains(clip.text, "2") {
		t.Fatalf("clipboard = %q, want the selected lines", clip.text)
	}
	if strings.Contains(clip.text, "3") {
		t.Errorf("clipboard = %q, must hold only the selection, not the whole result", clip.text)
	}
	if !m.jqPlayOpen() {
		t.Error("copying must leave the playground open")
	}
}

// TestJQPlaygroundMenuCopyReachesResultBuffer (#1980): the Edit menu's copy
// dispatches editor.ActionMsg — while the playground pane is focused it must
// act on the substitute result buffer, not the pane's hidden document.
func TestJQPlaygroundMenuCopyReachesResultBuffer(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"foo":[1,2,3]}`))
	m = setProgram(m, ".foo[]")
	clip := &jqTestClip{}
	m.jqPlay.resultEd.SetClipboard(clip)

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = drainKey(m, tea.KeyPressMsg{Code: 'V', Text: "V"})
	tm, cmd := m.Update(editor.ActionMsg{Action: "copy"})
	m = drainCmd(tm.(Model), cmd)
	if !strings.Contains(clip.text, "1") {
		t.Fatalf("clipboard = %q, want the result buffer's selection", clip.text)
	}
}

// TestJQPlaygroundSurvivesFocusChange (#1980): moving the focus to another
// pane leaves the playground mounted with query and result intact, the other
// pane takes keys normally, and refocusing resumes the query line as it was.
func TestJQPlaygroundSurvivesFocusChange(t *testing.T) {
	m := jqApp(t, `{"foo":[1,2,3]}`)
	jqKey := m.activeWS().Panes.Focused()
	// A second editor pane to work in while the result stays visible.
	m.SplitFocused(layout.ZoneRight)
	otherKey := m.activeWS().Panes.Focused()
	m.setFocus(jqKey)
	m = openJQ(t, m)
	m = setProgram(m, ".foo[]")
	program := m.jqPlay.program

	// The spatial focus move escapes the playground pane instead of being
	// swallowed by the modal routing.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl})
	if !m.jqPlayOpen() {
		t.Fatal("a focus change must not close the playground")
	}
	if got := m.activeWS().Panes.Focused(); got != otherKey {
		t.Fatalf("focus = %q, want the other pane %q", got, otherKey)
	}

	// Editing in the other pane works normally; nothing leaks into the query
	// line and the result stays intact.
	m = drainKey(m, tea.KeyPressMsg{Code: 'i', Text: "i"})
	m = typeInto(m, "hello")
	if ed := m.activeWS().Panes.Get(otherKey).Editor(); ed == nil || !strings.Contains(ed.Text(), "hello") {
		t.Fatal("typing must edit the focused pane while the playground is open elsewhere")
	}
	if got := m.jqPlay.program; got != program {
		t.Fatalf("query line = %q, keys for the other pane must not reach it", got)
	}
	if got := m.jqPlay.result.Text(); got != "1\n2\n3" {
		t.Fatalf("result = %q, must survive the focus change", got)
	}
	v := ansi.Strip(m.render())
	if !strings.Contains(v, "jq:") {
		t.Errorf("the unfocused playground must keep rendering, got:\n%s", v)
	}
	if strings.Contains(v, "> jq:") {
		t.Errorf("the unfocused query line must blank its `>` marker (#1978), got:\n%s", v)
	}

	// Refocusing the hosting pane resumes the query line.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape}) // leave insert mode first
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl})
	if got := m.activeWS().Panes.Focused(); got != jqKey {
		t.Fatalf("focus = %q, want back on the playground pane %q", got, jqKey)
	}
	m = typeInto(m, " ")
	if got := m.jqPlay.program; got != program+" " {
		t.Fatalf("query line = %q, refocusing must resume it", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.jqPlayOpen() {
		t.Error("esc in the playground pane must still close the mode")
	}
}

// TestJQPlaygroundSurvivesClickIntoOtherPane (#1980): a mouse click into
// another pane moves the focus and leaves the playground mounted.
func TestJQPlaygroundSurvivesClickIntoOtherPane(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"foo":[1,2,3]}`))
	m = setProgram(m, ".foo[]")
	r, ok := m.lay.Panes[pane.ExplorerKey]
	if !ok {
		t.Fatal("the explorer pane must be laid out")
	}
	tm, cmd := m.Update(tea.MouseClickMsg{X: r.X + r.W/2, Y: r.Y + r.H/2, Button: tea.MouseLeft})
	m = drainCmd(tm.(Model), cmd)
	if !m.jqPlayOpen() {
		t.Fatal("a click into another pane must not close the playground")
	}
	if got := m.activeWS().Panes.Focused(); got != pane.ExplorerKey {
		t.Fatalf("focus = %q, want the clicked explorer", got)
	}
	if got := m.jqPlay.result.Text(); got != "1\n2\n3" {
		t.Errorf("result = %q, must survive the click", got)
	}
}

// TestJQPlaygroundClosesWithHostingPane (#1980): the mode survives focus
// changes, so its pane can now be closed from elsewhere — the playground must
// die with it instead of dangling over a removed key.
func TestJQPlaygroundClosesWithHostingPane(t *testing.T) {
	m := jqApp(t, `{"foo":[1,2,3]}`)
	jqKey := m.activeWS().Panes.Focused()
	m.SplitFocused(layout.ZoneRight)
	m.setFocus(jqKey)
	m = openJQ(t, m)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl})
	m.closePane(jqKey)
	if m.jqPlayOpen() {
		t.Fatal("closing the hosting pane must close the playground")
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
// the way a user runs one. Typing may have opened the completion popup
// (#1979), which owns enter while it shows; it is dismissed first so enter
// keeps its record-and-run meaning.
func runJQProgram(m Model, program string) Model {
	m.jqPlay.program, m.jqPlay.pos = "", 0
	m = typeInto(m, program)
	m = dismissJQPopup(m)
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

// jqLongProgram is a pipeline of the shape the issue reports (#2032): far
// wider than a pane, with `|` stages to break at.
const jqLongProgram = `.hits.hits[]._source | .keyword as $keyword | .ser[] | select(.domain == "universal-search-box.com") | {$keyword, type: .kind, url: .link}`

// toggleJQView presses the chord bound to json.jqQueryView (default
// ctrl+alt+e), the way a user reaches the full-query view.
func toggleJQView(m Model) Model {
	return drainKey(m, tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl | tea.ModAlt})
}

// jqQueryText renders the query rows and strips the color, so an assertion can
// talk about the program that is actually on screen.
func jqQueryText(m Model, width int) string {
	var b strings.Builder
	for _, row := range m.jqQueryRows(width) {
		b.WriteString(ansi.Strip(row))
	}
	return b.String()
}

// TestJQPlaygroundExpandedQueryShowsWholeProgram is the issue's acceptance
// case (#2032): a program wider than the pane is cut on the one-line view and
// fully readable after the toggle — without leaving the playground and
// without changing the program.
func TestJQPlaygroundExpandedQueryShowsWholeProgram(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"hits":{"hits":[]}}`))
	m = setProgram(m, jqLongProgram)

	const width = 80
	if got := jqQueryText(m, width); strings.Contains(got, ".hits.hits[]") {
		t.Fatalf("the one-line view cannot show the whole program, got %q", got)
	}
	m = toggleJQView(m)
	if !m.jqPlay.expanded {
		t.Fatal("the json.jqQueryView chord must expand the query view")
	}
	if got := m.jqPlay.program; got != jqLongProgram {
		t.Fatalf("the view must not touch the program, got %q", got)
	}
	got := strings.ReplaceAll(jqQueryText(m, width), " ", "")
	want := strings.ReplaceAll(jqLongProgram, " ", "")
	if !strings.Contains(got, want) {
		t.Errorf("the expanded view must show the whole program, got %q", got)
	}
	// Toggling back restores the resting one-line layout.
	m = toggleJQView(m)
	if m.jqPlay.expanded || len(m.jqQueryRows(width)) != 1 {
		t.Error("the toggle must fold the view back to one row")
	}
}

// TestJQPlaygroundExpandedQueryKeepsHighlighting (#2032): the wrapped rows are
// colored by the same scanner the one-line view uses.
func TestJQPlaygroundExpandedQueryKeepsHighlighting(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"a":"x"}`))
	m = setProgram(m, `.aaaaaaaaaa | select(.b == "x") | .ccccccccc | map(.d)`)
	m = toggleJQView(m)
	rows := m.jqQueryRows(40)
	if len(rows) < 2 {
		t.Fatalf("the program should wrap over several rows, got %d", len(rows))
	}
	joined := strings.Join(rows, "")
	if ansi.Strip(joined) == joined {
		t.Fatal("the expanded rows should carry color")
	}
	styles := m.jqKindStyles()
	for _, want := range []string{
		styles[jqplay.KindString].Render("x"),
		styles[jqplay.KindFunc].Render("m"),
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the expanded view should render %q, got %q", want, joined)
		}
	}
}

// jqResultHeight is how many rows the substitute result buffer renders — the
// editor keeps its height private, and the rendered row count is what the
// pane geometry is about anyway.
func jqResultHeight(m Model) int {
	return len(strings.Split(m.jqPlay.resultEd.View(), "\n"))
}

// TestJQPlaygroundExpandedHeaderKeepsGeometry (#2032): the growing header is
// what the pane geometry reserves and what the result buffer shrinks by, so
// the mode still fills its pane exactly — and the mouse translation follows.
func TestJQPlaygroundExpandedHeaderKeepsGeometry(t *testing.T) {
	// A result long enough to fill the pane, so the buffer's rendered height is
	// its geometry rather than its (shorter) content.
	items := make([]string, 40)
	for i := range items {
		items[i] = fmt.Sprintf(`{"n":%d}`, i)
	}
	m := openJQ(t, jqApp(t, `{"items":[`+strings.Join(items, ",")+`]}`))
	m = setProgram(m, `.items[] | select(.n >= 0) | {n: .n, doubled: (.n * 2), label: ("row-" + (.n | tostring))}`)
	key := m.jqPlay.paneKey
	r, ok := m.lay.Panes[key]
	if !ok {
		t.Fatal("the hosting pane must have a rect")
	}
	width := r.W - paneChromeW
	if got := m.jqHeaderRowsFor(key); got != jqHeaderRows {
		t.Fatalf("the resting header is %d rows, want %d", got, jqHeaderRows)
	}
	before := jqResultHeight(m)
	if got, want := len(strings.Split(m.jqInlineBody(width), "\n")), paneInterior(r.H, paneChromeH); got != want {
		t.Fatalf("the resting pane body renders %d rows, want %d", got, want)
	}

	m = toggleJQView(m)
	rows := m.jqHeaderRowsFor(key)
	if rows <= jqHeaderRows {
		t.Fatalf("the expanded header must grow, got %d rows", rows)
	}
	if got, want := len(m.jqQueryRows(width))+jqInfoRows, rows; got != want {
		t.Errorf("rendered %d header rows, geometry reserved %d", got, want)
	}
	if got, want := jqResultHeight(m), before-(rows-jqHeaderRows); got != want {
		t.Errorf("result buffer is %d rows, want %d", got, want)
	}
	if got, want := len(strings.Split(m.jqInlineBody(width), "\n")), paneInterior(r.H, paneChromeH); got != want {
		t.Errorf("the expanded pane body renders %d rows, want %d", got, want)
	}
}

// TestJQPlaygroundExpandedQueryCaps (#2032): a program too long even for the
// expanded view keeps the result visible, windows around the cursor's row and
// says that it is still cut.
func TestJQPlaygroundExpandedQueryCaps(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"a":1}`))
	m = setProgram(m, strings.Repeat(".aaaaaaaa | ", 40)+".a")
	m = toggleJQView(m)
	rows := m.jqQueryRows(60)
	if len(rows) > jqMaxQueryRows {
		t.Fatalf("the expanded view must cap at %d rows, got %d", jqMaxQueryRows, len(rows))
	}
	if !strings.Contains(ansi.Strip(strings.Join(rows, "")), "…") {
		t.Error("a capped expanded view must mark that it is cut")
	}
	// The cursor sits at the end of the program, so the last rows are the ones
	// on screen — the window follows the caret like the one-line view.
	if got := ansi.Strip(rows[0]); !strings.HasPrefix(got, "> jq: …") {
		t.Errorf("the capped view must window around the cursor, got %q", got)
	}
	if got := strings.TrimRight(jqQueryText(m, 60), " "); !strings.HasSuffix(got, "| .a") {
		t.Errorf("the cursor's row must be on screen, got %q", got)
	}
	if !strings.Contains(ansi.Strip(m.jqInfoRow(60)), "query cut") {
		t.Error("the info row must still flag the cut")
	}
}

// TestJQPlaygroundQueryCutHint (#2032): a cut program is flagged on the info
// row, and the hint names the chord that shows it whole — from the query line
// and from the result buffer alike.
func TestJQPlaygroundQueryCutHint(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"hits":{"hits":[]}}`))
	m = setProgram(m, ".a")
	if got := ansi.Strip(m.jqInfoRow(90)); strings.Contains(got, "query cut") {
		t.Errorf("a program that fits is not cut, got %q", got)
	}
	m = setProgram(m, jqLongProgram)
	if got := ansi.Strip(m.jqInfoRow(90)); !strings.Contains(got, "query cut") {
		t.Errorf("a cut program must be flagged, got %q", got)
	}
	if got := ansi.Strip(m.jqInfoRow(200)); !strings.Contains(got, "ctrl+alt+e full query") {
		t.Errorf("the info row must document the full-query chord, got %q", got)
	}
	m = toggleJQView(m)
	if got := ansi.Strip(m.jqInfoRow(200)); !strings.Contains(got, "ctrl+alt+e one-line query") {
		t.Errorf("the expanded view must document the way back, got %q", got)
	}
	// The result buffer's hint row documents it too — the toggle works from
	// there as well.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if !m.jqPlay.bufFocus {
		t.Fatal("tab must move the focus into the result buffer")
	}
	if got := ansi.Strip(m.jqInfoRow(200)); !strings.Contains(got, "ctrl+alt+e") {
		t.Errorf("the result buffer's hints must document the toggle, got %q", got)
	}
	m = toggleJQView(m)
	if m.jqPlay.expanded {
		t.Error("the toggle must work from the result buffer too")
	}
}

// TestJQPlaygroundQueryViewCommand (#2032): the palette / Tools menu command
// drives the same toggle as the chord, and is inert with no playground open.
func TestJQPlaygroundQueryViewCommand(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"a":1}`))
	m = setProgram(m, jqLongProgram)
	tm, cmd := m.Update(ToggleJQQueryViewMsg{})
	m = drainCmd(tm.(Model), cmd)
	if !m.jqPlay.expanded {
		t.Fatal("json.jqQueryView must expand the query view")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	tm, cmd = m.Update(ToggleJQQueryViewMsg{})
	m = drainCmd(tm.(Model), cmd)
	if m.jqPlayOpen() {
		t.Error("the command must not open a playground of its own")
	}
}

// jqKeys feeds a run of plain keys into the model one at a time — the vim
// fold commands are two-key sequences (z then a), so a test types them the
// way a user does.
func jqKeys(m Model, keys string) Model {
	for _, r := range keys {
		m = drainKey(m, tea.KeyPressMsg{Text: string(r), Code: r})
	}
	return m
}

// TestJQResultFoldsAtCursor is the issue's acceptance case (#2029): za in the
// result buffer collapses the object under the cursor to one row carrying a
// placeholder with its key count, and hides its body.
func TestJQResultFoldsAtCursor(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"spec":{"image":"ike","tag":"v1"},"n":3}`))
	m = setProgram(m, ".")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab}) // keyboard into the result
	m = jqKeys(m, "jjza")                              // onto the "spec" line, fold it

	view := ansi.Strip(m.render())
	if !strings.Contains(view, "⋯ 2 keys }") {
		t.Fatalf("the collapsed object must render its key count, got:\n%s", view)
	}
	if strings.Contains(view, `"image"`) {
		t.Errorf("the folded body must be hidden, got:\n%s", view)
	}
	if !strings.Contains(view, `"n": 3`) {
		t.Errorf("only the fold's own body may be hidden, got:\n%s", view)
	}
}

// TestJQResultFoldsNested: a node inside an opened parent folds on its own —
// zM collapses everything, zo on the outer node reveals one level with the
// inner fold still closed, and its placeholder counts array items.
func TestJQResultFoldsNested(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"spec":{"ports":[80,443,8080]}}`))
	m = setProgram(m, ".")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = jqKeys(m, "zM") // every fold closed
	if view := ansi.Strip(m.render()); !strings.Contains(view, "⋯ 1 key }") || strings.Contains(view, `"ports"`) {
		t.Fatalf("zM must collapse the outermost value, got:\n%s", view)
	}
	m = jqKeys(m, "zo") // one level open again
	view := ansi.Strip(m.render())
	if !strings.Contains(view, `"spec": { ⋯ 1 key }`) {
		t.Fatalf("zo must reveal one level, with the node inside it still folded, got:\n%s", view)
	}
	m = jqKeys(m, "jzo") // and the node inside it opens on its own
	view = ansi.Strip(m.render())
	if !strings.Contains(view, "⋯ 3 items ]") {
		t.Errorf("the nested array must fold with its item count, got:\n%s", view)
	}
	if strings.Contains(view, "443") {
		t.Errorf("a fold inside the opened node must stay closed, got:\n%s", view)
	}
}

// TestJQResultFoldsResetOnNewQuery: a new program installs a new result, and
// the folds of the previous one must not survive it — no orphan fold, no
// hidden line in a document that never had one.
func TestJQResultFoldsResetOnNewQuery(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"spec":{"image":"ike","tag":"v1"},"n":3}`))
	m = setProgram(m, ".")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = jqKeys(m, "jjza")

	m = setProgram(m, ".spec")
	view := ansi.Strip(m.render())
	if strings.Contains(view, "⋯") {
		t.Fatalf("a new query must leave no fold behind, got:\n%s", view)
	}
	if !strings.Contains(view, `"image": "ike"`) {
		t.Fatalf("the new result must render in full, got:\n%s", view)
	}
	// The new result folds on its own terms: one object, two keys.
	m = jqKeys(m, "za")
	if view := ansi.Strip(m.render()); !strings.Contains(view, "⋯ 2 keys }") {
		t.Errorf("the new result must be foldable, got:\n%s", view)
	}
}

// TestJQResultFoldHintsAdvertised: the info row names the fold keys while the
// result buffer holds the keyboard — the mode's own help line is where the
// binding is documented (#2029).
func TestJQResultFoldHintsAdvertised(t *testing.T) {
	m := openJQ(t, jqApp(t, `{"a":1}`))
	m = setProgram(m, ".")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	hints := strings.Join(m.jqHints(), " · ")
	if !strings.Contains(hints, "za fold") || !strings.Contains(hints, "zM/zR fold all") {
		t.Fatalf("the result buffer's hints must name the fold keys, got %q", hints)
	}
	// The row drops trailing hints whole on a narrow pane, so the assertion
	// that they reach the screen is made against a wide one.
	if row := ansi.Strip(m.jqInfoRow(200)); !strings.Contains(row, "za fold") {
		t.Errorf("the info row must show the fold hint, got:\n%s", row)
	}
}
