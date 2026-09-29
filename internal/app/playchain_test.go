package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/jqplay"
)

// playchain_test.go covers the playground's result chaining (#2795): chain
// and back restore snapshot and program, the breadcrumb, jq and yq, the xmq
// refusal, the paused follow, the park/resume across a project switch and
// the default chords.

// chainPlay dispatches playground.chainResult.
func chainPlay(m Model) Model {
	tm, cmd := m.Update(ChainPlayResultMsg{})
	return drainCmd(tm.(Model), cmd)
}

// chainBack dispatches playground.chainBack.
func chainBack(m Model) Model {
	tm, cmd := m.Update(ChainPlayBackMsg{})
	return drainCmd(tm.(Model), cmd)
}

// compact is the installed result with whitespace removed.
func compact(m Model) string {
	return strings.Join(strings.Fields(m.play.result.Text()), "")
}

const chainDoc = `{"data":{"items":[{"ok":true,"n":1},{"ok":false,"n":2},{"ok":true,"n":3}]}}`

// TestPlayChainAndBackRestoreEverything is the issue's acceptance case:
// chain, run, chain again, back twice → the original snapshot and program.
func TestPlayChainAndBackRestoreEverything(t *testing.T) {
	m := openJQ(t, dismissOnboarding(playApp(t, chainDoc)))
	m = setProgram(m, ".data.items")
	m.play.program.Cur = 3
	rootIn, rootText := m.play.input, m.play.srcText

	m = chainPlay(m)
	s := m.play
	if len(s.chain) != 1 || s.program.Text != "." {
		t.Fatalf("after chain: depth %d, program %q", len(s.chain), s.program.Text)
	}
	if s.input == rootIn || s.input.Origin() != "chained" {
		t.Fatalf("the chained input must be the result, origin %q", s.input.Origin())
	}
	if got := compact(m); got != `[{"n":1,"ok":true},{"n":2,"ok":false},{"n":3,"ok":true}]` {
		t.Fatalf("`.` over the chained input = %q", got)
	}
	if s.changes != nil {
		t.Errorf("a level switch must not mark changes, got %v", s.changes)
	}

	m = setProgram(m, "map(select(.ok))")
	m = chainPlay(m)
	m = setProgram(m, ".[].n")
	if got := compact(m); got != "13" {
		t.Fatalf("second level result = %q, want 1 and 3", got)
	}
	if got := m.play.playChainCrumb(); got != "data.json › .data.items › map(select(.ok))" {
		t.Errorf("breadcrumb = %q", got)
	}

	m = chainBack(m)
	if m.play.program.Text != "map(select(.ok))" || len(m.play.chain) != 1 {
		t.Fatalf("first back: program %q depth %d", m.play.program.Text, len(m.play.chain))
	}
	if got := compact(m); got != `[{"n":1,"ok":true},{"n":3,"ok":true}]` {
		t.Fatalf("first back result = %q", got)
	}

	m = chainBack(m)
	s = m.play
	if s.playChained() || s.program.Text != ".data.items" || s.program.Cur != 3 {
		t.Fatalf("second back: chained %v, program %q caret %d", s.playChained(), s.program.Text, s.program.Cur)
	}
	if s.input != rootIn || s.srcText != rootText {
		t.Error("the root snapshot must come back")
	}
	if got := compact(m); !strings.HasPrefix(got, `[{"n":1,"ok":true}`) {
		t.Fatalf("root result = %q", got)
	}
	if row := playInfoRowPlain(m); strings.Contains(row, "›") {
		t.Errorf("back at the root the breadcrumb must be gone: %q", row)
	}

	m = chainBack(m)
	if !strings.Contains(m.play.status, "not chained") {
		t.Errorf("back at the root: status %q", m.play.status)
	}
}

// TestPlayChainRecordsHistory: every chained program goes into the history,
// and only the root program is remembered as the file's last one.
func TestPlayChainRecordsHistory(t *testing.T) {
	m := openJQ(t, dismissOnboarding(playApp(t, chainDoc)))
	m = setProgram(m, ".data.items")
	m = chainPlay(m)
	m = setProgram(m, "map(.n)")
	m = chainPlay(m)
	var entries []string
	for i := 0; i < m.play.hist.Len(); i++ {
		e, _ := m.play.hist.At(i)
		entries = append(entries, e)
	}
	for _, want := range []string{".data.items", "map(.n)"} {
		found := false
		for _, e := range entries {
			found = found || e == want
		}
		if !found {
			t.Errorf("history %q misses %q", entries, want)
		}
	}
	if got := m.playLastProgram[m.play.srcKey]; got != ".data.items" {
		t.Errorf("last program = %q, want the root one", got)
	}
}

// TestPlayChainBreadcrumbTruncatesFromTheLeft: on a narrow row the trail
// keeps its newest level and loses its oldest.
func TestPlayChainBreadcrumbTruncatesFromTheLeft(t *testing.T) {
	m := openJQ(t, dismissOnboarding(playApp(t, chainDoc)))
	m = setProgram(m, ".data.items")
	m = chainPlay(m)
	m = setProgram(m, "map(select(.ok))")
	m = chainPlay(m)

	wide := ansi.Strip(m.playInfoLine(200))
	if !strings.HasPrefix(wide, "data.json › .data.items › map(select(.ok)) · ") {
		t.Errorf("wide row = %q, want the whole trail first", wide)
	}
	for _, width := range []int{40, 24, 12} {
		line := ansi.Strip(m.playInfoLine(width))
		if w := ansi.StringWidth(line); w > width {
			t.Errorf("width %d: row is %d cells: %q", width, w, line)
		}
		if !strings.HasPrefix(line, "…") || strings.Contains(line, "data.json") {
			t.Errorf("width %d: row = %q, want the trail cut from the left", width, line)
		}
	}
	if line := ansi.Strip(m.playInfoLine(40)); !strings.Contains(line, "(.ok))") {
		t.Errorf("narrow row = %q, want the newest level kept", line)
	}
}

