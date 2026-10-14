package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ike/internal/agenttrace"
	"ike/internal/complexity"
	"ike/internal/config"
	"ike/internal/deeplink"
)

// complexity_verdict_test.go covers the Stop-hook verdict (#2964): off, a
// Stop event runs nothing; on, it diffs the session's repository against
// HEAD off the loop and posts one note row into the trace pane of that
// session (warning tone when the excess rose), or a toast when no pane
// shows it; the wording of the verdict.

// verdictConfig sets complexity.agent_verdict and tight thresholds, so the
// fixture repository's extra branch raises the excess.
func verdictConfig(t *testing.T, on bool) {
	t.Helper()
	prev := config.Get()
	t.Cleanup(func() { config.Set(prev) })
	c, _ := config.Load(config.Options{})
	c.Complexity.CCNWarn = 1
	c.Complexity.CognitiveWarn = 1
	c.Complexity.CommentDensityWarn = 0
	c.Complexity.AgentVerdict = on
	config.Set(c)
}

func stopEvent(cwd string) deeplink.Event {
	return deeplink.Event{SessionID: "sess-v", CWD: cwd, Event: "Stop", TranscriptPath: "/tmp/p/sess-v.jsonl"}
}

func TestComplexityVerdictOffRunsNothing(t *testing.T) {
	m := sized(t, 100, 30)
	verdictConfig(t, false)
	if m.complexityVerdictCmd(stopEvent(t.TempDir())) != nil {
		t.Fatal("off: a Stop must run nothing")
	}
	verdictConfig(t, true)
	if m.complexityVerdictCmd(agentEvent("UserPromptSubmit", t.TempDir())) != nil {
		t.Fatal("only a Stop runs the verdict")
	}
	// Through Update: an unmatched Stop with the setting off yields no
	// command at all; on, it yields the verdict job.
	verdictConfig(t, false)
	if _, cmd := m.Update(AgentEventMsg{Event: stopEvent(t.TempDir())}); cmd != nil {
		t.Fatal("off: Update must schedule nothing")
	}
	verdictConfig(t, true)
	if _, cmd := m.Update(AgentEventMsg{Event: stopEvent(t.TempDir())}); cmd == nil {
		t.Fatal("on: Update must schedule the verdict job")
	}
}

func TestComplexityVerdictDiffsSessionRepoAndToasts(t *testing.T) {
	dir := gitRepo(t)
	m := sized(t, 100, 30)
	verdictConfig(t, true)
	cmd := m.complexityVerdictCmd(stopEvent(dir))
	if cmd == nil {
		t.Fatal("no job")
	}
	msg, ok := cmd().(cxVerdictMsg)
	if !ok || msg.sessionID != "sess-v" {
		t.Fatalf("job result = %#v", msg)
	}
	// f gained a branch: CCN 2 → 3 and cognitive 1 → 2 over thresholds of 1
	// is +2 excess.
	if msg.note.text != "complexity: MORE COMPLEX (excess +2)" || !msg.note.warn {
		t.Fatalf("note = %+v", msg.note)
	}
	tm, _ := m.Update(msg)
	m = asModel(tm)
	if !hasToast(m, "MORE COMPLEX (excess +2)") {
		t.Fatalf("without a trace pane the verdict is a toast: %v", toastTexts(m))
	}
	if notes := m.traceNotes["sess-v"]; len(notes) != 1 {
		t.Fatalf("notes = %+v", notes)
	}
	// Outside any repository the note says so instead of failing silently.
	bad := m.complexityVerdictCmd(stopEvent(t.TempDir()))().(cxVerdictMsg)
	if !strings.HasPrefix(bad.note.text, "complexity: no verdict") || !bad.note.warn {
		t.Fatalf("note = %+v", bad.note)
	}
}

