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

// playground_test.go covers the playground (#1936, jq dialect): live
// evaluation over a JSON buffer and over an HTTP response body, inline
// errors, the debounce/generation cancellation, the result actions and the
// session program history.

// noDebounce makes evaluation fire on the next tick instead of after the
// human-scale delay, so a test drives the dialog without sleeping.
func noDebounce(t *testing.T) {
	t.Helper()
	prev := playDebounce
	playDebounce = 0
	t.Cleanup(func() { playDebounce = prev })
}

// playApp opens body as a .json file in the focused editor and returns the
// model, which is the "open JSON buffer" the playground is written for.
func playApp(t *testing.T, body string) Model {
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
	tm, cmd := m.Update(OpenPlaygroundMsg{})
	m = drainCmd(tm.(Model), cmd)
	if !m.playOpen() {
		t.Fatal("json.jqPlayground must open the playground")
	}
	return m
}

// setProgram replaces the query line and evaluates, the way enter does.
func setProgram(m Model, program string) Model {
	m.play.program = program
	m.play.pos = len([]rune(program))
	return drainCmd(m, m.runPlayNow())
}

// TestJQPlaygroundEvaluatesLive is the issue's acceptance case: a select
// program against an open JSON buffer shows the matching values.
func TestJQPlaygroundEvaluatesLive(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":[{"bar":1},{"bar":4},{"bar":9}]}`))
	m = setProgram(m, ".foo[] | select(.bar > 3)")

	s := m.play
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
	m := openJQ(t, playApp(t, `{"name":"ike"}`))
	m.play.program, m.play.pos = "", 0
	m = typeInto(m, ".name")
	if got := m.play.result.Text(); got != `"ike"` {
		t.Fatalf("live result = %q, want the field value", got)
	}
}

// playCaretApp opens a JSON file with the caret parked on the "name" key, whose
// document path is .spec.name — the input the two path-seeding cases need.
func playCaretApp(t *testing.T) Model {
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
	m := playCaretApp(t)
	tm, cmd := m.Update(OpenPlaygroundAtPathMsg{})
	m = drainCmd(tm.(Model), cmd)
	if !m.playOpen() {
		t.Fatal("json.jqPlaygroundAtPath must open the playground")
	}
	if got := m.play.program; got != ".spec.name" {
		t.Errorf("seeded program = %q, want the caret's jq path", got)
	}
}

// TestJQPlaygroundOpensOnIdentity: the ordinary open does *not* prefill the
// caret's path any more (#1982) — a fresh file starts on `.`, so checking
// something needs no deleting first.
func TestJQPlaygroundOpensOnIdentity(t *testing.T) {
	m := openJQ(t, playCaretApp(t))
	if got := m.play.program; got != "." {
		t.Errorf("seeded program = %q, want the identity program", got)
	}
}

