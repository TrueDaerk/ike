package app

// playchain.go is the playground's result chaining (#2795): exploratory work
// narrows step by step — `.data.items`, then work on that subtree — and the
// only way used to be a longer and longer program. playground.chainResult
// makes the current result the next input snapshot and starts the program
// over at the identity; playground.chainBack pops one level, restoring the
// snapshot and the program (caret, toggles) it left. The info row leads with
// the trail, `data.json › .data.items › map(select(.ok))`, cut from the left
// on a narrow pane so the newest level stays readable.
//
// Decisions this file encodes:
//
//   - The chained input is the result's *values* (jqplay.Result.Chain), not
//     its text re-parsed: a -r / -c rendering is a view, and the strings of a
//     raw result chain as strings. The toggles that only shape the output
//     (-r, -c) stay with the next level; slurp (-s), which reshapes the input,
//     starts off. The popped level gets its own toggles back.
//   - Following the source file (#2356) pauses while chained. A chained
//     level's input is a result, not the file, so a change on disk cannot be
//     re-read into it; the info row says the source changed, and popping back
//     to the root re-reads the file if its bytes moved in the meantime. The
//     root snapshot's digest (srcHash) is never touched by a chained level,
//     which is what makes that check the same one resumePlayRun makes.
//   - xmq is refused with a notice: its engine is the external binary, whose
//     outputs are text in the command's notation, not values — a `to-json`
//     output belongs in the jq playground (ctrl+o opens it as a scratch).
//   - Every chained program goes into the history, like enter.
//   - Only the root level's programs are the input's "last valid program"
//     (#1982): a chained level's program queries a result, not the file.
//   - The chain lives on playState, so it parks and resumes with the
//     playground across a project switch (#2535) like everything else.

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/host"
	"ike/internal/jqplay"
	"ike/internal/ui"
	"ike/internal/undostore"
)

// ChainPlayResultMsg is playground.chainResult: the result becomes the next
// input and the program starts over.
type ChainPlayResultMsg struct{}

// ChainPlayBackMsg is playground.chainBack: pop one chained level.
type ChainPlayBackMsg struct{}

// playChainFrame is one level the chain left: the snapshot it queried and the
// program on the query line when it was chained, restored by a pop. label is
// the program that produced the next level — the breadcrumb's segment — which
// is the stage cut while stepping (#2785), the program otherwise.
type playChainFrame struct {
	input   *jqplay.Input
	srcText string
	srcLen  int
	csv     bool
	csvSep  rune
	program string
	cur     int
	opts    jqplay.Options
	label   string
}

// playChained reports whether the playground is on a chained level.
func (s *playState) playChained() bool { return s != nil && len(s.chain) > 0 }

// chainPlayResult is the command. It refuses — with the reason on the info
// row — whenever the buffer is not the current program's output: nothing run
// yet, a stale result under an error, or a run still pending.
func (m *Model) chainPlayResult() tea.Cmd {
	s := m.play
	if s == nil {
		return nil
	}
	if s.dialect == jqplay.DialectXMQ {
		m.host.Notify(host.Info, "the xmq playground cannot chain its result — outputs are text, not values; open a to-json result in the jq playground (ctrl+o scratch)")
		return nil
	}
	refusal := ""
	switch {
	case s.parsing || s.pending:
		refusal = "nothing to chain yet — the result is still running"
	case s.playStale():
		refusal = "nothing to chain — the result is stale; fix the error first"
	case !s.haveResult:
		refusal = "nothing to chain — run a program first"
	}
	if refusal != "" {
		s.status, s.statusWarn = refusal, true
		return nil
	}
	in, text, err := s.result.Chain()
	if err != nil {
		s.status, s.statusWarn = "nothing to chain — "+err.Error(), true
		return nil
	}
	label := strings.TrimSpace(s.playEvalProgram())
	if label == "" {
		label = playIdentity(s.dialect)
	}
	s.chain = append(s.chain, playChainFrame{
		input: s.input, srcText: s.srcText, srcLen: s.srcLen, csv: s.csv, csvSep: s.csvSep,
		program: s.program.Text, cur: s.program.Cur, opts: s.opts, label: label,
	})
	s.hist.Add(s.program.Text)
	s.input, s.inputErr = in, ""
	s.srcText, s.srcLen = "", len(text)
	if len(text) <= jqplay.MaxSampleBytes {
		s.srcText = text
	}
	s.csv, s.csvSep = false, 0 // the chained text is the dialect's own, never CSV
	s.opts.Slurp = false
	m.resetPlayProgram(playIdentity(s.dialect), -1)
	s.chainSwitch = true
	s.status, s.statusWarn = "chained — "+m.playCommandChord("playground.chainBack")+" goes back", false
	return m.runPlayNow()
}

