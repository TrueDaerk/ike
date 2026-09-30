package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/agentask"
	"ike/internal/agenttrace"
	"ike/internal/config"
	"ike/internal/tracepanel"
)

// agentask_test.go covers the app half of agent.ask (#2845): the prompt
// over the trace's selected node, the fork command run through a fake
// `claude` that records its argv and cwd, the answer overlay, the error
// dialogs, cancellation, and — the invariant of the whole feature — that
// the original session's transcript is never written to.

// askApp opens the trace on a one-turn transcript and selects the Edit
// call's file row. It returns the model, the transcript path and the edited
// file.
func askApp(t *testing.T) (Model, string, string) {
	t.Helper()
	m, dir := traceApp(t)
	// The first-start LSP dialog would eat the scripted keys.
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	target := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(target, []byte("a\nb\nd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sess-1.jsonl")
	if err := os.WriteFile(path, []byte(transcriptLines("sess-1", projectRoot(), target, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	m = openTrace(t, m)
	p := m.agentTracePanel()
	if !p.HasSession() {
		t.Fatalf("pane shows no session:\n%s", p.View())
	}
	for range 3 {
		p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if cur := p.Current(); cur == nil || cur.Key != "e2/f0" {
		t.Fatalf("selection = %+v, want the file row", cur)
	}
	return m, path, target
}

// fakeClaude puts a claude script on a fresh PATH that records its argv
// (NUL-separated) and cwd, prints body and exits with code (with a
// resume-refusal on stderr when non-zero).
func fakeClaude(t *testing.T, body string, code int) (argsFile, cwdFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake")
	}
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	cwdFile = filepath.Join(dir, "cwd")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\0' \"$a\"; done > \"$FAKE_CLAUDE_ARGS\"\npwd > \"$FAKE_CLAUDE_CWD\"\n"
	if code != 0 {
		script += "echo 'No conversation found with session ID: sess-1' >&2\n"
	}
	script += "printf '%s' '" + body + "'\nexit " + itoa(code) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("FAKE_CLAUDE_ARGS", argsFile)
	t.Setenv("FAKE_CLAUDE_CWD", cwdFile)
	return argsFile, cwdFile
}

// typeAsk opens the prompt, types q and presses enter, returning the batch
// the run started.
func typeAsk(t *testing.T, m Model, q string) (Model, tea.Cmd) {
	t.Helper()
	out, _ := m.Update(AgentAskMsg{})
	m = out.(Model)
	if !m.agentAskOpen() || m.agentAsk.phase != askPrompting {
		t.Fatal("agent.ask must open the prompt")
	}
	for _, r := range q {
		out, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = out.(Model)
	}
	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if m.agentAsk == nil || m.agentAsk.phase != askRunning || cmd == nil {
		t.Fatal("enter must start the run")
	}
	return m, cmd
}

// runAsk executes the run half of the batch synchronously and feeds its
// result back, skipping the spinner tick.
func runAsk(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("start = %T, want a batch", msg)
	}
	var done tea.Msg
	for _, c := range batch {
		if c == nil {
			continue
		}
		if d, ok := c().(askDoneMsg); ok {
			done = d
		}
	}
	if done == nil {
		t.Fatal("the batch carried no run")
	}
	out, _ := m.Update(done)
	return out.(Model)
}

func TestAgentAskNeedsTraceSession(t *testing.T) {
	m := sized(t, 100, 30)
	out, _ := m.Update(AgentAskMsg{})
	m = out.(Model)
	if m.agentAskOpen() || m.agentAsk != nil {
		t.Fatal("without a traced session nothing must open")
	}
}

func TestAgentAskForksWithFakeClaudeAndNeverWritesTheOriginal(t *testing.T) {
	askSpinInterval = time.Millisecond
	m, transcript, target := askApp(t)
	before, err := os.ReadFile(transcript)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(transcript)
	argsFile, cwdFile := fakeClaude(t, `{"type":"result","subtype":"success","is_error":false,"result":"**Because** c became d.","session_id":"fork-1","duration_ms":1200,"total_cost_usd":0.0123}`, 0)

	m, cmd := typeAsk(t, m, "why?")
	// The running view shows the question and the spinner.
	if body := m.shell.Content().Render(80); !strings.Contains(body, "Q: why?") || !strings.Contains(body, "forking the session") {
		t.Fatalf("running view:\n%s", body)
	}
	m = runAsk(t, m, cmd)
	if m.agentAsk == nil || m.agentAsk.phase != askAnswered {
		t.Fatalf("phase = %v, err = %v", m.agentAsk.phase, m.agentAsk.err)
	}
	if !m.agentAskOpen() {
		t.Fatal("the answer must stay in the shell")
	}

	// The fork command: resume + fork on the cheaper model, no tools, one
	// turn, JSON, explain-only — and the node context ahead of the question.
	args, _ := os.ReadFile(argsFile)
	argv := strings.Split(strings.TrimSuffix(string(args), "\x00"), "\x00")
	want := agentask.Command("sess-1", agentask.Defaults, "")
	if len(argv) != len(want)-1 || strings.Join(argv[:len(argv)-1], " ") != strings.Join(want[1:len(want)-1], " ") {
		t.Fatalf("fake received %q", argv)
	}
	prompt := argv[len(argv)-1]
	for _, s := range []string{"file: " + target + ":3 (edit)", "tool: Edit " + target, "assistant said: Editing turn 1.", "turn: #1 (", "Question: why?"} {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt lacks %q:\n%s", s, prompt)
		}
	}
	// It ran in the session's cwd, so the fork lands in the same project dir.
	cwd, _ := os.ReadFile(cwdFile)
	if got, _ := filepath.EvalSymlinks(strings.TrimSpace(string(cwd))); got != mustEvalSymlinks(projectRoot()) {
		t.Fatalf("fake ran in %q, want %q", got, projectRoot())
	}

	// The original transcript is byte-for-byte what it was.
	after, _ := os.ReadFile(transcript)
	st2, _ := os.Stat(transcript)
	if string(after) != string(before) || !st2.ModTime().Equal(st.ModTime()) {
		t.Fatal("the original session's transcript was written to")
	}
	// The fork is remembered and excluded from discovery.
	if len(m.askForks) != 1 || m.askForks[0] != "fork-1" {
		t.Fatalf("forks = %v", m.askForks)
	}

	// The overlay: question, context, rendered markdown, run facts. Wide
	// enough that the temp-dir path of the file line never wraps.
	body := ansiSeq.ReplaceAllString(m.shell.Content().Render(400), "")
	for _, s := range []string{"Q: why?", "context:", "main.go:3 (edit)", "Because", "c became d", "1.2s", "$0.0123", "fork fork"} {
		if !strings.Contains(body, s) {
			t.Errorf("answer lacks %q:\n%s", s, body)
		}
	}
	if strings.Contains(body, "**Because**") {
		t.Error("markdown must be rendered, not shown raw")
	}
	if strings.Contains(m.shell.Content().Title(), "failed") {
		t.Errorf("title = %q", m.shell.Content().Title())
	}
	// Esc closes.
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.agentAskOpen() || m.agentAsk != nil {
		t.Fatal("esc must close the answer")
	}
}

func mustEvalSymlinks(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func TestAgentAskHidesContextWhenOff(t *testing.T) {
	askSpinInterval = time.Millisecond
	m, _, target := askApp(t)
	prev := config.Get()
	cfg := *prev
	cfg.Agent.Ask.ShowContext = false
	config.Set(&cfg)
	t.Cleanup(func() { config.Set(prev) })
	fakeClaude(t, `{"type":"result","subtype":"success","result":"ok","session_id":"fork-2"}`, 0)
	m, cmd := typeAsk(t, m, "why?")
	m = runAsk(t, m, cmd)
	body := ansiSeq.ReplaceAllString(m.shell.Content().Render(120), "")
	if strings.Contains(body, "context:") || strings.Contains(body, target) {
		t.Fatalf("show_context off must hide the context:\n%s", body)
	}
	if !strings.Contains(body, "ok") {
		t.Fatalf("answer missing:\n%s", body)
	}
}

func TestAgentAskMissingClaudeIsADialog(t *testing.T) {
	askSpinInterval = time.Millisecond
	m, _, _ := askApp(t)
	t.Setenv("PATH", t.TempDir())
	m, cmd := typeAsk(t, m, "why?")
	m = runAsk(t, m, cmd)
	if m.agentAsk == nil || m.agentAsk.phase != askFailed {
		t.Fatal("a missing claude must fail the ask")
	}
	if title, body := m.shell.Content().Title(), m.shell.Content().Render(80); title != "Ask failed" || !strings.Contains(body, "not installed") || !strings.Contains(body, "PATH") {
		t.Fatalf("dialog = %q:\n%s", title, body)
	}
	out, _ := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if m = out.(Model); m.agentAskOpen() {
		t.Fatal("q must close the dialog")
	}
}

func TestAgentAskResumeRefusedIsADialog(t *testing.T) {
	askSpinInterval = time.Millisecond
	m, _, _ := askApp(t)
	fakeClaude(t, "", 1)
	m, cmd := typeAsk(t, m, "why?")
	m = runAsk(t, m, cmd)
	if m.agentAsk == nil || m.agentAsk.phase != askFailed {
		t.Fatal("a refused resume must fail the ask")
	}
	body := m.shell.Content().Render(80)
	if !strings.Contains(body, "refused to resume session sess-1") || !strings.Contains(body, "No conversation found") {
		t.Fatalf("dialog:\n%s", body)
	}
}

func TestAgentAskEscCancelsRunAndLateResultIsIgnored(t *testing.T) {
	askSpinInterval = time.Millisecond
	m, _, _ := askApp(t)
	fakeClaude(t, `{"type":"result","subtype":"success","result":"late","session_id":"fork-3"}`, 0)
	m, cmd := typeAsk(t, m, "why?")
	gen := m.agentAsk.gen
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.agentAskOpen() || m.agentAsk != nil {
		t.Fatal("esc while running must close")
	}
	// The run finishes anyway; its result and spinner tick land on nothing.
	m = runAskLoose(m, cmd)
	if m.agentAsk != nil || m.shell.IsOpen() {
		t.Fatal("a late result must not reopen the shell")
	}
	out, _ = m.Update(askSpinMsg{gen: gen})
	if m = out.(Model); m.shell.IsOpen() {
		t.Fatal("a late spinner tick must not reopen the shell")
	}
}

// runAskLoose is runAsk without the assertions, for a cancelled run.
func runAskLoose(m Model, cmd tea.Cmd) Model {
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			if d, ok := c().(askDoneMsg); ok {
				out, _ := m.Update(d)
				m = out.(Model)
			}
		}
	}
	return m
}

func TestAgentAskPromptKeysAndPaneKey(t *testing.T) {
	m, _, _ := askApp(t)
	// 'a' in the pane asks about the selected row.
	cmd := m.agentTracePanel().Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if cmd == nil {
		t.Fatal("a in the pane yielded nothing")
	}
	msg, ok := cmd().(tracepanel.AskMsg)
	if !ok {
		t.Fatalf("a = %T", cmd())
	}
	out, _ := m.Update(msg)
	m = out.(Model)
	if !m.agentAskOpen() {
		t.Fatal("AskMsg must open the prompt")
	}
	body := m.shell.Content().Render(80)
	if !strings.Contains(body, "about: ") || !strings.Contains(body, "file:") || !strings.Contains(m.shell.Content().Title(), "Ask the agent") {
		t.Fatalf("prompt:\n%s", body)
	}
	// Enter on an empty question stays open with a note.
	out, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if cmd != nil || !m.agentAskOpen() || m.agentAsk.phase != askPrompting || m.agentAsk.problem == "" {
		t.Fatal("an empty question must not run")
	}
	// Paste fills the line; ctrl+u clears it.
	if !m.pasteAgentAskPrompt("why this") {
		t.Fatal("paste must land in the question")
	}
	if got := m.agentAsk.input.Text; got != "why this" {
		t.Fatalf("pasted = %q", got)
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m = out.(Model)
	if m.agentAsk.input.Text != "" {
		t.Fatal("ctrl+u must clear the question")
	}
	// Esc cancels without running anything.
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.agentAskOpen() || m.agentAsk != nil {
		t.Fatal("esc must close the prompt")
	}
	// A question about the session as a whole: nothing selected → no node.
	m.agentTracePanel().Reset()
	m.agentTracePanel().Set(nil, tracepanel.Info{ID: "sess-1", CWD: projectRoot()})
	out, _ = m.Update(AgentAskMsg{})
	m = out.(Model)
	if !m.agentAskOpen() || m.agentAsk.node != nil || !strings.Contains(m.shell.Content().Render(80), "the whole session") {
		t.Fatalf("session-wide ask:\n%s", m.shell.Content().Render(80))
	}
}

// readArgv reads the argv the fake claude recorded.
func readArgv(t *testing.T, argsFile string) []string {
	t.Helper()
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(args), "\x00"), "\x00")
}

