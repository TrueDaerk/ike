package app

import (
	"runtime"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// playfocusquery_test.go covers playground.focusQuery (#3084): cmd+l, the
// browser's "go to the address bar", puts the keyboard on the query line with
// the whole program selected — from the result buffer, the variables line and
// the query line itself.

func focusQueryPlay(t *testing.T) Model {
	t.Helper()
	m := openJQ(t, dismissOnboarding(playApp(t, `{"a":1,"b":2}`)))
	return setProgram(m, ".a")
}

func focusQuery(m Model) Model {
	tm, cmd := m.Update(FocusPlayQueryMsg{})
	return drainCmd(tm.(Model), cmd)
}

// TestPlayFocusQueryFromResult: from the result buffer the query line takes
// the keyboard all selected; esc undoes the jump instead of closing, and a
// second jump's selection is replaced by typing.
func TestPlayFocusQueryFromResult(t *testing.T) {
	m := focusQueryPlay(t)
	m = pressKey(m, tea.KeyTab)
	if !m.play.bufFocus {
		t.Fatal("tab must focus the result buffer")
	}
	m = focusQuery(m)
	if m.play.bufFocus || !m.play.program.Selected() || m.play.program.Cur != m.play.program.Len() {
		t.Fatalf("focusQuery: bufFocus=%v selected=%v cur=%d", m.play.bufFocus, m.play.program.Selected(), m.play.program.Cur)
	}
	m = pressKey(m, tea.KeyEscape)
	if !m.playOpen() || !m.play.bufFocus || m.play.program.Selected() {
		t.Fatalf("esc must restore the result focus: open=%v bufFocus=%v selected=%v", m.playOpen(), m.play.bufFocus, m.play.program.Selected())
	}
	m = focusQuery(m)
	m = typeInto(m, ".b")
	if m.play.program.Text != ".b" || m.play.result.Text() != "2" {
		t.Fatalf("typing must replace the query: %q → %q", m.play.program.Text, m.play.result.Text())
	}
}

// TestPlayFocusQueryFromQueryLineIdempotent: on the query line the command
// just selects all, again and again.
func TestPlayFocusQueryFromQueryLineIdempotent(t *testing.T) {
	m := focusQueryPlay(t)
	for i := 0; i < 2; i++ {
		m = focusQuery(m)
		if m.play.bufFocus || !m.play.program.Selected() {
			t.Fatalf("pass %d: bufFocus=%v selected=%v", i, m.play.bufFocus, m.play.program.Selected())
		}
	}
	m = pressKey(m, tea.KeyEscape)
	if !m.playOpen() || m.play.bufFocus || m.play.program.Selected() {
		t.Fatalf("esc drops the selection and stays on the query line: open=%v bufFocus=%v", m.playOpen(), m.play.bufFocus)
	}
}

// TestPlayFocusQueryFromVarsLine: the variables line hands the keyboard over
// too, and stays shown.
func TestPlayFocusQueryFromVarsLine(t *testing.T) {
	m := focusQueryPlay(t)
	m = toggleVars(m)
	if !m.play.varsFocus {
		t.Fatal("the variables line must have the keyboard")
	}
	m = focusQuery(m)
	if m.play.varsFocus || m.play.bufFocus || !m.play.varsShown || !m.play.program.Selected() {
		t.Fatalf("varsFocus=%v bufFocus=%v shown=%v selected=%v", m.play.varsFocus, m.play.bufFocus, m.play.varsShown, m.play.program.Selected())
	}
}

// TestPlayFocusQueryDefaultChord: cmd+l reaches the command from the result
// buffer (macOS; off macOS the fold lands on ctrl+l, which the playground
// keeps for clear-output / the filter picker).
func TestPlayFocusQueryDefaultChord(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("cmd+l is a macOS chord")
	}
	m := focusQueryPlay(t)
	m = pressKey(m, tea.KeyTab)
	m = drainKey(m, tea.KeyPressMsg{Code: 'l', Mod: tea.ModSuper})
	if m.play.bufFocus || !m.play.program.Selected() {
		t.Fatalf("cmd+l: bufFocus=%v selected=%v", m.play.bufFocus, m.play.program.Selected())
	}
}