// chainPlayBack pops one chained level: its snapshot, program, caret and
// toggles come back and the program runs again. Landing on the root with the
// followed file changed while chained re-reads it (see the file comment).
func (m *Model) chainPlayBack() tea.Cmd {
	s := m.play
	if s == nil {
		return nil
	}
	if !s.playChained() {
		s.status, s.statusWarn = "not chained — nothing to go back to", true
		return nil
	}
	f := s.chain[len(s.chain)-1]
	s.chain = s.chain[:len(s.chain)-1]
	s.hist.Add(s.program.Text)
	s.cancelRun()
	s.input, s.inputErr = f.input, ""
	s.srcText, s.srcLen = f.srcText, f.srcLen
	s.csv, s.csvSep = f.csv, f.csvSep
	s.opts = f.opts
	m.resetPlayProgram(f.program, f.cur)
	s.chainSwitch = true
	s.status, s.statusWarn = "", false
	if !s.playChained() && s.srcPath != "" {
		if text, ok := m.playSourceText(s); ok && undostore.Hash([]byte(text)) != s.srcHash {
			return m.playRefreshInput(text)
		}
	}
	return m.runPlayNow()
}

// resetPlayProgram puts program on the query line with the caret at cur (-1:
// the end), dropping everything that described the program it replaces — a
// stage selection, the completion popup, a history walk, an armed select-all.
func (m *Model) resetPlayProgram(program string, cur int) {
	s := m.play
	s.program = ui.NewField(program)
	if cur >= 0 && cur <= s.program.Len() {
		s.program.Cur = cur
	}
	s.stage, s.stageProg = 0, ""
	s.comp, s.histIdx, s.qgoal = nil, -1, -1
	s.draft, s.draftPos = "", 0
}

// playChainPausedWatch is playWatchEvent's answer while chained: the change is
// not read into a chained level, only announced — the pop to the root reads it.
func (s *playState) playChainPausedWatch() {
	s.status = "source file changed — following is paused while chained; go back to the root to reload"
	s.statusWarn = true
}

// playChainCrumb is the breadcrumb's plain text: the source, then the program
// that produced each chained level. "" when not chained.
func (s *playState) playChainCrumb() string {
	if !s.playChained() {
		return ""
	}
	parts := make([]string, 0, len(s.chain)+1)
	parts = append(parts, s.source)
	for _, f := range s.chain {
		parts = append(parts, f.label)
	}
	return strings.Join(parts, playCrumbSep)
}

// playCrumbSep separates the breadcrumb's levels.
const playCrumbSep = " › "

// playChainSegment is the breadcrumb fitted into budget cells: the trail is
// cut from the *left*, so the newest levels — the ones the program on the
// query line is about — stay readable on a narrow pane.
func (m Model) playChainSegment(budget int) string {
	crumb := m.play.playChainCrumb()
	if crumb == "" || budget < 1 {
		return ""
	}
	if w := ansi.StringWidth(crumb); w > budget {
		crumb = ansi.TruncateLeft(crumb, w-budget+1, "…")
	}
	return lipgloss.NewStyle().Foreground(m.pal().Accent).Render(crumb)
}
