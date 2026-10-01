package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/agenttrace"
	"ike/internal/deeplink"
	"ike/internal/pane"
	"ike/internal/tracepanel"
)

// agenttrace_history_test.go covers the app half of the session history
// (#2860): the record written at turn boundaries and on the session's end,
// a /clear (a new SessionStart for the same terminal) closing the first
// record and starting a second, the picker listing and opening a stored
// session read-only with esc returning to the live one, agent.ask on a
// stored session gated on its transcript, and agent.trace.import.

// drain runs cmd, unwrapping batches, and feeds every message it yields
// into the model. Tick commands are not run (they would sleep).
func traceDrain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case traceTickMsg:
		default:
			out, next := m.Update(msg)
			m = out.(Model)
			queue = append(queue, next)
		}
	}
	return m
}

// reread runs one incremental read the way a tick does.
func traceReread(t *testing.T, m Model) Model {
	t.Helper()
	m.traceBusy = false
	cmd := m.traceReadCmd()
	if cmd == nil {
		t.Fatal("no reader to read")
	}
	out, next := m.Update(cmd())
	m = out.(Model)
	_ = next
	return m
}

func traceAppendTurn(t *testing.T, path, id, target string, turn int) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(transcriptLines(id, projectRoot(), target, turn)); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// traceNotices joins the notifications the model raised so far (Update
// drains the host queue into the history ring).
func traceNotices(m Model) string {
	var parts []string
	for _, h := range m.history {
		parts = append(parts, h.text)
	}
	return strings.Join(parts, "\n")
}

