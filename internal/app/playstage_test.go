package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/jqplay"
)

// playstage_test.go covers the playground's pipeline stepping (#2785): the
// stage state machine, the cut program's result, the info-row counter, the
// query-line highlight in both views, the ways out (typing, esc) and the xmq
// no-op.

// stepStage dispatches playground.stageNext (delta 1) or stagePrev (-1).
func stepStage(m Model, delta int) Model {
	tm, cmd := m.Update(StepPlayStageMsg{Delta: delta})
	return drainCmd(tm.(Model), cmd)
}

// stageResult is the installed result, whitespace removed.
func stageResult(m Model) string {
	return strings.Join(strings.Fields(m.play.result.Text()), "")
}

// stagePlay opens the jq playground over the issue's document and program,
// the caret at the program's end.
func stagePlay(t *testing.T) Model {
	t.Helper()
	m := openJQ(t, dismissOnboarding(playApp(t, `{"items":[{"x":1},{"x":2},{"x":3}]}`)))
	return setProgram(m, ".items | map(.x) | add")
}

// TestPlayStageStepsThroughThePipeline is the issue's acceptance case: stage
// 1 shows the items array, stage 2 the mapped array, stage 3 the sum; the
// info row counts the stage.
func TestPlayStageStepsThroughThePipeline(t *testing.T) {
	m := stagePlay(t)
	if got := stageResult(m); got != "6" {
		t.Fatalf("full program = %q, want the sum", got)
	}
	// Caret on the last stage: previous steps back one.
	m = stepStage(m, -1)
	if m.play.stage != 2 || stageResult(m) != "[1,2,3]" {
		t.Fatalf("stage %d = %q, want stage 2's mapped array", m.play.stage, stageResult(m))
	}
	if row := playInfoRowPlain(m); !strings.Contains(row, "stage 2/3") {
		t.Errorf("info row = %q, want the stage counter", row)
	}
	m = stepStage(m, -1)
	if m.play.stage != 1 || stageResult(m) != `[{"x":1},{"x":2},{"x":3}]` {
		t.Fatalf("stage %d = %q, want stage 1's items", m.play.stage, stageResult(m))
	}
	m = stepStage(m, -1) // stops at the first stage
	if m.play.stage != 1 {
		t.Errorf("stage = %d past the first, want it to stay", m.play.stage)
	}
	m = stepStage(m, 1)
	m = stepStage(m, 1)
	if m.play.stage != 3 || stageResult(m) != "6" {
		t.Fatalf("stage %d = %q, want the last stage's sum", m.play.stage, stageResult(m))
	}
	if row := playInfoRowPlain(m); !strings.Contains(row, "stage 3/3") {
		t.Errorf("info row = %q, want stage 3/3", row)
	}
	if m.play.program.Text != ".items | map(.x) | add" {
		t.Errorf("stepping must not edit the program, got %q", m.play.program.Text)
	}
}

// TestPlayStageEntersAtTheCaret: entering stepping picks the caret's stage;
// next from the last stage starts over at the first.
func TestPlayStageEntersAtTheCaret(t *testing.T) {
	m := stagePlay(t)
	m.play.program.Cur = 10 // inside `map(.x)`
	if m = stepStage(m, 1); m.play.stage != 2 {
		t.Errorf("next with the caret in stage 2 selected %d", m.play.stage)
	}
	m = stagePlay(t)
	if m = stepStage(m, 1); m.play.stage != 1 {
		t.Errorf("next with the caret on the last stage selected %d, want the first", m.play.stage)
	}
}

// TestPlayStageTypingRunsTheFullProgram: an edit leaves stepping, and the run
// it schedules is the full program's.
func TestPlayStageTypingRunsTheFullProgram(t *testing.T) {
	m := stagePlay(t)
	m = stepStage(m, -1)
	m = stepStage(m, -1)
	if stageResult(m) == "6" {
		t.Fatal("test setup: stage 1 must not show the full result")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: ' ', Text: " "})
	if _, _, on := m.play.playStepping(); on {
		t.Fatal("typing must leave stepping")
	}
	if m.play.stage != 0 {
		t.Errorf("stage = %d after an edit, want 0", m.play.stage)
	}
	if got := stageResult(m); got != "6" {
		t.Errorf("result after typing = %q, want the full program's sum", got)
	}
	if row := playInfoRowPlain(m); strings.Contains(row, "stage ") {
		t.Errorf("info row still counts a stage: %q", row)
	}
}