// TestJQPlaygroundRecallsLastProgramPerFile: a program that ran cleanly over a
// file is what the next open over that same file starts on, while another file
// still opens on `.` (#1982).
func TestJQPlaygroundRecallsLastProgramPerFile(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":{"bar":1}}`))
	m = runJQProgram(m, ".foo.bar")
	m = closeJQ(m)

	m = openJQ(t, m)
	if got := m.play.program; got != ".foo.bar" {
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
	if got := m.play.program; got != "." {
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
	m := openJQ(t, playApp(t, `{"foo":1}`))
	m = runJQProgram(m, ".foo")
	m = setProgram(m, ".foo[")
	if m.play.runErr == "" {
		t.Fatal("the broken program must report a compile error")
	}
	m = closeJQ(m)

	m = openJQ(t, m)
	if got := m.play.program; got != ".foo" {
		t.Errorf("reopened on %q, want the last program that ran", got)
	}
}

// TestJQPlaygroundInvalidProgramShowsError: a program that does not compile
// paints an error line in the dialog instead of crashing or clearing it.
func TestJQPlaygroundInvalidProgramShowsError(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m = setProgram(m, ".foo[")
	if m.play.runErr == "" {
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
	m := openJQ(t, playApp(t, "this is not json\n"))
	if m.play.inputErr == "" {
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
	m := openJQ(t, playApp(t, "null"))
	m = setProgram(m, "range(infinite)")
	if !m.play.result.Truncated {
		t.Fatal("an infinite program must report a truncated result")
	}
	v := ansi.Strip(m.render())
	if !strings.Contains(v, "stopped at") {
		t.Errorf("the dialog should mark the capped result, got:\n%s", v)
	}
	// The cap is a caveat, not decoration: it renders in the Warning slot,
	// not the row's dim Hint (#1978).
	warn := lipgloss.NewStyle().Foreground(m.pal().Warning)
	if row := m.playInfoRow(120); !strings.Contains(row, warn.Render(fmt.Sprintf(" (stopped at %d)", jqplay.MaxOutputs))) {
		t.Errorf("the cap should render in the Warning color, got %q", row)
	}
}

// TestJQPlaygroundErrorKeepsLastGoodResult (#2412): a run that fails leaves
// the previous result in the buffer and says on the info row which one the
// reader is looking at, instead of counting the failed run's partial output.
func TestJQPlaygroundErrorKeepsLastGoodResult(t *testing.T) {
	m := openJQ(t, playApp(t, `[{"x":1},3]`))
	m = setProgram(m, ".[0]")
	good := m.play.result.Text()
	m = setProgram(m, ".[] | .x")
	s := m.play
	if s.runErr == "" {
		t.Fatal("the failing program must report a runtime error")
	}
	if got := s.result.Text(); got != good {
		t.Errorf("result = %q, want the last good result %q", got, good)
	}
	if !s.playStale() {
		t.Error("a failed run over a good result must be marked stale")
	}
	row := ansi.Strip(m.playInfoRow(200))
	if !strings.HasPrefix(row, "E: ") {
		t.Errorf("the error must render, got %q", row)
	}
	if !strings.Contains(row, "showing the last good result (1 value(s))") {
		t.Errorf("the error row should name the result on screen, got %q", row)
	}
}

// TestJQPlaygroundStaleBannerCycle (#2412) is the issue's acceptance case: a
// valid -> invalid -> valid sequence keeps the result buffer readable
// throughout, and the stale banner appears with the error and clears with the
// next good run.
func TestJQPlaygroundStaleBannerCycle(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":"bar","baz":2}`))
	const banner = "stale \u2014 the query has an error"

	m = setProgram(m, ".foo")
	if got := m.play.result.Text(); got != `"bar"` {
		t.Fatalf("valid result = %q, want %q", got, `"bar"`)
	}
	if v := ansi.Strip(m.render()); strings.Contains(v, banner) {
		t.Errorf("a good result must not show the stale banner, got:\n%s", v)
	}

	m = setProgram(m, ".foo | {a,")
	if got := m.play.result.Text(); got != `"bar"` {
		t.Errorf("an invalid query blanked the result: %q", got)
	}
	v := ansi.Strip(m.render())
	if !strings.Contains(v, banner) {
		t.Errorf("the invalid query should raise the stale banner, got:\n%s", v)
	}
	if !strings.Contains(v, "E: ") || !strings.Contains(v, `"bar"`) {
		t.Errorf("the error and the last good result must both be on screen, got:\n%s", v)
	}

	m = setProgram(m, ".baz")
	if got := m.play.result.Text(); got != "2" {
		t.Errorf("result = %q, want the new value 2", got)
	}
	if m.play.runErr != "" || m.play.playStale() {
		t.Errorf("a good run must clear the error state, got err=%q stale=%v", m.play.runErr, m.play.playStale())
	}
	if v := ansi.Strip(m.render()); strings.Contains(v, banner) {
		t.Errorf("the banner must clear with the next good run, got:\n%s", v)
	}
}

// TestJQPlaygroundInputErrorKeepsResult (#2412): the same holds when the
// *input* stops parsing - the source buffer is edited into invalid JSON - and
// the input error still takes precedence on the info row.
func TestJQPlaygroundInputErrorKeepsResult(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":"bar"}`))
	m = setProgram(m, ".foo")
	good := m.play.result.Text()

	m = drainCmd(m, m.finishPlayParse(playParseDoneMsg{st: m.play, gen: m.play.pgen, err: "not valid JSON"}))
	s := m.play
	if got := s.result.Text(); got != good {
		t.Errorf("a bad input blanked the result: got %q, want %q", got, good)
	}
	if !s.playStale() {
		t.Error("a bad input over a good result must be marked stale")
	}
	if got := m.playErrorLine(); got != "not valid JSON" {
		t.Errorf("the input error must take the info row, got %q", got)
	}
	if v := ansi.Strip(m.render()); !strings.Contains(v, "stale \u2014 the input has an error") {
		t.Errorf("the banner should name the input error, got:\n%s", v)
	}
}

// TestJQPlaygroundZeroValuesWarn (#1978): a program yielding nothing blanks
// the result buffer, so the zero count is the only signal — it renders in
// Warning, not the row's dim Hint.
func TestJQPlaygroundZeroValuesWarn(t *testing.T) {
	m := openJQ(t, playApp(t, `[1,2,3]`))
	m = setProgram(m, ".[] | select(. > 9)")
	if got := m.play.result.Text(); got != "" {
		t.Fatalf("result = %q, want empty", got)
	}
	warn := lipgloss.NewStyle().Foreground(m.pal().Warning)
	if row := m.playInfoRow(120); !strings.Contains(row, warn.Render("Result — 0 value(s)")) {
		t.Errorf("a zero-value summary should render in Warning, got %q", row)
	}
}

// TestJQPlaygroundNarrowInfoRowDropsWholeHints (#1978): on a narrow pane the
// key hints are dropped as whole segments, never clipped mid-word, and the
// input/result summary always survives.
func TestJQPlaygroundNarrowInfoRowDropsWholeHints(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m = setProgram(m, ".")
	// 240 cells: the full hint tail is ~220 wide since the cheatsheet chord
	// joined it (#2382), and the point of the assertion is that nothing is
	// dropped when there is room, not what the exact tally happens to be.
	wide := ansi.Strip(m.playInfoRow(240))
	if !strings.Contains(wide, "esc close") {
		t.Fatalf("a wide row should hold every hint, got %q", wide)
	}
	narrow := ansi.Strip(m.playInfoRow(60))
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
	m := openJQ(t, playApp(t, `{"a":1,"b":2}`))
	s := m.play

	// Two program changes: the first tick is stale by the time it arrives.
	s.result = jqplay.Result{} // drop the seed program's output
	s.program, s.pos = ".a", 2
	stale := m.schedulePlayEval()
	s.program, s.pos = ".b", 2
	fresh := m.schedulePlayEval()

	m = drainCmd(m, stale)
	if len(s.result.Outputs) != 0 {
		t.Fatalf("the superseded tick must not evaluate, got %v", s.result.Outputs)
	}
	m = drainCmd(m, fresh)
	if got := s.result.Text(); got != "2" {
		t.Fatalf("result = %q, want the current program's value", got)
	}

	// A late result carrying an old generation is dropped outright.
	m.finishPlayEval(playEvalDoneMsg{st: s, gen: s.gen - 1, res: jqplay.Result{Outputs: []string{"999"}}})
	if got := s.result.Text(); got != "2" {
		t.Errorf("a stale result overwrote the current one: %q", got)
	}
}

// TestJQPlaygroundDropsResultAfterClose: a run outliving its dialog must not
// resurrect it, and must not panic on the nil state.
func TestJQPlaygroundDropsResultAfterClose(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	s := m.play
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.playOpen() {
		t.Fatal("esc must close the playground")
	}
	m.finishPlayEval(playEvalDoneMsg{st: s, gen: s.gen, res: jqplay.Result{Outputs: []string{"1"}}})
	if _, cmd := m.Update(playParseDoneMsg{st: s}); cmd != nil {
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

	m := openJQ(t, playApp(t, `{"foo":[1,2,3]}`))
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
	m := openJQ(t, playApp(t, `{"foo":[1,2,3]}`))
	m = setProgram(m, "[.foo[] | . * 2]")
	m = drainKey(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})

	if m.playOpen() {
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

	m := openJQ(t, playApp(t, `{"a":1}`))
	m = setProgram(m, "empty")
	m = drainKey(m, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if copied != "untouched" {
		t.Errorf("an empty result must not write to the clipboard, got %q", copied)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if !m.playOpen() {
		t.Error("an empty result must not open a scratch")
	}
}

// TestJQPlaygroundHistory: enter records the program, up/down walk the
// session history, and the history survives closing the dialog.
func TestJQPlaygroundHistory(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1,"b":2}`))
	m = runJQProgram(m, ".a")
	m = runJQProgram(m, ".b")

	// enter left the query line holding the newest entry, so the first ↑
	// skips it (#1973) — a step that changed nothing would read as a dead key.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.play.program; got != ".a" {
		t.Fatalf("first ↑ = %q, want the program before the one on the line", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.play.program; got != ".a" {
		t.Fatalf("↑ at the oldest program = %q, want it to stay", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.play.program; got != ".b" {
		t.Fatalf("↓ = %q, want back to the newer program", got)
	}

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	m = openJQ(t, m)
	// The reopen seeds this file's last valid program, ".b" (#1982), so the
	// first ↑ skips it and offers the entry before it — the history is still
	// there, and it is still the session-wide one.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.play.program; got != ".a" {
		t.Errorf("the history must outlive the dialog, got %q", got)
	}
}

// TestJQPlaygroundHistoryKeepsDraft: browsing away from a half-written
// program and back restores it — ↓ to the live slot must not clear the query
// line (#1973).
func TestJQPlaygroundHistoryKeepsDraft(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1,"b":2}`))
	m = runJQProgram(m, ".a")
	m.play.program, m.play.pos = "", 0
	m = typeInto(m, ".b")
	m = dismissJQPopup(m) // ↑ must reach the history, not the popup (#1979)

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.play.program; got != ".a" {
		t.Fatalf("↑ = %q, want the recorded program", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.play.program; got != ".b" {
		t.Fatalf("↓ = %q, want the draft back", got)
	}
	if got := m.play.pos; got != len(".b") {
		t.Errorf("the caret should sit at the end of the restored draft, got %d", got)
	}
	if got := m.play.result.Text(); got != "2" {
		t.Errorf("the restored draft must be evaluated again, got %q", got)
	}
	// A ↓ with nothing to come back from leaves the line alone.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.play.program; got != ".b" {
		t.Errorf("↓ at the live slot = %q, want the line untouched", got)
	}
}

// TestJQPlaygroundHistorySkipsSeededProgram: reopening over the same caret
// seeds the program that was last run, so the first ↑ must offer the one
// before it rather than re-typing what is already on the line (#1973).
func TestJQPlaygroundHistorySkipsSeededProgram(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1,"b":2}`))
	m = runJQProgram(m, ".a")
	m.play.program, m.play.pos = "", 0
	m = typeInto(m, ".b")
	m = dismissJQPopup(m) // the first esc would only dismiss the popup (#1979)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	m = openJQ(t, m)
	// Reopening seeds the file's last valid program, ".b" (#1982) — which is
	// exactly the newest history entry the first ↑ has to step over.
	if got := m.play.program; got != ".b" {
		t.Fatalf("reopen seeded %q, want the last program run on the file", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.play.program; got != ".a" {
		t.Errorf("↑ over the seeded program = %q, want the entry before it", got)
	}
}

// TestJQPlaygroundResultNavigation: tab moves the keyboard into the result
// buffer, where the full editor keymap navigates — G jumps to the last line
// and scrolls the viewport; tab returns to the query line.
func TestJQPlaygroundResultNavigation(t *testing.T) {
	m := openJQ(t, playApp(t, "null"))
	m = setProgram(m, "[range(100)]")
	ed := m.play.resultEd
	if got := ed.LineCount(); got < 100 {
		t.Fatalf("the fixture must overflow the pane, got %d rows", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if !m.play.bufFocus {
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
	if m.play.bufFocus {
		t.Error("tab must return the focus to the query line")
	}
}

// TestJQPlaygroundInlineEnterExit is #1970's core acceptance case: the mode
// mounts in the queried pane (no floating dialog), and esc restores the
// original buffer unchanged and editable.
func TestJQPlaygroundInlineEnterExit(t *testing.T) {
	const body = `{"foo":[1,2,3]}`
	m := playApp(t, body)
	m = openJQ(t, m)
	if got, want := m.play.paneKey, m.activeWS().Panes.Focused(); got != want {
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
	if m.playOpen() {
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
	m := openJQ(t, playApp(t, `{"name":"ike"}`))

	m = drainKey(m, tea.KeyPressMsg{Code: 'a', Mod: tea.ModSuper | tea.ModShift})
	if !m.palette.IsOpen() {
		t.Fatal("cmd+shift+a in the playground must open Search Everywhere")
	}
	if !m.playOpen() {
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
	if !m.play.bufFocus {
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
	m.play.program, m.play.pos = "", 0
	m = typeInto(m, ".name")
	if m.play.program != ".name" {
		t.Fatalf("typing must stay with the query line, got %q", m.play.program)
	}
	if m.palette.IsOpen() {
		t.Fatal("plain typing must not dispatch global bindings")
	}
	m = dismissJQPopup(m) // typing opened the completion popup (#1979)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.playOpen() {
		t.Fatal("esc must keep closing the playground")
	}
}

// TestJQPlaygroundResultReadOnly: the result buffer refuses every mutation —
// typing in query focus edits only the program, and edit keys in buffer
// focus bounce off the read-only flag.
func TestJQPlaygroundResultReadOnly(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m = setProgram(m, ".")
	ed := m.play.resultEd
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
	if m.play.result.Text() != before {
		t.Error("the evaluation result itself must be untouched")
	}
}

// TestJQPlaygroundResultSelection: visual selection works in the result
// buffer, esc first leaves visual mode like in any buffer, and only a second
// esc — from resting normal mode — closes the playground.
func TestJQPlaygroundResultSelection(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":[1,2,3]}`))
	m = setProgram(m, ".foo[]")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = drainKey(m, tea.KeyPressMsg{Code: 'V', Text: "V"})
	m = drainKey(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	sel, has := m.play.resultEd.SelectionText()
	if !has || strings.TrimSpace(sel) == "" {
		t.Fatalf("visual selection must work in the result buffer, got %q", sel)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.playOpen() {
		t.Fatal("esc in visual mode must only leave the selection, not the mode")
	}
	if _, has := m.play.resultEd.SelectionText(); has {
		t.Error("esc must have collapsed the visual selection")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.playOpen() {
		t.Error("esc from resting normal mode must close the playground")
	}
}

// playTestClip is a fake system clipboard for the result buffer's `+` register.
type playTestClip struct{ text string }

func (c *playTestClip) Read() (string, error) { return c.text, nil }
func (c *playTestClip) Write(s string) error  { c.text = s; return nil }

// playCopyKey is the app keymap's editor.copy chord as a key event: cmd+c on
// macOS, folded to ctrl+c everywhere else (keymap.NormalizeKey).
func playCopyKey() tea.KeyPressMsg {
	if runtime.GOOS == "darwin" {
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModMeta}
	}
	return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
}

// TestJQPlaygroundCopyChordCopiesSelection (#1980): the copy chord writes the
// result buffer's visual selection to the system clipboard, like in a normal
// read-only buffer — the modal routing must not swallow it.
func TestJQPlaygroundCopyChordCopiesSelection(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":[1,2,3]}`))
	m = setProgram(m, ".foo[]")
	clip := &playTestClip{}
	m.play.resultEd.SetClipboard(clip)

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = drainKey(m, tea.KeyPressMsg{Code: 'V', Text: "V"})
	m = drainKey(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if _, has := m.play.resultEd.SelectionText(); !has {
		t.Fatal("the fixture needs a visual selection")
	}
	m = drainKey(m, playCopyKey())
	if !strings.Contains(clip.text, "1") || !strings.Contains(clip.text, "2") {
		t.Fatalf("clipboard = %q, want the selected lines", clip.text)
	}
	if strings.Contains(clip.text, "3") {
		t.Errorf("clipboard = %q, must hold only the selection, not the whole result", clip.text)
	}
	if !m.playOpen() {
		t.Error("copying must leave the playground open")
	}
}

// TestJQPlaygroundMenuCopyReachesResultBuffer (#1980): the Edit menu's copy
// dispatches editor.ActionMsg — while the playground pane is focused it must
// act on the substitute result buffer, not the pane's hidden document.
func TestJQPlaygroundMenuCopyReachesResultBuffer(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":[1,2,3]}`))
	m = setProgram(m, ".foo[]")
	clip := &playTestClip{}
	m.play.resultEd.SetClipboard(clip)

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
	m := playApp(t, `{"foo":[1,2,3]}`)
	playKey := m.activeWS().Panes.Focused()
	// A second editor pane to work in while the result stays visible.
	m.SplitFocused(layout.ZoneRight)
	otherKey := m.activeWS().Panes.Focused()
	m.setFocus(playKey)
	m = openJQ(t, m)
	m = setProgram(m, ".foo[]")
	program := m.play.program

	// The spatial focus move escapes the playground pane instead of being
	// swallowed by the modal routing.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl})
	if !m.playOpen() {
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
	if got := m.play.program; got != program {
		t.Fatalf("query line = %q, keys for the other pane must not reach it", got)
	}
	if got := m.play.result.Text(); got != "1\n2\n3" {
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
	if got := m.activeWS().Panes.Focused(); got != playKey {
		t.Fatalf("focus = %q, want back on the playground pane %q", got, playKey)
	}
	m = typeInto(m, " ")
	if got := m.play.program; got != program+" " {
		t.Fatalf("query line = %q, refocusing must resume it", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.playOpen() {
		t.Error("esc in the playground pane must still close the mode")
	}
}

// TestJQPlaygroundSurvivesClickIntoOtherPane (#1980): a mouse click into
// another pane moves the focus and leaves the playground mounted.
func TestJQPlaygroundSurvivesClickIntoOtherPane(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":[1,2,3]}`))
	m = setProgram(m, ".foo[]")
	r, ok := m.lay.Panes[pane.ExplorerKey]
	if !ok {
		t.Fatal("the explorer pane must be laid out")
	}
	tm, cmd := m.Update(tea.MouseClickMsg{X: r.X + r.W/2, Y: r.Y + r.H/2, Button: tea.MouseLeft})
	m = drainCmd(tm.(Model), cmd)
	if !m.playOpen() {
		t.Fatal("a click into another pane must not close the playground")
	}
	if got := m.activeWS().Panes.Focused(); got != pane.ExplorerKey {
		t.Fatalf("focus = %q, want the clicked explorer", got)
	}
	if got := m.play.result.Text(); got != "1\n2\n3" {
		t.Errorf("result = %q, must survive the click", got)
	}
}

// TestJQPlaygroundClosesWithHostingPane (#1980): the mode survives focus
// changes, so its pane can now be closed from elsewhere — the playground must
// die with it instead of dangling over a removed key.
func TestJQPlaygroundClosesWithHostingPane(t *testing.T) {
	m := playApp(t, `{"foo":[1,2,3]}`)
	playKey := m.activeWS().Panes.Focused()
	m.SplitFocused(layout.ZoneRight)
	m.setFocus(playKey)
	m = openJQ(t, m)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl})
	m.closePane(playKey)
	if m.playOpen() {
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
	if got := m.play.source; got != "HTTP response" {
		t.Fatalf("source = %q, want the response pane", got)
	}
	if got := m.play.paneKey; got != pane.HTTPKey {
		t.Fatalf("the mode must mount in the response pane, got %q", got)
	}
	m = setProgram(m, "[.items[].id]")
	if got := strings.Join(strings.Fields(m.play.result.Text()), ""); got != "[7,8]" {
		t.Errorf("result = %q, want the ids from the response body", got)
	}
}

// TestJQPlaygroundQueriesSelection: with a visual selection open, the
// selected lines are the input — an embedded JSON blob in a log file is
// queryable without extracting it first.
func TestJQPlaygroundQueriesSelection(t *testing.T) {
	m := playApp(t, "{\"a\":1}\nnot json at all\n")
	// Select the first line in visual-line mode.
	m = drainKey(m, tea.KeyPressMsg{Code: 'V', Text: "V"})
	m = openJQ(t, m)
	if !strings.Contains(m.play.source, "selection") {
		t.Fatalf("source = %q, want the selection", m.play.source)
	}
	if m.play.inputErr != "" {
		t.Fatalf("the selected JSON must parse, got %q", m.play.inputErr)
	}
	m = setProgram(m, ".a")
	if got := m.play.result.Text(); got != "1" {
		t.Errorf("result = %q, want the selected object's field", got)
	}
}

// TestJQPlaygroundHighlightsQuery: the query line is colored by the jq
// scanner, so a path and a string literal in the same program do not render
// in one flat color.
func TestJQPlaygroundHighlightsQuery(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":"x"}`))
	m = setProgram(m, `.a == "x"`)
	row := m.playQueryRow(60)
	if ansi.Strip(row) == row {
		t.Fatalf("the query line should carry color, got %q", row)
	}
	styles := m.playKindStyles()
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
	tm, cmd := m.Update(OpenPlaygroundMsg{})
	m = drainCmd(tm.(Model), cmd)
	if m.playOpen() {
		t.Fatal("with no JSON at hand the playground must not open")
	}
}

// TestJQPlaygroundPasteFlattens: a bracketed paste lands in the query line as
// one line — a multi-line program must not smuggle newlines into it.
func TestJQPlaygroundPasteFlattens(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m.play.program, m.play.pos = "", 0
	tm, cmd := m.Update(tea.PasteMsg{Content: ".a\n| tostring"})
	m = drainCmd(tm.(Model), cmd)
	if strings.Contains(m.play.program, "\n") {
		t.Fatalf("the query line kept a newline: %q", m.play.program)
	}
	if m.play.result.Err != "" {
		t.Errorf("the flattened program should still compile, got %q", m.play.result.Err)
	}
}

// runJQProgram clears the query line, types program and records it with enter,
// the way a user runs one. Typing may have opened the completion popup
// (#1979), which owns enter while it shows; it is dismissed first so enter
// keeps its record-and-run meaning.
func runJQProgram(m Model, program string) Model {
	m.play.program, m.play.pos = "", 0
	m = typeInto(m, program)
	m = dismissJQPopup(m)
	return drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
}

// TestJQPlaygroundHistoryCrossBuffer (#1977): the program history is one
// session-wide list. A program run over file A is offered by ↑ when the
// playground is reopened over a *different* file, and one run over an HTTP
// response body joins the same list.
func TestJQPlaygroundHistoryCrossBuffer(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
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
	if got := m.play.program; got != ".a" {
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
	if got := m.play.paneKey; got != pane.HTTPKey {
		t.Fatalf("the mode must mount in the response pane, got %q", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.play.program; got != ".b" {
		t.Fatalf("↑ over the response pane = %q, want the newest editor program", got)
	}
	m = runJQProgram(m, ".items")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	m.setFocus(m.recentEditor)
	m = openJQ(t, m)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.play.program; got != ".items" {
		t.Errorf("↑ back in an editor = %q, want the program run on the response", got)
	}
}

// TestJQPlaygroundHistorySurvivesReopen (#1977): the playground is reachable
// from the Tools menu while one is already open, and reopening replaces the
// mode. The programs the replaced one recorded must not go with it — the
// history is the root model's, not the mode's.
func TestJQPlaygroundHistorySurvivesReopen(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m = runJQProgram(m, ".a")

	m = openJQ(t, m) // reopened without esc: the mode is replaced
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.play.program; got != ".a" {
		t.Errorf("↑ after reopening = %q, want the program recorded before it", got)
	}
}

// playLongProgram is a pipeline of the shape the issue reports (#2032): far
// wider than a pane, with `|` stages to break at.
const playLongProgram = `.hits.hits[]._source | .keyword as $keyword | .ser[] | select(.domain == "universal-search-box.com") | {$keyword, type: .kind, url: .link}`

// toggleJQView presses the chord bound to json.jqQueryView (default
// ctrl+alt+e), the way a user reaches the full-query view.
func toggleJQView(m Model) Model {
	return drainKey(m, tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl | tea.ModAlt})
}

// playQueryText renders the query rows and strips the color, so an assertion can
// talk about the program that is actually on screen.
func playQueryText(m Model, width int) string {
	var b strings.Builder
	for _, row := range m.playQueryRows(width) {
		b.WriteString(ansi.Strip(row))
	}
	return b.String()
}

// TestJQPlaygroundExpandedQueryShowsWholeProgram is the issue's acceptance
// case (#2032): a program wider than the pane is cut on the one-line view and
// fully readable after the toggle — without leaving the playground and
// without changing the program.
func TestJQPlaygroundExpandedQueryShowsWholeProgram(t *testing.T) {
	m := openJQ(t, playApp(t, `{"hits":{"hits":[]}}`))
	m = setProgram(m, playLongProgram)

	const width = 80
	if got := playQueryText(m, width); strings.Contains(got, ".hits.hits[]") {
		t.Fatalf("the one-line view cannot show the whole program, got %q", got)
	}
	m = toggleJQView(m)
	if !m.play.expanded {
		t.Fatal("the json.jqQueryView chord must expand the query view")
	}
	if got := m.play.program; got != playLongProgram {
		t.Fatalf("the view must not touch the program, got %q", got)
	}
	got := strings.ReplaceAll(playQueryText(m, width), " ", "")
	want := strings.ReplaceAll(playLongProgram, " ", "")
	if !strings.Contains(got, want) {
		t.Errorf("the expanded view must show the whole program, got %q", got)
	}
	// Toggling back restores the resting one-line layout.
	m = toggleJQView(m)
	if m.play.expanded || len(m.playQueryRows(width)) != 1 {
		t.Error("the toggle must fold the view back to one row")
	}
}

// TestJQPlaygroundExpandedQueryKeepsHighlighting (#2032): the wrapped rows are
// colored by the same scanner the one-line view uses.
func TestJQPlaygroundExpandedQueryKeepsHighlighting(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":"x"}`))
	m = setProgram(m, `.aaaaaaaaaa | select(.b == "x") | .ccccccccc | map(.d)`)
	m = toggleJQView(m)
	rows := m.playQueryRows(40)
	if len(rows) < 2 {
		t.Fatalf("the program should wrap over several rows, got %d", len(rows))
	}
	joined := strings.Join(rows, "")
	if ansi.Strip(joined) == joined {
		t.Fatal("the expanded rows should carry color")
	}
	styles := m.playKindStyles()
	for _, want := range []string{
		styles[jqplay.KindString].Render("x"),
		styles[jqplay.KindFunc].Render("m"),
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the expanded view should render %q, got %q", want, joined)
		}
	}
}

// playResultHeight is how many rows the substitute result buffer renders — the
// editor keeps its height private, and the rendered row count is what the
// pane geometry is about anyway.
func playResultHeight(m Model) int {
	return len(strings.Split(m.play.resultEd.View(), "\n"))
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
	m := openJQ(t, playApp(t, `{"items":[`+strings.Join(items, ",")+`]}`))
	m = setProgram(m, `.items[] | select(.n >= 0) | {n: .n, doubled: (.n * 2), label: ("row-" + (.n | tostring))}`)
	key := m.play.paneKey
	r, ok := m.lay.Panes[key]
	if !ok {
		t.Fatal("the hosting pane must have a rect")
	}
	width := r.W - paneChromeW
	if got := m.playHeaderRowsFor(key); got != playHeaderRows {
		t.Fatalf("the resting header is %d rows, want %d", got, playHeaderRows)
	}
	before := playResultHeight(m)
	if got, want := len(strings.Split(m.playInlineBody(width), "\n")), paneInterior(r.H, paneChromeH); got != want {
		t.Fatalf("the resting pane body renders %d rows, want %d", got, want)
	}

	m = toggleJQView(m)
	rows := m.playHeaderRowsFor(key)
	if rows <= playHeaderRows {
		t.Fatalf("the expanded header must grow, got %d rows", rows)
	}
	if got, want := len(m.playQueryRows(width))+playInfoRows, rows; got != want {
		t.Errorf("rendered %d header rows, geometry reserved %d", got, want)
	}
	if got, want := playResultHeight(m), before-(rows-playHeaderRows); got != want {
		t.Errorf("result buffer is %d rows, want %d", got, want)
	}
	if got, want := len(strings.Split(m.playInlineBody(width), "\n")), paneInterior(r.H, paneChromeH); got != want {
		t.Errorf("the expanded pane body renders %d rows, want %d", got, want)
	}
}

// TestJQPlaygroundExpandedQueryCaps (#2032): a program too long even for the
// expanded view keeps the result visible, windows around the cursor's row and
// says that it is still cut.
func TestJQPlaygroundExpandedQueryCaps(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m = setProgram(m, strings.Repeat(".aaaaaaaa | ", 40)+".a")
	m = toggleJQView(m)
	rows := m.playQueryRows(60)
	if len(rows) > playMaxQueryRows {
		t.Fatalf("the expanded view must cap at %d rows, got %d", playMaxQueryRows, len(rows))
	}
	if !strings.Contains(ansi.Strip(strings.Join(rows, "")), "…") {
		t.Error("a capped expanded view must mark that it is cut")
	}
	// The cursor sits at the end of the program, so the last rows are the ones
	// on screen — the window follows the caret like the one-line view.
	if got := ansi.Strip(rows[0]); !strings.HasPrefix(got, "> jq: …") {
		t.Errorf("the capped view must window around the cursor, got %q", got)
	}
	if got := strings.TrimRight(playQueryText(m, 60), " "); !strings.HasSuffix(got, "| .a") {
		t.Errorf("the cursor's row must be on screen, got %q", got)
	}
	if !strings.Contains(ansi.Strip(m.playInfoRow(60)), "query cut") {
		t.Error("the info row must still flag the cut")
	}
}

// TestJQPlaygroundQueryCutHint (#2032): a cut program is flagged on the info
// row, and the hint names the chord that shows it whole — from the query line
// and from the result buffer alike.
func TestJQPlaygroundQueryCutHint(t *testing.T) {
	m := openJQ(t, playApp(t, `{"hits":{"hits":[]}}`))
	m = setProgram(m, ".a")
	if got := ansi.Strip(m.playInfoRow(90)); strings.Contains(got, "query cut") {
		t.Errorf("a program that fits is not cut, got %q", got)
	}
	m = setProgram(m, playLongProgram)
	if got := ansi.Strip(m.playInfoRow(90)); !strings.Contains(got, "query cut") {
		t.Errorf("a cut program must be flagged, got %q", got)
	}
	if got := ansi.Strip(m.playInfoRow(200)); !strings.Contains(got, "ctrl+alt+e full query") {
		t.Errorf("the info row must document the full-query chord, got %q", got)
	}
	m = toggleJQView(m)
	if got := ansi.Strip(m.playInfoRow(200)); !strings.Contains(got, "ctrl+alt+e one-line query") {
		t.Errorf("the expanded view must document the way back, got %q", got)
	}
	// The result buffer's hint row documents it too — the toggle works from
	// there as well.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if !m.play.bufFocus {
		t.Fatal("tab must move the focus into the result buffer")
	}
	if got := ansi.Strip(m.playInfoRow(200)); !strings.Contains(got, "ctrl+alt+e") {
		t.Errorf("the result buffer's hints must document the toggle, got %q", got)
	}
	m = toggleJQView(m)
	if m.play.expanded {
		t.Error("the toggle must work from the result buffer too")
	}
}

// TestJQPlaygroundQueryViewCommand (#2032): the palette / Tools menu command
// drives the same toggle as the chord, and is inert with no playground open.
func TestJQPlaygroundQueryViewCommand(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m = setProgram(m, playLongProgram)
	tm, cmd := m.Update(TogglePlaygroundQueryViewMsg{})
	m = drainCmd(tm.(Model), cmd)
	if !m.play.expanded {
		t.Fatal("json.jqQueryView must expand the query view")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	tm, cmd = m.Update(TogglePlaygroundQueryViewMsg{})
	m = drainCmd(tm.(Model), cmd)
	if m.playOpen() {
		t.Error("the command must not open a playground of its own")
	}
}

// playKeys feeds a run of plain keys into the model one at a time — the vim
// fold commands are two-key sequences (z then a), so a test types them the
// way a user does.
func playKeys(m Model, keys string) Model {
	for _, r := range keys {
		m = drainKey(m, tea.KeyPressMsg{Text: string(r), Code: r})
	}
	return m
}

// TestJQResultFoldsAtCursor is the issue's acceptance case (#2029): za in the
// result buffer collapses the object under the cursor to one row carrying a
// placeholder with its key count, and hides its body.
func TestJQResultFoldsAtCursor(t *testing.T) {
	m := openJQ(t, playApp(t, `{"spec":{"image":"ike","tag":"v1"},"n":3}`))
	m = setProgram(m, ".")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab}) // keyboard into the result
	m = playKeys(m, "jjza")                            // onto the "spec" line, fold it

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
	m := openJQ(t, playApp(t, `{"spec":{"ports":[80,443,8080]}}`))
	m = setProgram(m, ".")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = playKeys(m, "zM") // every fold closed
	if view := ansi.Strip(m.render()); !strings.Contains(view, "⋯ 1 key }") || strings.Contains(view, `"ports"`) {
		t.Fatalf("zM must collapse the outermost value, got:\n%s", view)
	}
	m = playKeys(m, "zo") // one level open again
	view := ansi.Strip(m.render())
	if !strings.Contains(view, `"spec": { ⋯ 1 key }`) {
		t.Fatalf("zo must reveal one level, with the node inside it still folded, got:\n%s", view)
	}
	m = playKeys(m, "jzo") // and the node inside it opens on its own
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
	m := openJQ(t, playApp(t, `{"spec":{"image":"ike","tag":"v1"},"n":3}`))
	m = setProgram(m, ".")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = playKeys(m, "jjza")

	m = setProgram(m, ".spec")
	view := ansi.Strip(m.render())
	if strings.Contains(view, "⋯") {
		t.Fatalf("a new query must leave no fold behind, got:\n%s", view)
	}
	if !strings.Contains(view, `"image": "ike"`) {
		t.Fatalf("the new result must render in full, got:\n%s", view)
	}
	// The new result folds on its own terms: one object, two keys.
	m = playKeys(m, "za")
	if view := ansi.Strip(m.render()); !strings.Contains(view, "⋯ 2 keys }") {
		t.Errorf("the new result must be foldable, got:\n%s", view)
	}
}

// TestJQResultFoldHintsAdvertised: the info row names the fold keys while the
// result buffer holds the keyboard — the mode's own help line is where the
// binding is documented (#2029).
func TestJQResultFoldHintsAdvertised(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m = setProgram(m, ".")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	hints := strings.Join(m.playHints(), " · ")
	if !strings.Contains(hints, "za fold") || !strings.Contains(hints, "zM/zR fold all") {
		t.Fatalf("the result buffer's hints must name the fold keys, got %q", hints)
	}
	// The row drops trailing hints whole on a narrow pane, so the assertion
	// that they reach the screen is made against a wide one.
	if row := ansi.Strip(m.playInfoRow(200)); !strings.Contains(row, "za fold") {
		t.Errorf("the info row must show the fold hint, got:\n%s", row)
	}
}

// playMultiLineProgram is a pipeline that wraps over more than one row in the
// test pane, with a *shorter* row above the last one — so a vertical motion
// over it has a goal column to lose (#2038).
const playMultiLineProgram = `.items[] | select(.n >= 1) | {a: .n, b: (.n * 2), c: (.n * 3), d: (.n * 4)} | .a`

// playQueryLines is the wrapped program as the hosting pane lays it out — the
// rows a cursor motion moves through.
func playQueryLines(t *testing.T, m Model) []jqplay.Line {
	t.Helper()
	w, ok := m.playPaneQueryWidth()
	if !ok {
		t.Fatal("the hosting pane must be laid out")
	}
	return jqplay.Wrap(m.play.program, w)
}

// playMultiLineApp opens the playground over a two-item document with
// playMultiLineProgram on the query line and the multi-line view up.
func playMultiLineApp(t *testing.T) (Model, []jqplay.Line) {
	t.Helper()
	m := openJQ(t, playApp(t, `{"items":[{"n":1},{"n":2}]}`))
	m = setProgram(m, playMultiLineProgram)
	m = toggleJQView(m)
	lines := playQueryLines(t, m)
	if len(lines) < 2 {
		t.Fatalf("setup: the program must wrap over several rows, got %d", len(lines))
	}
	last, prev := lines[len(lines)-1], lines[len(lines)-2]
	if last.End-last.Start <= prev.End-prev.Start {
		t.Fatalf("setup: the caret's row must be the longer one, got %+v", lines)
	}
	return m, lines
}

// TestJQPlaygroundMultiLineWalksRows is the issue's acceptance case (#2038):
// in the multi-line view ↑/↓ move the caret between the program's rows — with
// the goal column surviving a shorter row on the way — instead of walking the
// history, and the program is never touched by a motion.
func TestJQPlaygroundMultiLineWalksRows(t *testing.T) {
	m, lines := playMultiLineApp(t)
	end := m.play.pos
	row, _ := jqplay.RowCol(lines, end)
	if row != len(lines)-1 {
		t.Fatalf("setup: the caret starts on the last row, got %d", row)
	}

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.play.program; got != playMultiLineProgram {
		t.Fatalf("↑ must move the caret, not the program: %q", got)
	}
	up, col := jqplay.RowCol(lines, m.play.pos)
	if up != row-1 {
		t.Fatalf("↑ put the caret on row %d, want %d", up, row-1)
	}
	if want := lines[up].End - lines[up].Start; col != want {
		t.Errorf("the caret sits in column %d of a %d-wide row, want its end", col, want)
	}
	if m.play.histIdx != -1 {
		t.Error("a row motion must not enter the history")
	}

	// The goal column is remembered across the shorter row: ↓ lands back on
	// the column ↑ left, not on the one the short row clamped it to.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.play.pos; got != end {
		t.Errorf("↓ back = %d, want the goal column restored to %d", got, end)
	}
	// ↓ on the last row has no row to go to, so it hands over to the history —
	// which is at the live slot with nothing newer, and leaves the line alone.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.play.program; got != playMultiLineProgram {
		t.Errorf("↓ at the last row = %q, want the program untouched", got)
	}
	// The one-line view keeps the arrows on the history (#1973).
	m = toggleJQView(m)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.play.pos; got != end {
		t.Errorf("↑ in the one-line view moved the caret to %d", got)
	}
}

// TestJQPlaygroundMultiLineEditsAnyRow (#2038): home/end are row-local once
// there are rows, the caret edits where it stands — several rows up from the
// end of the program — and the edit runs live like any other keystroke.
func TestJQPlaygroundMultiLineEditsAnyRow(t *testing.T) {
	m, lines := playMultiLineApp(t)
	if got := m.play.result.Text(); got != "1\n2" {
		t.Fatalf("setup: result = %q, want both items", got)
	}

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp}) // onto the first row
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyHome})
	if got := m.play.pos; got != lines[0].Start {
		t.Fatalf("home = %d, want the row's start %d", got, lines[0].Start)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnd})
	if got := m.play.pos; got != lines[0].End {
		t.Fatalf("end = %d, want the row's end %d", got, lines[0].End)
	}

	// Typing at the caret inserts a whole stage in the middle of the program.
	m = typeInto(m, " select(.n > 1) |")
	m = dismissJQPopup(m)
	want := playMultiLineProgram[:lines[0].End] + " select(.n > 1) |" + playMultiLineProgram[lines[0].End:]
	if got := m.play.program; got != want {
		t.Fatalf("program = %q, want the stage inserted at the caret: %q", got, want)
	}
	if got := m.play.result.Text(); got != "2" {
		t.Errorf("result = %q, want the edited program's live result", got)
	}
	// ctrl+home / ctrl+end still reach the ends of the whole program.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyHome, Mod: tea.ModCtrl})
	if got := m.play.pos; got != 0 {
		t.Errorf("ctrl+home = %d, want the program's start", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	if got := m.play.pos; got != len([]rune(want)) {
		t.Errorf("ctrl+end = %d, want the program's end", got)
	}
}

// TestJQPlaygroundMultiLineHistoryKeys (#2038): with ↑/↓ on the rows, the
// history moves to alt+↑/alt+↓ — reachable from any row — and a plain ↑ on the
// first row still hands over to it, the way a multi-line shell prompt does.
func TestJQPlaygroundMultiLineHistoryKeys(t *testing.T) {
	m := openJQ(t, playApp(t, `{"items":[{"n":1},{"n":2}]}`))
	m = runJQProgram(m, ".items")
	m = setProgram(m, playMultiLineProgram)
	m = toggleJQView(m)
	if lines := playQueryLines(t, m); len(lines) < 2 {
		t.Fatalf("setup: the program must wrap, got %d row(s)", len(lines))
	}

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModAlt})
	if got := m.play.program; got != ".items" {
		t.Fatalf("alt+↑ = %q, want the recorded program", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModAlt})
	if got := m.play.program; got != playMultiLineProgram {
		t.Fatalf("alt+↓ = %q, want the draft back", got)
	}

	// Walk to the first row, then off its top: the plain ↑ falls through.
	for i := 0; i < len(playQueryLines(t, m)); i++ {
		m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	}
	if got := m.play.program; got != ".items" {
		t.Errorf("↑ off the first row = %q, want the history entry", got)
	}
}