// typeQuestion types q into the open prompt and presses enter.
func typeQuestion(t *testing.T, m Model, q string) (Model, tea.Cmd) {
	t.Helper()
	for _, r := range q {
		out, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = out.(Model)
	}
	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if m.agentAsk == nil || m.agentAsk.phase != askRunning || cmd == nil {
		t.Fatal("enter must start the run")
	}
	return m, cmd
}

// TestAgentAskFollowUpChainsOnTheFork (#2844): `f` on an answer asks again
// on the fork — never the original — the chain shows in the overlay, the
// fork is kept for the node, reopening the ask on that node continues it,
// and ctrl+n starts a fresh fork.
func TestAgentAskFollowUpChainsOnTheFork(t *testing.T) {
	askSpinInterval = time.Millisecond
	m, transcript, _ := askApp(t)
	before, _ := os.ReadFile(transcript)
	argsFile, _ := fakeClaude(t, `{"type":"result","subtype":"success","result":"First answer.","session_id":"fork-1"}`, 0)
	m, cmd := typeAsk(t, m, "why?")
	m = runAsk(t, m, cmd)
	if m.agentAsk.phase != askAnswered {
		t.Fatalf("phase = %v, err = %v", m.agentAsk.phase, m.agentAsk.err)
	}
	if body := ansiSeq.ReplaceAllString(m.shell.Content().Render(120), ""); !strings.Contains(body, "f follow up") {
		t.Fatalf("answer lacks the follow-up key:\n%s", body)
	}
	if !agentaskTagged(readArgv(t, argsFile)) {
		t.Fatal("the first ask's prompt must carry the marker")
	}

	// f: the prompt again, on the fork.
	out, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = out.(Model)
	if !m.agentAskOpen() || m.agentAsk.phase != askPrompting || m.agentAsk.followUp != "fork-1" {
		t.Fatalf("f = phase %v, followUp %q", m.agentAsk.phase, m.agentAsk.followUp)
	}
	body := ansiSeq.ReplaceAllString(m.shell.Content().Render(120), "")
	for _, s := range []string{"Q: why?", "First answer.", "follow-up on fork fork", "ctrl+n new fork"} {
		if !strings.Contains(body, s) {
			t.Errorf("follow-up prompt lacks %q:\n%s", s, body)
		}
	}

	argsFile, _ = fakeClaude(t, `{"type":"result","subtype":"success","result":"Second answer.","session_id":"fork-1"}`, 0)
	m, cmd = typeQuestion(t, m, "and then?")
	if body := m.shell.Content().Render(80); !strings.Contains(body, "asking fork fork") {
		t.Fatalf("running view:\n%s", body)
	}
	m = runAsk(t, m, cmd)
	if m.agentAsk.phase != askAnswered {
		t.Fatalf("follow-up phase = %v, err = %v", m.agentAsk.phase, m.agentAsk.err)
	}
	argv := readArgv(t, argsFile)
	want := agentask.FollowUp("fork-1", agentask.Defaults, "")
	if strings.Join(argv[:len(argv)-1], "\x00") != strings.Join(want[1:len(want)-1], "\x00") {
		t.Fatalf("follow-up argv = %q", argv)
	}
	for _, a := range argv {
		if a == "sess-1" || a == "--fork-session" {
			t.Fatalf("a follow-up must resume the fork only: %q", argv)
		}
	}
	if p := argv[len(argv)-1]; !agentaskTagged(argv) || strings.Contains(p, "Context from") || !strings.HasSuffix(p, "and then?") {
		t.Fatalf("follow-up prompt = %q", p)
	}
	body = ansiSeq.ReplaceAllString(m.shell.Content().Render(120), "")
	for _, s := range []string{"Q: why?", "First answer.", "Q: and then?", "Second answer."} {
		if !strings.Contains(body, s) {
			t.Errorf("chain lacks %q:\n%s", s, body)
		}
	}
	if after, _ := os.ReadFile(transcript); string(after) != string(before) {
		t.Fatal("the original session's transcript was written to")
	}

	// Reopening the ask on the same node continues the kept fork.
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	out, _ = m.Update(AgentAskMsg{})
	m = out.(Model)
	if m.agentAsk == nil || m.agentAsk.followUp != "fork-1" {
		t.Fatalf("reopened ask = %+v, want the kept fork", m.agentAsk)
	}
	// ctrl+n drops it: the next question forks the original afresh.
	out, _ = m.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	m = out.(Model)
	if m.agentAsk.followUp != "" {
		t.Fatal("ctrl+n must start a fresh fork")
	}
	argsFile, _ = fakeClaude(t, `{"type":"result","subtype":"success","result":"Fresh.","session_id":"fork-2"}`, 0)
	m, cmd = typeQuestion(t, m, "again?")
	m = runAsk(t, m, cmd)
	if argv := readArgv(t, argsFile); argv[2] != "sess-1" || argv[3] != "--fork-session" {
		t.Fatalf("fresh fork argv = %q", argv)
	}
	if got := m.askNodeForks[m.agentAsk.forkKey]; got != "fork-2" {
		t.Fatalf("kept fork = %q, want fork-2", got)
	}
}

// TestAgentAskFollowUpOnVanishedForkForgetsIt: a refused resume of a kept
// fork fails the ask and forgets the fork, so the next ask forks afresh.
func TestAgentAskFollowUpOnVanishedForkForgetsIt(t *testing.T) {
	askSpinInterval = time.Millisecond
	m, _, _ := askApp(t)
	fakeClaude(t, `{"type":"result","subtype":"success","result":"ok","session_id":"fork-9"}`, 0)
	m, cmd := typeAsk(t, m, "why?")
	m = runAsk(t, m, cmd)
	out, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = out.(Model)
	fakeClaude(t, "", 1)
	m, cmd = typeQuestion(t, m, "more?")
	m = runAsk(t, m, cmd)
	if m.agentAsk.phase != askFailed {
		t.Fatalf("phase = %v", m.agentAsk.phase)
	}
	if _, ok := m.askNodeForks[m.agentAsk.forkKey]; ok {
		t.Fatal("a vanished fork must be forgotten")
	}
}

// agentaskTagged reports whether the recorded prompt carries the ask marker.
func agentaskTagged(argv []string) bool {
	return len(argv) > 0 && agenttrace.IsAskPrompt(argv[len(argv)-1])
}
