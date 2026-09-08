package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/theme"
	"ike/internal/undostore"
)

// playground_switch.go carries the inline playground across a project switch
// (#2535). The mode is bound to its document (#2355), and the document
// survives the switch parked in its workspace — so the mode parks with it in
// wsExtras and is mounted again, query, result, history position and
// multi-line state intact, when the workspace resumes. Before this the fresh
// model performSwitch builds simply started with no playground, and coming
// back to a project found the query line gone.
//
// Two things do not park as-is:
//
//   - Work in flight. An evaluation or an off-loop parse started before the
//     switch would deliver its message to the model of the *other* project,
//     where it is dropped as belonging to no open playground. The park
//     cancels the run, and the resume re-drives whatever was pending, so a
//     query typed right before the switch still has its result on return.
//   - The chrome. The result buffer's palette and editor config belong to
//     the model, and the incoming project's settings layer may differ, so
//     the resume re-threads both the way the pane registry's editors are.
//
// The history the parked state points at is session state (#1977): it rides
// model-to-model with performSwitch, so the pointer stays the one live list.

// parkPlayground detaches the open playground for the workspace's Aux: the
// run in flight is abandoned (its result could only land in the wrong model),
// the state itself is handed over untouched, and the model's slot is cleared
// so the fresh model does not start with a playground of another project.
func (m *Model) parkPlayground() *playState {
	s := m.play
	if s == nil {
		return nil
	}
	s.cancelRun()
	m.play = nil
	return s
}

// resumePlayground mounts a parked playground back on the model being built
// for its workspace, re-threading the chrome the result buffer inherited from
// the model it was opened in. Sizing is left to the layout pass the switch
// runs right after (sizePlayResult), where every pane gets its bounds.
func (m *Model) resumePlayground(s *playState, pal *theme.Palette, cfg host.Config) {
	if s == nil {
		return
	}
	m.play = s
	if s.resultEd != nil {
		s.resultEd.SetPalette(pal)
		s.resultEd.Configure(cfg)
	}
}

// resumePlayRun re-drives the work the park interrupted, once the resumed
// model is sized. A parse that was still decoding is started over from the
// document — the queried response or the followed file, the two sources the
// state can re-read — and a followed file whose bytes changed while the
// workspace was parked (the reconcile pass reloaded its buffer, but no
// watcher event ever reached the playground) is re-read the way an external
// change is (#2356). A run that was pending re-runs the current program.
// Nothing pending, nothing changed: the result comes back as it was left.
func (m *Model) resumePlayRun() tea.Cmd {
	s := m.play
	if s == nil {
		return nil
	}
	if s.parsing {
		s.parsing = false
		if text, ok := m.playResumeText(s); ok {
			return m.parsePlayInput(text)
		}
		// A selection mid-parse: the range cannot be re-read honestly (see
		// playInputSource), and a parse that never finished has no input to
		// run against. Say so instead of leaving a blank result unexplained.
		s.inputErr = "input parse interrupted by the project switch — reopen the playground"
		s.pending = false
		return nil
	}
	if s.srcPath != "" {
		if text, ok := m.playSourceText(s); ok && undostore.Hash([]byte(text)) != s.srcHash {
			return m.playRefreshInput(text)
		}
	}
	if s.pending {
		return m.runPlayNow()
	}
	return nil
}

// playResumeText is the document a parked-mid-parse playground can be re-read
// from: the queried response instance's body, or the followed file's buffer.
func (m Model) playResumeText(s *playState) (string, bool) {
	if s.srcInst != nil {
		h := s.srcInst.HTTP()
		if h == nil || !h.HasBodyText() {
			return "", false
		}
		body := h.JQInput()
		return body, strings.TrimSpace(body) != ""
	}
	if s.srcPath == "" {
		return "", false
	}
	return m.playSourceText(s)
}