// TestPlayChainYQ: the yq playground chains its values the same way, and the
// chained level's result is YAML again.
func TestPlayChainYQ(t *testing.T) {
	m := openYQ(t, yqApp(t, "items:\n  - name: a\n  - name: b\n"))
	m = setProgram(m, ".items")
	m = chainPlay(m)
	m = setProgram(m, ".[1].name")
	if got := m.play.result.Text(); got != "b" {
		t.Fatalf("chained yq result = %q", got)
	}
	if m.play.input.Dialect() != jqplay.DialectYQ {
		t.Errorf("chained input dialect = %v", m.play.input.Dialect())
	}
	m = chainBack(m)
	if m.play.program.Text != ".items" || m.play.playChained() {
		t.Errorf("back: program %q", m.play.program.Text)
	}
}

// TestPlayChainXMQRefused: the xmq playground's outputs are text, so the
// chain is refused with a notice and nothing changes.
func TestPlayChainXMQRefused(t *testing.T) {
	fakeXMQOnPath(t)
	m := openXMQ(t, xmqApp(t, "xml", "<root/>\n"))
	m = setProgram(m, "to-json")
	m = chainPlay(m)
	if m.play.playChained() || m.play.program.Text != "to-json" {
		t.Fatalf("xmq chained: depth %d program %q", len(m.play.chain), m.play.program.Text)
	}
	if !strings.Contains(lastNotification(t, m), "xmq playground cannot chain") {
		t.Errorf("notification = %q", lastNotification(t, m))
	}
}

// TestPlayChainRefusesAStaleResult: an error on the row means the buffer is
// not the program's output, so there is nothing honest to chain.
func TestPlayChainRefusesAStaleResult(t *testing.T) {
	m := openJQ(t, dismissOnboarding(playApp(t, chainDoc)))
	m = setProgram(m, ".data.items")
	m = setProgram(m, ".data.items | error")
	m = chainPlay(m)
	if m.play.playChained() || !strings.Contains(m.play.status, "stale") {
		t.Errorf("stale result chained: depth %d status %q", len(m.play.chain), m.play.status)
	}
	m = setProgram(m, "empty")
	m = chainPlay(m)
	if m.play.playChained() || !strings.Contains(m.play.status, "no values") {
		t.Errorf("empty result chained: status %q", m.play.status)
	}
}

// TestPlayChainPausesFollowing: a change of the followed file does not reach
// a chained level; the pop to the root re-reads it.
func TestPlayChainPausesFollowing(t *testing.T) {
	m, path := playWatchApp(t, `{"foo":{"a":1}}`)
	m = dismissOnboarding(m)
	m = setProgram(m, ".foo")
	m = chainPlay(m)
	m = setProgram(m, ".a")

	m = playExternalWrite(t, m, path, `{"foo":{"a":42}}`)
	if got := m.play.result.Text(); got != "1" || !m.play.playChained() {
		t.Fatalf("chained level after the change: %q", got)
	}
	if !strings.Contains(m.play.status, "paused while chained") {
		t.Errorf("status = %q, want the paused note", m.play.status)
	}

	m = chainBack(m)
	if got := compact(m); got != `{"a":42}` {
		t.Errorf("root after back = %q, want the changed file re-read", got)
	}
}

// TestPlayChainSurvivesProjectSwitch: the chain parks and resumes with the
// playground (#2535).
func TestPlayChainSurvivesProjectSwitch(t *testing.T) {
	m, b := playSwitchModel(t, chainDoc, ".data.items")
	a := cwd(t)
	m = chainPlay(m)
	m = setProgram(m, "length")

	m = switchTo(t, m, b)
	m = switchTo(t, m, a)
	s := m.play
	if s == nil || len(s.chain) != 1 || s.program.Text != "length" {
		t.Fatalf("resumed: %+v", s)
	}
	if got := s.result.Text(); got != "3" {
		t.Errorf("resumed result = %q", got)
	}
	m = chainBack(m)
	if m.play.program.Text != ".data.items" || m.play.playChained() {
		t.Errorf("back after resume: program %q", m.play.program.Text)
	}
}

// TestPlayChainDefaultChords: ctrl+alt+shift+↓ chains and ctrl+alt+shift+↑
// goes back, from the query line and from the result buffer.
func TestPlayChainDefaultChords(t *testing.T) {
	m := openJQ(t, dismissOnboarding(playApp(t, chainDoc)))
	m = setProgram(m, ".data.items")
	down := tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModCtrl | tea.ModAlt | tea.ModShift}
	up := tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModCtrl | tea.ModAlt | tea.ModShift}

	m = drainKey(m, down)
	if !m.play.playChained() {
		t.Fatal("ctrl+alt+shift+down on the query line must chain")
	}
	if row := playInfoRowPlain(m); !strings.Contains(row, "ctrl+alt+shift+up chain back") && !strings.Contains(row, "chained — ctrl+alt+shift+up") {
		t.Errorf("info row = %q, want the way back named", row)
	}
	m.play.setBufFocus(true)
	m = drainKey(m, up)
	if m.play.playChained() || m.play.program.Text != ".data.items" {
		t.Errorf("ctrl+alt+shift+up in the result must go back: program %q", m.play.program.Text)
	}
}