// TestJQPlaygroundMultiLineClickPlacesCaret (#2038): a click on a query row
// puts the caret on the clicked cell — the way to reach a stage far down a
// long pipeline — and returns the keyboard to the query line.
func TestJQPlaygroundMultiLineClickPlacesCaret(t *testing.T) {
	m, lines := playMultiLineApp(t)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab}) // keyboard in the result
	r, ok := m.lay.Panes[m.play.paneKey]
	if !ok {
		t.Fatal("the hosting pane must have a rect")
	}
	const col = 3
	x := r.X + paneContentX + playQueryPrefixW + col
	y := r.Y + paneContentY + 1 // the second query row
	tm, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m = drainCmd(tm.(Model), cmd)
	if m.play.bufFocus {
		t.Fatal("a click on the query header must return the keyboard to the query line")
	}
	if got, want := m.play.pos, lines[1].Start+col; got != want {
		t.Errorf("the click put the caret at %d, want %d", got, want)
	}
	// Typing lands where the click put it.
	m = typeInto(m, "x")
	m = dismissJQPopup(m)
	if got := []rune(m.play.program)[lines[1].Start+col]; got != 'x' {
		t.Errorf("the rune at the clicked cell is %q, want the typed one", got)
	}
}

// TestJQPlaygroundMultiLineWindowFollowsCaret (#2038): a program past the row
// cap scrolls under the caret, and the header the geometry reserves still
// matches the header that is drawn.
func TestJQPlaygroundMultiLineWindowFollowsCaret(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m = setProgram(m, strings.Repeat(".aaaaaaaa | ", 40)+".a")
	m = toggleJQView(m)
	key := m.play.paneKey
	r, ok := m.lay.Panes[key]
	if !ok {
		t.Fatal("the hosting pane must have a rect")
	}
	width := paneInterior(r.W, paneChromeW)
	before := playQueryText(m, width)
	for i := 0; i < playMaxQueryRows+4; i++ {
		m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	}
	if after := playQueryText(m, width); after == before {
		t.Error("↑ past the top of the window must scroll the rows")
	}
	lines, rows, start := m.playQueryWindow(width)
	if cur := jqplay.LineAt(lines, m.play.pos); cur < start || cur >= start+rows {
		t.Errorf("the caret's row %d is outside the window [%d,%d)", cur, start, start+rows)
	}
	if got, want := len(m.playQueryRows(width))+playInfoRows, m.playHeaderRowsFor(key); got != want {
		t.Errorf("rendered %d header rows, geometry reserved %d", got, want)
	}
	if rows > playMaxQueryRows {
		t.Errorf("the scrolled window is %d rows, want at most %d", rows, playMaxQueryRows)
	}
}