func TestAgentTraceHistoryRecordsLiveSession(t *testing.T) {
	m, dir := traceApp(t)
	target := filepath.Join(t.TempDir(), "main.go")
	path := filepath.Join(dir, "sess-1.jsonl")
	if err := os.WriteFile(path, []byte(transcriptLines("sess-1", projectRoot(), target, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	m = openTrace(t, m)
	st := traceStore()
	if !strings.HasPrefix(st.Dir, os.Getenv("IKE_CONFIG_DIR")) {
		t.Fatalf("store dir = %s, want it under IKE_CONFIG_DIR", st.Dir)
	}
	all, err := st.List()
	if err != nil || len(all) != 1 || all[0].ID != "sess-1" || all[0].Turns != 1 || all[0].Ended || all[0].Source != "live" {
		t.Fatalf("after the first read: %+v, %v", all, err)
	}
	if m.traceSaved.id != "sess-1" || m.traceSaved.turns != 1 {
		t.Fatalf("saved state = %+v", m.traceSaved)
	}
	// An idle read writes nothing; a new turn does.
	before, _ := os.Stat(st.Path("sess-1"))
	time.Sleep(20 * time.Millisecond)
	m = traceReread(t, m)
	after, _ := os.Stat(st.Path("sess-1"))
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("an idle read must not rewrite the record")
	}
	traceAppendTurn(t, path, "sess-1", target, 2)
	m = traceReread(t, m)
	rec, err := st.Load("sess-1")
	if err != nil || rec.Turns != 2 || rec.FilesChanged != 1 || rec.FirstPrompt != "Turn 1 prompt" || rec.Ended {
		t.Fatalf("record after turn 2 = %+v, %v", rec, err)
	}
	// The record round-trips into the same tree the pane shows.
	if got, want := strings.Join(traceOutlineKeys(agenttrace.BuildTree(rec.Session())), ","), strings.Join(traceOutlineKeys(m.agentTracePanel().Nodes()), ","); got != want {
		t.Fatalf("stored tree %s != live tree %s", got, want)
	}
	// SessionEnd files the record as ended.
	m.traceSession.Ended = true
	m = traceReread(t, m)
	if rec, _ := st.Load("sess-1"); rec == nil || !rec.Ended {
		t.Fatal("the end of the session must close the record")
	}
}

// outlineKeys flattens a tree into its keys.
func traceOutlineKeys(nodes []agenttrace.Node) []string {
	var out []string
	agenttrace.Walk(nodes, func(n *agenttrace.Node) { out = append(out, n.Key) })
	return out
}

// hookedTrace opens a tool pane bound by hook to a session with a one-turn
// transcript and the trace pane on it.
func traceHooked(t *testing.T) (Model, string, string, string) {
	t.Helper()
	withTools(t, sleepTool("watcher"))
	m := sized(t, 120, 40)
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	dir := filepath.Join(cfg, "projects", agenttrace.EncodeCWD(projectRoot()))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(ToolOpenMsg{Name: "watcher"})
	m = out.(Model)
	inst := m.toolPane("watcher")
	if inst == nil {
		t.Fatal("tool pane did not open")
	}
	t.Cleanup(func() { inst.Terminal().Close() })
	key := inst.Terminal().SessionKey()
	target := filepath.Join(t.TempDir(), "main.go")
	path := filepath.Join(dir, "sess-1.jsonl")
	if err := os.WriteFile(path, []byte(transcriptLines("sess-1", projectRoot(), target, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	ev := deeplink.Event{SessionID: "sess-1", CWD: projectRoot(), Event: "SessionStart", TranscriptPath: path, Pane: key, PID: os.Getpid()}
	out, _ = m.Update(AgentEventMsg{Event: ev})
	m = out.(Model)
	m = openTrace(t, m)
	if info := m.agentTracePanel().Info(); info.ID != "sess-1" || !info.FromHook {
		t.Fatalf("trace follows %+v", info)
	}
	return m, key, dir, target
}

func TestAgentTraceClearStartsSecondRecordAndPickerSwitches(t *testing.T) {
	m, key, dir, target := traceHooked(t)
	st := traceStore()
	if all, _ := st.List(); len(all) != 1 || all[0].ID != "sess-1" {
		t.Fatalf("records before /clear: %+v", all)
	}

	// /clear: Claude starts a new session in the same terminal and the hook
	// announces it. The first record closes, the second starts.
	path2 := filepath.Join(dir, "sess-2.jsonl")
	if err := os.WriteFile(path2, []byte(transcriptLines("sess-2", projectRoot(), target, 2)), 0o644); err != nil {
		t.Fatal(err)
	}
	ev := deeplink.Event{SessionID: "sess-2", CWD: projectRoot(), Event: "SessionStart", TranscriptPath: path2, Pane: key, PID: os.Getpid()}
	out, cmd := m.Update(AgentEventMsg{Event: ev})
	m = traceDrain(t, out.(Model), cmd)
	p := m.agentTracePanel()
	if p.Info().ID != "sess-2" || m.traceReader.Path() != path2 {
		t.Fatalf("trace must follow the new session, shows %+v", p.Info())
	}
	all, err := st.List()
	if err != nil || len(all) != 2 {
		t.Fatalf("records after /clear: %+v, %v", all, err)
	}
	byID := map[string]agenttrace.Summary{}
	for _, s := range all {
		byID[s.ID] = s
	}
	if !byID["sess-1"].Ended || byID["sess-2"].Ended {
		t.Fatalf("sess-1 must be closed, sess-2 live: %+v", byID)
	}

	// 's' lists both, newest first, the live one marked.
	out, cmd = m.Update(p.Update(tea.KeyPressMsg{Code: 's', Text: "s"})())
	m = traceDrain(t, out.(Model), cmd)
	p = m.agentTracePanel()
	if !p.PickerOpen() {
		t.Fatalf("picker must open:\n%s", p.View())
	}
	rows := p.PickerRows()
	if strings.Join(rows, ",") != "sess-2,sess-1" {
		t.Fatalf("picker rows = %v", rows)
	}
	if cur := p.PickerCurrent(); cur == nil || !cur.Live {
		t.Fatalf("the live session must be marked: %+v", cur)
	}
	view := ansiSeq.ReplaceAllString(p.View(), "")
	if !strings.Contains(view, "● ") || !strings.Contains(view, "Turn 1 prompt") || !strings.Contains(view, "Turn 2 prompt") {
		t.Fatalf("picker view:\n%s", view)
	}

	// Enter on the stored session shows it read-only; a live read leaves it
	// alone; esc returns to the live one.
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	out, cmd = m.Update(p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})())
	m = traceDrain(t, out.(Model), cmd)
	p = m.agentTracePanel()
	if !p.Stored() || p.Info().ID != "sess-1" || m.traceHistoryID != "sess-1" {
		t.Fatalf("stored session not shown: %+v", p.Info())
	}
	if view := ansiSeq.ReplaceAllString(p.View(), ""); !strings.Contains(view, "history · ") || !strings.Contains(view, "#1 Turn 1 prompt") {
		t.Fatalf("stored view:\n%s", view)
	}
	traceAppendTurn(t, path2, "sess-2", target, 3)
	m = traceReread(t, m)
	p = m.agentTracePanel()
	if !p.Stored() || p.Info().ID != "sess-1" {
		t.Fatal("a live read must not replace the stored view")
	}
	if rec, _ := st.Load("sess-2"); rec == nil || rec.Turns != 2 {
		t.Fatalf("the live session keeps being filed meanwhile: %+v", rec)
	}
	// The graph view of the stored session works too.
	p.SetViewMode(tracepanel.ViewGraph)
	if cur := p.CurrentStop(); cur == nil || cur.Key != "t1" {
		t.Fatalf("stored graph = %+v", cur)
	}
	out, cmd = m.Update(p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})())
	m = traceDrain(t, out.(Model), cmd)
	p = m.agentTracePanel()
	if p.Stored() || m.traceHistoryID != "" || p.Info().ID != "sess-2" || p.Info().Turns != 2 {
		t.Fatalf("esc must return to the live session: %+v", p.Info())
	}
}

func TestAgentTraceAskOnStoredSessionNeedsTranscript(t *testing.T) {
	m, key, dir, target := traceHooked(t)
	path2 := filepath.Join(dir, "sess-2.jsonl")
	if err := os.WriteFile(path2, []byte(transcriptLines("sess-2", projectRoot(), target, 2)), 0o644); err != nil {
		t.Fatal(err)
	}
	ev := deeplink.Event{SessionID: "sess-2", CWD: projectRoot(), Event: "SessionStart", TranscriptPath: path2, Pane: key, PID: os.Getpid()}
	out, cmd := m.Update(AgentEventMsg{Event: ev})
	m = traceDrain(t, out.(Model), cmd)
	out, cmd = m.Update(tracepanel.ShowHistoryMsg{ID: "sess-1"})
	m = traceDrain(t, out.(Model), cmd)
	p := m.agentTracePanel()
	if !p.Stored() {
		t.Fatal("stored session not shown")
	}
	m.history = nil

	// With the transcript on disk the ask prompt opens on the stored id.
	out, _ = m.Update(AgentAskMsg{})
	m = out.(Model)
	if !m.agentAskOpen() || m.agentAsk.session.ID != "sess-1" || m.agentAsk.session.Transcript != filepath.Join(dir, "sess-1.jsonl") {
		t.Fatalf("ask on a stored session = %+v", m.agentAsk)
	}
	m.closeAgentAsk()

	// Claude pruned the transcript: a explains, no prompt.
	if err := os.Remove(filepath.Join(dir, "sess-1.jsonl")); err != nil {
		t.Fatal(err)
	}
	out, _ = m.Update(tracepanel.AskMsg{})
	m = out.(Model)
	if m.agentAskOpen() {
		t.Fatal("no prompt without a transcript")
	}
	if got := traceNotices(m); !strings.Contains(got, "transcript") || !strings.Contains(got, "gone") {
		t.Fatalf("notice = %q", got)
	}
}

func TestAgentTraceImportCommand(t *testing.T) {
	m, dir := traceApp(t)
	target := filepath.Join(t.TempDir(), "main.go")
	orig := filepath.Join(dir, "sess-a.jsonl")
	if err := os.WriteFile(orig, []byte(transcriptLines("sess-a", projectRoot(), target, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	// A fork: the same root line under another id.
	fork := strings.ReplaceAll(transcriptLines("sess-a", projectRoot(), target, 1), `"sessionId":"sess-a"`, `"sessionId":"sess-b"`)
	if err := os.WriteFile(filepath.Join(dir, "sess-b.jsonl"), []byte(fork), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.jsonl"), []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, cmd := m.Update(AgentTraceImportMsg{})
	m = out.(Model)
	if !m.traceImporting || m.traceImportSegment() == "" {
		t.Fatal("the import must show in the status bar")
	}
	m = traceDrain(t, m, cmd)
	if m.traceImporting || m.traceImportSegment() != "" {
		t.Fatal("the segment must clear when the import is done")
	}
	got := traceNotices(m)
	for _, want := range []string{"1 imported", "1 forks skipped", "unreadable: broken.jsonl"} {
		if !strings.Contains(got, want) {
			t.Errorf("notice %q lacks %q", got, want)
		}
	}
	all, err := traceStore().List()
	if err != nil || len(all) != 1 || all[0].ID != "sess-a" || all[0].Source != "import" || !all[0].Ended {
		t.Fatalf("store after import: %+v, %v", all, err)
	}
	// Running it again imports nothing new.
	out, cmd = m.Update(AgentTraceImportMsg{})
	m = traceDrain(t, out.(Model), cmd)
	if got := traceNotices(m); !strings.Contains(got, "0 imported") || !strings.Contains(got, "1 unchanged") {
		t.Fatalf("second import notice = %q", got)
	}
	// The picker opens even without a live session, listing the import.
	out, cmd = m.Update(AgentTraceHistoryMsg{})
	m = traceDrain(t, out.(Model), cmd)
	p := m.agentTracePanel()
	if p == nil || m.activeWS().Panes.Get(pane.AgentTraceKey) == nil || !p.PickerOpen() || len(p.PickerRows()) != 1 {
		t.Fatalf("agent.trace.history must open the pane and the picker")
	}
}