// TestPlayStageEscReturnsToFullProgram: esc leaves stepping first — the
// playground stays open — and a second esc closes it.
func TestPlayStageEscReturnsToFullProgram(t *testing.T) {
	m := stagePlay(t)
	m = stepStage(m, -1)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.playOpen() {
		t.Fatal("esc while stepping must not close the playground")
	}
	if _, _, on := m.play.playStepping(); on || stageResult(m) != "6" {
		t.Fatalf("esc must return to the full program (stepping %v, result %q)", on, stageResult(m))
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.playOpen() {
		t.Error("the next esc closes the playground as usual")
	}
}

// TestPlayStageEscFromResultBuffer: the same from the result buffer's resting
// normal mode.
func TestPlayStageEscFromResultBuffer(t *testing.T) {
	m := stagePlay(t)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = stepStage(m, -1)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.playOpen() || m.play.stage != 0 || stageResult(m) != "6" {
		t.Fatalf("esc in the result buffer must leave stepping first (open %v, stage %d)", m.playOpen(), m.play.stage)
	}
}

// TestPlayStageChordsReachTheCommand: the default chords run the commands
// from the query line.
func TestPlayStageChordsReachTheCommand(t *testing.T) {
	m := stagePlay(t)
	mods := tea.ModCtrl | tea.ModAlt | tea.ModShift
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyLeft, Mod: mods})
	if m.play.stage != 2 {
		t.Fatalf("ctrl+alt+shift+left selected stage %d, want 2", m.play.stage)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyRight, Mod: mods})
	if m.play.stage != 3 {
		t.Errorf("ctrl+alt+shift+right selected stage %d, want 3", m.play.stage)
	}
	if m.play.program.Text != ".items | map(.x) | add" {
		t.Errorf("the chords must not reach the query line, program %q", m.play.program.Text)
	}
}

// TestPlayStageSingleStageSaysSo: a program without a top-level pipe has
// nothing to step through — including one whose only pipe is nested.
func TestPlayStageSingleStageSaysSo(t *testing.T) {
	m := stagePlay(t)
	m = setProgram(m, ".items | map(.x)")
	m = setProgram(m, "[.items[] | .x]")
	m = stepStage(m, -1)
	if m.play.stage != 0 {
		t.Fatalf("stage = %d on a single-stage program", m.play.stage)
	}
	if !strings.Contains(m.play.status, "no pipeline stages") {
		t.Errorf("status = %q, want the explanation", m.play.status)
	}
}

// TestPlayStageHighlightsBothViews: the selected stage renders on the muted
// selection background, the dropped tail faint, in the one-row and in the
// multi-line query view.
func TestPlayStageHighlightsBothViews(t *testing.T) {
	m := stagePlay(t)
	m = stepStage(m, -1) // stage 2: `map(.x)`, runes [9,16)
	paint := m.playStructurePainter(m.play.program.Text, jqplay.Tokens(m.play.program.Text), m.playKindStyles())
	bg := m.pal().SelectionMuted
	if got := paint(9).GetBackground(); got != bg {
		t.Errorf("stage rune background = %v, want %v", got, bg)
	}
	if got := paint(0).GetBackground(); got == bg {
		t.Error("a rune before the stage must not be highlighted")
	}
	if !paint(20).GetFaint() {
		t.Error("the dropped tail must render faint")
	}
	if paint(0).GetFaint() {
		t.Error("the kept head must not render faint")
	}
	// Both views paint through it: each renders differently while stepping,
	// with the same text.
	width := playResultWidth(m)
	views := func(m Model) (one, multi string) {
		one = m.playQueryRow(width)
		m.togglePlayQueryView()
		multi = strings.Join(m.playQueryRows(width), "\n")
		m.togglePlayQueryView()
		return one, multi
	}
	oneOn, multiOn := views(m)
	m.play.stage = 0
	oneOff, multiOff := views(m)
	for name, pair := range map[string][2]string{"one-row": {oneOn, oneOff}, "multi-line": {multiOn, multiOff}} {
		if ansi.Strip(pair[0]) != ansi.Strip(pair[1]) {
			t.Errorf("%s view changed its text: %q vs %q", name, ansi.Strip(pair[0]), ansi.Strip(pair[1]))
		}
		if pair[0] == pair[1] {
			t.Errorf("%s view shows no stage highlight", name)
		}
	}
}

// TestPlayStageYQ: the yq dialect steps the same way.
func TestPlayStageYQ(t *testing.T) {
	m := openYQ(t, dismissOnboarding(yqApp(t, "items:\n  - x: 1\n  - x: 2\n")))
	m = setProgram(m, ".items | map(.x) | add")
	m = stepStage(m, -1)
	if m.play.stage != 2 || !strings.Contains(m.play.result.Text(), "- 1") {
		t.Fatalf("yq stage %d = %q, want the mapped list", m.play.stage, m.play.result.Text())
	}
}

// TestPlayStageXMQNotifies: xmq has no pipeline — the commands notify and
// change nothing.
func TestPlayStageXMQNotifies(t *testing.T) {
	fakeXMQOnPath(t)
	m := openXMQ(t, dismissOnboarding(xmqApp(t, "xml", "<r/>\n")))
	m.play.program.Set("select /r | to-json")
	m = stepStage(m, 1)
	if m.play.stage != 0 {
		t.Fatalf("xmq stage = %d, want stepping refused", m.play.stage)
	}
	if !strings.Contains(lastNotification(t, m), "xmq playground has no pipeline stages") {
		t.Errorf("notification = %q", lastNotification(t, m))
	}
}