func TestComplexityVerdictPostsRowIntoTracePane(t *testing.T) {
	m, dir := traceApp(t)
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	target := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(target, []byte("a\nb\nd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), []byte(transcriptLines("sess-1", projectRoot(), target, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	m = openTrace(t, m)
	p := m.agentTracePanel()
	if !p.HasSession() || p.Info().ID != "sess-1" {
		t.Fatalf("pane shows %+v", p.Info())
	}
	before := len(p.Nodes()[len(p.Nodes())-1].Children)

	at := time.Date(2026, 10, 14, 9, 30, 0, 0, time.Local)
	tm, _ := m.Update(cxVerdictMsg{sessionID: "sess-1", note: traceNote{at: at, text: "complexity: LESS COMPLEX (excess −12)"}})
	m = asModel(tm)
	if hasToast(m, "LESS COMPLEX") {
		t.Fatal("a pane showing the session gets the row, not a toast")
	}
	nodes := m.agentTracePanel().Nodes()
	last := nodes[len(nodes)-1]
	if last.Kind != agenttrace.NodeTurn || len(last.Children) != before+1 {
		t.Fatalf("the note must join the last turn: %+v", last)
	}
	note := last.Children[len(last.Children)-1]
	if note.Kind != agenttrace.NodeNote || note.Label != "complexity: LESS COMPLEX (excess −12)" || note.Detail != "09:30" || note.Warn || note.Event != -1 {
		t.Fatalf("note row = %+v", note)
	}
	rows := m.agentTracePanel().Rows()
	if !strings.HasPrefix(strings.TrimSpace(rows[len(rows)-1]), "note:") {
		t.Fatalf("the row must be visible: %v", rows)
	}
	// A worse verdict is the warning tone; a session the pane does not
	// show is a toast and stays filed for when it does.
	tm, _ = m.Update(cxVerdictMsg{sessionID: "sess-1", note: traceNote{at: at, text: "complexity: MORE COMPLEX (excess +3)", warn: true}})
	m = asModel(tm)
	nodes = m.agentTracePanel().Nodes()
	kids := nodes[len(nodes)-1].Children
	if n := kids[len(kids)-1]; !n.Warn || len(kids) != before+2 {
		t.Fatalf("second note = %+v (%d children)", n, len(kids))
	}
	tm, _ = m.Update(cxVerdictMsg{sessionID: "sess-other", note: traceNote{at: at, text: "complexity: UNCHANGED (excess 0)"}})
	m = asModel(tm)
	if !hasToast(m, "UNCHANGED") || len(m.traceNotes["sess-other"]) != 1 {
		t.Fatalf("other session: toasts=%v notes=%v", toastTexts(m), m.traceNotes)
	}
	// The notes survive a tree rebuild (a live read sets the nodes anew).
	if got := m.traceWithNotes(m.traceNodes, "sess-1"); len(got[len(got)-1].Children) != before+2 {
		t.Fatal("notes must be re-applied to a rebuilt tree")
	}
	if len(m.traceNodes[len(m.traceNodes)-1].Children) != before {
		t.Fatal("the base tree must stay untouched")
	}
}

func TestComplexityVerdictText(t *testing.T) {
	d := func(baseEx, headEx, baseCog, headCog int) complexity.Diff {
		var out complexity.Diff
		out.Base.Excess, out.Head.Excess = baseEx, headEx
		out.Base.CogSum, out.Head.CogSum = baseCog, headCog
		switch {
		case headEx > baseEx || (headEx == baseEx && headCog > baseCog):
			out.Verdict = complexity.MoreComplex
		case headEx < baseEx || (headEx == baseEx && headCog < baseCog):
			out.Verdict = complexity.LessComplex
		}
		return out
	}
	cases := map[string]string{
		complexityVerdictText(d(20, 8, 0, 0)):  "complexity: LESS COMPLEX (excess −12)",
		complexityVerdictText(d(5, 8, 0, 0)):   "complexity: MORE COMPLEX (excess +3)",
		complexityVerdictText(d(5, 5, 0, 0)):   "complexity: UNCHANGED (excess 0)",
		complexityVerdictText(d(5, 5, 10, 12)): "complexity: MORE COMPLEX (excess 0, cognitive +2)",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}
