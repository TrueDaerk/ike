package app

// playfindpaste_test.go guards #2772: inside a focused jq playground a paste
// goes to whatever takes the keyboard input — an open result search line when
// there is one, the query line otherwise. Before the fix every paste landed in
// the query line, yanked the focus back there and re-ran the program, even
// with the search line open and taking typed characters.

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
)

// pastePlay sends a bracketed paste through the app and settles it.
func pastePlay(m Model, text string) (Model, tea.Cmd) {
	tm, cmd := m.Update(tea.PasteMsg{Content: text})
	return tm.(Model), cmd
}

// assertPasteInSearchLine checks the search-line outcome shared by both entry
// routes: the flattened block is on the search line, the keyboard stayed in
// the result buffer and the program was neither changed nor re-evaluated.
func assertPasteInSearchLine(t *testing.T, m Model, cmd tea.Cmd, program, result string) {
	t.Helper()
	if got, want := m.play.resultEd.CommandLine(), "/ga mma"; got != want {
		t.Errorf("search line = %q, want %q (the flattened paste after the typed prefix)", got, want)
	}
	if m.play.program.Text != program {
		t.Errorf("query line = %q, want it unchanged (%q)", m.play.program.Text, program)
	}
	if m.play.pending {
		t.Error("a search-line paste must not schedule an evaluation")
	}
	if cmd != nil {
		t.Error("a search-line paste must not produce a command (no evaluation)")
	}
	if !m.play.bufFocus {
		t.Error("the keyboard must stay in the result buffer's search line")
	}
	if m.play.result.Text() != result {
		t.Errorf("the paste changed the read-only result:\n%s", m.play.result.Text())
	}
}

// TestPlayPasteIntoResultSearchLine: the search opened with "/" after tabbing
// into the result takes the paste, flattened like the editor's own search line.
func TestPlayPasteIntoResultSearchLine(t *testing.T) {
	m := playNoOnboarding(openJQ(t, playApp(t, `{"foo":["alpha","beta","gamma"]}`)))
	m = setProgram(m, ".foo[]")
	program, result := m.play.program.Text, m.play.result.Text()
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = playKeys(m, "/g")
	if m.play.resultEd.ModeName() != editor.Command {
		t.Fatal("test setup: '/' must open the result search line")
	}

	m, cmd := pastePlay(m, "a\nmma")
	assertPasteInSearchLine(t, m, cmd, program, result)
}

// TestPlayPasteIntoSearchFromQueryLine: the #2411 round trip — a search the
// find chord opened from the query line takes the paste, and esc still hands
// the keyboard back to the query line afterwards.
func TestPlayPasteIntoSearchFromQueryLine(t *testing.T) {
	m := playNoOnboarding(openJQ(t, playApp(t, `{"foo":["alpha","beta","gamma"]}`)))
	m = setProgram(m, ".foo[]")
	program, result := m.play.program.Text, m.play.result.Text()
	m = drainKey(m, playFindKey())
	m = playKeys(m, "g")

	m, cmd := pastePlay(m, "a\nmma")
	assertPasteInSearchLine(t, m, cmd, program, result)
	if !m.play.findQuery {
		t.Fatal("the paste must keep the find chord's return-to-query state")
	}

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.play.bufFocus {
		t.Error("esc after the paste must still return the keyboard to the query line")
	}
}

// TestPlayPasteQueryLineUnchanged: with no prompt open the query line keeps
// the #1936 behaviour — flattened, history reset, re-evaluated.
func TestPlayPasteQueryLineUnchanged(t *testing.T) {
	m := playNoOnboarding(openJQ(t, playApp(t, `{"foo":[1,2,3]}`)))
	m.play.program.Clear()
	m.play.histIdx = 2

	m, cmd := pastePlay(m, ".foo\n| length")
	if got := m.play.program.Text; got != ".foo | length" {
		t.Fatalf("query line = %q, want the flattened pasted program", got)
	}
	if m.play.histIdx != -1 {
		t.Errorf("histIdx = %d, a paste must reset the history walk", m.play.histIdx)
	}
	if !m.play.pending || cmd == nil {
		t.Error("a query-line paste must schedule an evaluation")
	}
	m = drainCmd(m, cmd)
	if got := m.play.result.Text(); got != "3" {
		t.Errorf("result = %q, want the pasted program's output", got)
	}
}

// TestPlayPasteResultBufferNormalMode documents today's behaviour for the
// result buffer focused without a prompt: the read-only buffer never takes the
// paste — the query line does, and the keyboard moves back there with it.
func TestPlayPasteResultBufferNormalMode(t *testing.T) {
	m := playNoOnboarding(openJQ(t, playApp(t, `{"foo":[1,2,3]}`)))
	m = setProgram(m, ".foo[]")
	result := m.play.result.Text()
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if !m.play.bufFocus || m.play.resultEd.ModeName() != editor.Normal {
		t.Fatal("test setup: the result buffer must be focused in normal mode")
	}

	m, _ = pastePlay(m, " | length")
	if m.play.result.Text() != result {
		t.Errorf("the read-only result took the paste:\n%s", m.play.result.Text())
	}
	if got := m.play.program.Text; got != ".foo[] | length" {
		t.Errorf("query line = %q, want the paste appended to the program", got)
	}
	if m.play.bufFocus {
		t.Error("the paste must move the keyboard back to the query line")
	}
}
