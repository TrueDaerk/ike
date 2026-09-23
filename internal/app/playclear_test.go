package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// playclear_test.go covers the result focus' ctrl+l (#2700): the shell's
// "clear the screen", answered where the habit fires. Telemetry had the key
// recorded as unbound in the playground — the query line's ctrl+l is the
// saved-filter picker (#1995), and the result buffer claimed nothing.

// ctrlL is the clear chord, the same press the query line reads as "open the
// saved-filter picker".
var ctrlL = tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl}

// intoResult moves the keyboard into the result buffer, where ctrl+l clears.
func intoResult(m Model) Model {
	return drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
}

// TestPlaygroundClearEmptiesTheOutput is the issue's first acceptance case:
// ctrl+l in the result buffer empties it, and the next change to the filter
// fills it again.
func TestPlaygroundClearEmptiesTheOutput(t *testing.T) {
	m := playReady(t, `{"foo":[{"bar":1},{"bar":4}]}`)
	m = setProgram(m, ".foo")
	if !strings.Contains(m.play.result.Text(), `"bar": 4`) {
		t.Fatalf("setup: the result must hold the queried array, got %q", m.play.result.Text())
	}

	m = intoResult(m)
	m = drainKey(m, ctrlL)
	if got := m.play.result.Text(); got != "" {
		t.Fatalf("ctrl+l must empty the result, got %q", got)
	}
	if m.play.haveResult {
		t.Error("a cleared playground has no result to call stale")
	}
	if v := ansi.Strip(m.render()); strings.Contains(v, `"bar": 4`) {
		t.Errorf("the cleared output must be gone from the screen, got:\n%s", v)
	}

	// The next filter change fills it again, exactly as the first run did.
	m.play.setBufFocus(false)
	m = typeInto(m, "[0]")
	if got := m.play.program.Text; got != ".foo[0]" {
		t.Fatalf("the query line must survive the clear, got %q", got)
	}
	if got := m.play.result.Text(); !strings.Contains(got, `"bar"`) {
		t.Fatalf("the next run must fill the result again, got %q", got)
	}
}

// TestPlaygroundClearKeepsTheQueryLine: clearing the screen is not clearing
// the work — the program, its caret and the history all stay.
func TestPlaygroundClearKeepsTheQueryLine(t *testing.T) {
	m := playReady(t, `{"foo":1}`)
	m = setProgram(m, ".foo")
	m.play.hist.Add(".bar")

	m = intoResult(m)
	m = drainKey(m, ctrlL)
	if got := m.play.program.Text; got != ".foo" {
		t.Fatalf("ctrl+l must leave the query line alone, got %q", got)
	}
	if got := m.play.hist.Len(); got == 0 {
		t.Error("ctrl+l must not touch the program history")
	}
	if m.play.input == nil {
		t.Error("ctrl+l must not drop the parsed input snapshot")
	}
	if m.play.status == "" {
		t.Error("ctrl+l should say what it did")
	}
}

// TestPlaygroundClearDropsTheErrorLine: the run error and the stale banner it
// flags the buffer with go with the output — a message about a result that is
// no longer on screen describes nothing.
func TestPlaygroundClearDropsTheErrorLine(t *testing.T) {
	m := playReady(t, `{"foo":1}`)
	m = setProgram(m, ".foo")
	m = setProgram(m, ".foo | (")
	if m.play.runErr == "" {
		t.Fatal("setup: a broken program must leave a run error")
	}
	if !m.play.playStale() {
		t.Fatal("setup: the last good result must be flagged stale")
	}

	m = intoResult(m)
	m = drainKey(m, ctrlL)
	if m.play.runErr != "" {
		t.Errorf("ctrl+l must clear the error line, got %q", m.play.runErr)
	}
	if m.play.playStale() {
		t.Error("an empty buffer cannot be a stale result")
	}
}

// TestPlaygroundClearKeepsTheFilterPickerOnTheQueryLine: the key is split by
// focus, so the query line's ctrl+l still opens the saved filters (#1995).
func TestPlaygroundClearKeepsTheFilterPickerOnTheQueryLine(t *testing.T) {
	m := playReady(t, `{"foo":1}`)
	m = setProgram(m, ".foo")

	m = drainKey(m, ctrlL)
	if got := m.play.result.Text(); got == "" {
		t.Error("the query line's ctrl+l must not clear the result")
	}
	if m.play.runErr != "" {
		t.Errorf("the query line's ctrl+l must not touch the error line, got %q", m.play.runErr)
	}
	// The library is empty in a fresh session, so the picker explains where
	// filters come from instead of opening on nothing — either way the key
	// went to the filters, not to the clear.
	if got := m.play.status; strings.Contains(got, "cleared") {
		t.Errorf("the query line's ctrl+l must not clear, status %q", got)
	}
}

// TestPlaygroundClearDialectKeepsTheResultPath: a cleared yq playground stays
// a yq one — the buffer's display path is what resolves its highlighting, so
// the empty result must carry the dialect rather than the zero value's jq.
func TestPlaygroundClearDialectKeepsTheResultPath(t *testing.T) {
	m := playReady(t, `{"foo":1}`)
	m = setProgram(m, ".foo")
	m = intoResult(m)
	m = drainKey(m, ctrlL)
	if got, want := m.play.result.ResultPath(), m.play.dialect.Name()+" result."; !strings.HasPrefix(got, want) {
		t.Fatalf("cleared result path = %q, want the %s dialect's", got, m.play.dialect.Name())
	}
}
