package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/jqplay"
)

// playcompile_test.go covers the live syntax check (#2780): the query line is
// compiled on every keystroke, ahead of the debounced run.

// playKey sends one key to the playground and returns the model with the
// command it produced left undrained — the state the keystroke itself left,
// before any debounce tick or run could fire.
func playKey(m Model, r rune) (Model, tea.Cmd) {
	tm, cmd := m.updatePlaygroundKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	return tm.(Model), cmd
}

// TestPlayCompileErrorShowsBeforeDebounce: typing `.a |` shows the compile
// error on the keystroke itself — no tick, no run — and completing the
// program to `.a | .b` clears it and evaluates.
func TestPlayCompileErrorShowsBeforeDebounce(t *testing.T) {
	m := openJQ(t, dismissOnboarding(playApp(t, `{"a":{"b":7}}`)))
	m = setProgram(m, ".a ")
	gen := m.play.gen
	m, cmd := playKey(m, '|')
	s := m.play
	if want := jqplay.Compile(jqplay.DialectJQ, ".a |"); s.runErr != want || want == "" {
		t.Fatalf("runErr = %q right after the keystroke, want the compile error %q", s.runErr, want)
	}
	if s.gen != gen || s.pending {
		t.Fatalf("a program that does not compile must not schedule a run (gen %d → %d, pending %v)", gen, s.gen, s.pending)
	}
	row := ansi.Strip(m.playInfoRow(200))
	if !strings.HasPrefix(row, "E: ") {
		t.Errorf("the compile error should take the info row, got %q", row)
	}
	m = drainCmd(m, cmd)
	if m.play.gen != gen {
		t.Fatalf("draining the keystroke's command started a run (gen %d → %d)", gen, m.play.gen)
	}

	m = typeInto(m, " .b")
	s = m.play
	if s.runErr != "" || s.compileBad {
		t.Fatalf("a completed program must clear the compile error, got %q", s.runErr)
	}
	if got := s.result.Text(); got != "7" {
		t.Errorf("the completed program should evaluate, result = %q", got)
	}
}

// TestPlayCompileErrorDropsInFlightRun: a run already in flight for the last
// valid program cannot land over the compile error of the one typed since —
// nor can the debounce tick scheduled for it.
func TestPlayCompileErrorDropsInFlightRun(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	s := m.play
	s.program.Set(".a")
	tick := m.schedulePlayEval()
	gen := s.gen
	s.program.Set(".a |")
	if m.schedulePlayEval() != nil {
		t.Fatal("a program that does not compile must not schedule anything")
	}
	m = drainCmd(m, tick) // the old tick finds a broken program
	m.finishPlayEval(playEvalDoneMsg{st: s, gen: gen, res: jqplay.Result{Outputs: []string{"999"}}})
	if s.gen != gen {
		t.Fatalf("the eval generation advanced (%d → %d)", gen, s.gen)
	}
	if !s.compileBad || s.runErr == "" {
		t.Fatalf("the compile error must stay up, got %q", s.runErr)
	}
	if strings.Contains(s.result.Text(), "999") {
		t.Error("a result in flight for an earlier program landed over the compile error")
	}
}

// TestPlayCompileErrorKeepsLastGoodResult: the compile error behaves like any
// failed run (#2412) — the last good result stays, marked stale.
func TestPlayCompileErrorKeepsLastGoodResult(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m = setProgram(m, ".a")
	good := m.play.result.Text()
	m = setProgram(m, ".a |")
	s := m.play
	if !s.compileBad || s.result.Text() != good || !s.playStale() {
		t.Fatalf("compile error should keep %q stale, got %q (bad %v, stale %v)", good, s.result.Text(), s.compileBad, s.playStale())
	}
	if !strings.Contains(ansi.Strip(m.playInfoRow(200)), "showing the last good result") {
		t.Errorf("the row should name the result on screen, got %q", ansi.Strip(m.playInfoRow(200)))
	}
}

// TestPlayCompileXMQUnterminatedQuote: the xmq dialect's compile step is the
// shell-word split, so an open quote never reaches the binary.
func TestPlayCompileXMQUnterminatedQuote(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	s := m.play
	s.dialect = jqplay.DialectXMQ
	s.program.Set(`select "//a`)
	gen := s.gen
	if m.runPlayNow() != nil || s.gen != gen || !s.compileBad {
		t.Fatalf("an unterminated quote must stop the run (gen %d → %d, err %q)", gen, s.gen, s.runErr)
	}
}
