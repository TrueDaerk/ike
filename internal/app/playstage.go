package app

// playstage.go is the playground's pipeline stepping (#2785): debugging a long
// jq/yq pipeline used to mean deleting its tail to see what an earlier stage
// produced. playground.stageNext / playground.stagePrev select a stage of the
// program's top-level pipeline instead; the result shows the program cut
// after that stage (jqplay.StageProgram), the info row counts `stage k/n` and
// the query line highlights the stage — the part the cut drops renders faint.
//
// Stepping is a view over the program *as it was* when the stage was picked:
// playState.stageProg pins that text, and any edit — typing, a paste, a
// history step, an inserted filter or path — ends the mode, because the
// stage numbers no longer describe the program on the line. The full program
// then runs through the ordinary debounce. esc leaves stepping first, before
// it would close the playground. xmq's command line is shell words, not a
// pipeline, so both commands only say so there.

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/host"
	"ike/internal/jqplay"
)

// StepPlayStageMsg moves the playground's stage selection by Delta (+1 next,
// -1 previous) — playground.stageNext / playground.stagePrev.
type StepPlayStageMsg struct{ Delta int }

// playStepping reports the selected stage (1-based) and the program's stage
// ranges while stepping is on: a stage is selected and the program is still
// the text it was selected over.
func (s *playState) playStepping() (k int, stages []jqplay.Line, ok bool) {
	if s == nil || s.stage < 1 || s.stageProg != s.program.Text {
		return 0, nil, false
	}
	stages = jqplay.Stages(s.program.Text)
	if s.stage > len(stages) {
		return 0, nil, false
	}
	return s.stage, stages, true
}

// playEvalProgram is the program a run evaluates: the cut after the selected
// stage while stepping, the query line's text otherwise.
func (s *playState) playEvalProgram() string {
	if k, _, ok := s.playStepping(); ok {
		return jqplay.StageProgram(s.program.Text, k)
	}
	return s.program.Text
}

// syncPlayStage drops a stage selection the program has moved away from, so
// the run about to start is the full program's (every edit path reaches a
// run through schedulePlayEval or runPlayNow).
func (s *playState) syncPlayStage() {
	if s.stage > 0 && s.stageProg != s.program.Text {
		s.stage, s.stageProg = 0, ""
	}
}

// stepPlayStage is the command. Entering stepping picks the caret's stage;
// with the caret on the last stage — whose output is the full result already
// on screen — previous selects the stage before it and next starts over at
// the first. While stepping, the selection moves by delta and stops at either
// end.
func (m *Model) stepPlayStage(delta int) tea.Cmd {
	s := m.play
	if s == nil {
		return nil
	}
	if s.dialect == jqplay.DialectXMQ {
		m.host.Notify(host.Info, "the xmq playground has no pipeline stages to step through")
		return nil
	}
	stages := jqplay.Stages(s.program.Text)
	n := len(stages)
	if n < 2 {
		s.status, s.statusWarn = "no pipeline stages to step through — the program has no top-level |", true
		return nil
	}
	k, _, on := s.playStepping()
	switch {
	case on:
		k = max(1, min(n, k+delta))
		if k == s.stage {
			return nil // already at that end
		}
	default:
		k = jqplay.StageAt(stages, s.program.Cur)
		if k == n {
			if delta < 0 {
				k = n - 1
			} else {
				k = 1
			}
		}
	}
	s.stage, s.stageProg = k, s.program.Text
	s.comp = nil
	return m.runPlayNow()
}

// leavePlayStepping ends stepping and reruns the full program, reporting
// whether stepping was on — esc's first job while it is (#2785).
func (m *Model) leavePlayStepping() (tea.Cmd, bool) {
	s := m.play
	if _, _, ok := s.playStepping(); !ok {
		return nil, false
	}
	s.stage, s.stageProg = 0, ""
	s.status, s.statusWarn = "full program", false
	return m.runPlayNow(), true
}

// playStageSegment is the info row's `stage k/n` counter, "" when not
// stepping.
func (m Model) playStageSegment() string {
	k, stages, ok := m.play.playStepping()
	if !ok {
		return ""
	}
	return lipgloss.NewStyle().Foreground(m.pal().Accent).Bold(true).
		Render(fmt.Sprintf("stage %d/%d", k, len(stages)))
}

// playStagePainter overlays the stepping highlight on the query line's
// painter: the selected stage on the muted selection background, everything
// the cut drops faint. Both query-line views paint through it, so the one-row
// and the multi-line view agree.
func (m Model) playStagePainter(program string, paint func(int) lipgloss.Style) func(int) lipgloss.Style {
	s := m.play
	k, stages, ok := s.playStepping()
	if !ok || program != s.program.Text {
		return paint
	}
	st := stages[k-1]
	bg := m.pal().SelectionMuted
	return func(i int) lipgloss.Style {
		switch {
		case i >= st.Start && i < st.End:
			return paint(i).Background(bg)
		case i >= st.End:
			return paint(i).Faint(true)
		}
		return paint(i)
	}
}
