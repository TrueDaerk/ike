package app

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/agenttrace"
	"ike/internal/complexity"
	"ike/internal/config"
	"ike/internal/deeplink"
	"ike/internal/host"
	"ike/internal/vcs"
)

// complexity_verdict.go is the Stop-hook verdict (#2964, epic 0551): with
// complexity.agent_verdict on, the `Stop` hook event Claude Code pushes at
// the end of every agent turn (`ike agent-hook Stop`, #2843) runs the
// Changes tab's diff job (runComplexityDiff, complexity_diff.go) against
// HEAD for the session's working directory — off the Update loop, the hook
// process has long exited 0 — and posts the verdict as a note row into the
// Agent Trace window of that session: `complexity: LESS COMPLEX (excess
// −12)`, in the warning tone when the excess rose. The notes live in the
// model per session id (traceNotes) and are re-applied to every rebuilt
// tree, so they survive the pane's live reads; a pane not showing the
// session gets a toast instead. Off, nothing runs.

// traceNote is one note row posted into a session's trace.
type traceNote struct {
	at   time.Time
	text string
	warn bool
}

// maxTraceNotes bounds the notes kept per session.
const maxTraceNotes = 50

// cxVerdictMsg is a finished verdict run.
type cxVerdictMsg struct {
	sessionID string
	note      traceNote
}

// complexityVerdictCmd is the hook event's verdict job: nil unless the
// event is a Stop and complexity.agent_verdict is on. The model state the
// diff reads — the VCS snapshot, the index, the dirty buffers — is gathered
// here; the session's repository is detected off the loop and the full
// job is used when it is the project's own.
func (m Model) complexityVerdictCmd(ev deeplink.Event) tea.Cmd {
	if ev.Event != "Stop" || !config.Get().Complexity.AgentVerdict {
		return nil
	}
	var snapJob *cxDiffJob
	if m.vcs != nil && m.vcs.snap != nil && m.cxDiff != nil {
		j := m.complexityDiffJob(cxDiffKey{})
		snapJob = &j
	}
	fallback := cxDiffJob{dirty: map[string]string{}, res: m.complexityResolver(config.Get().Complexity)}
	sessionID, cwd, at := ev.SessionID, ev.CWD, time.Now()
	return func() tea.Msg {
		return cxVerdictMsg{sessionID: sessionID, note: runComplexityVerdict(cwd, at, snapJob, fallback)}
	}
}

// runComplexityVerdict is the off-loop half: detect the session's
// repository, diff it against HEAD and word the verdict.
func runComplexityVerdict(cwd string, at time.Time, snapJob *cxDiffJob, fallback cxDiffJob) traceNote {
	root, err := vcs.DetectRoot(cwd)
	if err != nil || root == "" {
		return traceNote{at: at, text: "complexity: no verdict — " + displayPath(cwd) + " is not in a git repository", warn: true}
	}
	if ev, err := filepath.EvalSymlinks(root); err == nil {
		root = ev
	}
	job := fallback
	job.repoRoot = root
	if snapJob != nil && agentSameDir(snapJob.repoRoot, root) {
		job = *snapJob
	}
	res, ok := runComplexityDiff(job).(complexityDiffMsg)
	switch {
	case !ok:
		return traceNote{at: at, text: "complexity: no verdict", warn: true}
	case res.err != "":
		return traceNote{at: at, text: "complexity: no verdict — " + res.err, warn: true}
	}
	d := res.changes.Diff
	return traceNote{at: at, text: complexityVerdictText(d), warn: d.DeltaExcess() > 0}
}

// complexityVerdictText words a diff's verdict for the trace row:
// "complexity: LESS COMPLEX (excess −12)"; when the excess held, the
// cognitive delta that decided the verdict follows.
func complexityVerdictText(d complexity.Diff) string {
	s := "complexity: " + strings.ToUpper(d.Verdict.String()) + " (excess " + signed(d.DeltaExcess())
	if d.DeltaExcess() == 0 && d.DeltaCognitive() != 0 {
		s += ", cognitive " + signed(d.DeltaCognitive())
	}
	return s + ")"
}

// signed prints a delta with its sign, the minus as U+2212.
func signed(n int) string {
	switch {
	case n > 0:
		return "+" + strconv.Itoa(n)
	case n < 0:
		return "−" + strconv.Itoa(-n)
	}
	return "0"
}

// handleComplexityVerdict files the note and shows it: the open trace pane
// of that session gets the row, any other state a toast.
func (m Model) handleComplexityVerdict(msg cxVerdictMsg) (tea.Model, tea.Cmd) {
	if m.traceNotes == nil {
		m.traceNotes = map[string][]traceNote{}
	}
	notes := append(m.traceNotes[msg.sessionID], msg.note)
	if len(notes) > maxTraceNotes {
		notes = notes[len(notes)-maxTraceNotes:]
	}
	m.traceNotes[msg.sessionID] = notes
	p := m.agentTracePanel()
	if p != nil && p.HasSession() && !m.traceShowingHistory() && p.Info().ID == msg.sessionID {
		p.Set(m.traceWithNotes(m.traceNodes, msg.sessionID), p.Info())
		return m, nil
	}
	sev := host.Info
	if msg.note.warn {
		sev = host.Warn
	}
	m.host.Notify(sev, msg.note.text)
	return m, nil
}

// traceWithNotes appends the session's notes to a rebuilt tree: each as a
// note row under the last turn (a root row when there is none). The input
// is not modified.
func (m Model) traceWithNotes(nodes []agenttrace.Node, sessionID string) []agenttrace.Node {
	notes := m.traceNotes[sessionID]
	if len(notes) == 0 {
		return nodes
	}
	out := make([]agenttrace.Node, len(nodes))
	copy(out, nodes)
	rows := make([]agenttrace.Node, 0, len(notes))
	turn := 0
	if n := len(out); n > 0 {
		turn = out[n-1].Turn
	}
	for i, note := range notes {
		rows = append(rows, agenttrace.Node{
			Kind: agenttrace.NodeNote, Key: "note:" + strconv.FormatInt(note.at.UnixNano(), 10) + "/" + strconv.Itoa(i),
			Turn: turn, Event: -1, Label: note.text, Detail: note.at.Local().Format("15:04"), Warn: note.warn, At: note.at,
		})
	}
	if n := len(out); n > 0 && out[n-1].Kind == agenttrace.NodeTurn {
		last := out[n-1]
		last.Children = append(append([]agenttrace.Node(nil), last.Children...), rows...)
		out[n-1] = last
		return out
	}
	return append(out, rows...)
}