// TestJQPlaygroundMultiLineKeepsOneLineProgram (#2038): the rows are a display
// and editing device only — the program never grows a line break, so the
// history, the saved filters (#1995) and the seeding all keep working on one
// line, and toggling the view moves neither the program nor the caret.
func TestJQPlaygroundMultiLineKeepsOneLineProgram(t *testing.T) {
	m, _ := playMultiLineApp(t)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	pos := m.play.pos

	m = toggleJQView(m)
	if m.play.program != playMultiLineProgram || m.play.pos != pos {
		t.Fatalf("the toggle moved the program or the caret: %q at %d", m.play.program, m.play.pos)
	}
	m = toggleJQView(m)
	if m.play.program != playMultiLineProgram || m.play.pos != pos {
		t.Fatalf("the toggle back moved the program or the caret: %q at %d", m.play.program, m.play.pos)
	}

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	got, ok := m.play.hist.At(0)
	if !ok || got != playMultiLineProgram {
		t.Fatalf("history holds %q (ok=%v), want the program as one line", got, ok)
	}
	m = saveFilter(t, m, "multi line", false)
	f, ok := loadPlayFilters(jqplay.DialectJQ, jqplay.ScopeProject).Get("multi line")
	if !ok || f.Program != playMultiLineProgram {
		t.Errorf("saved filter = %+v (ok=%v), want the program as one line", f, ok)
	}
	if strings.Contains(m.play.program, "\n") {
		t.Error("the query line must never hold a line break")
	}
}

// TestJQPlaygroundMultiLineHints (#2038): the info row says which meaning the
// arrows have in the view in front of the user.
func TestJQPlaygroundMultiLineHints(t *testing.T) {
	m, _ := playMultiLineApp(t)
	got := ansi.Strip(m.playInfoRow(240))
	if !strings.Contains(got, "↑/↓ lines") || !strings.Contains(got, "alt+↑/↓ history") {
		t.Errorf("the multi-line hints = %q, want the row and history keys", got)
	}
	m = toggleJQView(m)
	if got := ansi.Strip(m.playInfoRow(240)); !strings.Contains(got, "↑/↓ history") {
		t.Errorf("the one-line hints = %q, want the history keys", got)
	}
}
