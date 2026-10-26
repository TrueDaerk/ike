package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/agentask"
	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/config"
	"ike/internal/scratch"
)

// complexity_agent_test.go covers the Complexity window's `a` (#2964): the
// prompt built from the row's source, sites and candidate, the fresh
// `claude -p` argv run in the project root through a fake claude, the busy
// refusal, the missing-claude and failed-run notifications — and the
// end-to-end apply of #3009: the diff applied to the buffer as one undoable
// edit, a mismatching hunk, the one no-diff follow-up, agent_apply off, the
// status segment, its tick, and cancel.

// cxAgentApp opens the Complexity window on one measured function of a
// temp file and returns the model, the file and the request `a` sends.
func cxAgentApp(t *testing.T) (Model, string, complexitypanel.AskAgentMsg) {
	t.Helper()
	m, _ := timeApp(t)
	m = dismissOnboarding(m)
	out, _ := m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	p := m.complexityPanel()
	if p == nil {
		t.Fatal("no panel")
	}
	file := filepath.Join(t.TempDir(), "a.go")
	src := "package p\n\nfunc render(a int) int {\n\tif a > 0 {\n\t\tfor i := 0; i < a; i++ {\n\t\t\ta--\n\t\t}\n\t}\n\treturn a\n}\n\nfunc tiny() {}\n"
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	fn := complexity.Function{Name: "render", Kind: complexity.KindFunction, Line: 2, EndLine: 9, NLOC: 8, CCN: 3, Cognitive: 3,
		Sites: []complexity.Site{
			{Line: 3, Col: 1, EndLine: 7, Kind: "if", CCN: 1, Cognitive: 1},
			{Line: 4, Col: 2, EndLine: 6, Kind: "for", CCN: 1, Cognitive: 2, Nesting: 1, Level: 1},
		},
		Candidates: []complexity.Candidate{{Line: 4, EndLine: 6, Kind: "for", CogSaving: 1, NewCCN: 2, NewCog: 1, RestCCN: 2, RestCog: 1}},
	}
	plain := complexity.Function{Name: "tiny", Kind: complexity.KindFunction, Line: 11, EndLine: 11, NLOC: 1, CCN: 1}
	p.Set(complexity.View{Gen: 1, Files: map[string][]complexity.Function{file: {fn, plain}}}, complexity.Stats{Enabled: true})
	// 'a' reaches the focused pane and names the selected row (render, CCN
	// descending).
	tm, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = asModel(tm)
	if cmd == nil {
		t.Fatal("a must send the pane's request")
	}
	req, ok := cmd().(complexitypanel.AskAgentMsg)
	if !ok || req.Name != "render" || req.Path != file || req.Line != 2 || req.EndLine != 9 || len(req.Fn.Sites) != 2 {
		t.Fatalf("request = %+v ok=%v", req, ok)
	}
	return m, file, req
}

// fakeCxClaude puts a claude script on a fresh PATH that records its argv
// (NUL-separated) and cwd, prints body on stdout and stderr on stderr, and
// exits with code.
func fakeCxClaude(t *testing.T, body, stderr string, code int) (argsFile, cwdFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake")
	}
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	cwdFile = filepath.Join(dir, "cwd")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\0' \"$a\"; done > \"$FAKE_CX_ARGS\"\npwd > \"$FAKE_CX_CWD\"\n"
	if stderr != "" {
		script += "echo '" + stderr + "' >&2\n"
	}
	script += "printf '%s' '" + body + "'\nexit " + itoa(code) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("FAKE_CX_ARGS", argsFile)
	t.Setenv("FAKE_CX_CWD", cwdFile)
	return argsFile, cwdFile
}

// startCxAgent feeds the request through Update and returns the batch
// Update produced (the run sits inside it beside the settled-pass work).
func startCxAgent(t *testing.T, m Model, req complexitypanel.AskAgentMsg) (Model, tea.Cmd) {
	t.Helper()
	tm, cmd := m.Update(req)
	return asModel(tm), cmd
}

// cxAgentResult runs the commands of a batch until the agent run's result
// turns up; nil when the batch carried no run.
func cxAgentResult(cmd tea.Cmd) *cxAgentDoneMsg {
	pending := []tea.Cmd{cmd}
	for len(pending) > 0 {
		c := pending[0]
		pending = pending[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			pending = append(pending, msg...)
		case cxAgentDoneMsg:
			return &msg
		}
	}
	return nil
}

func TestComplexityAskAgentRunsFreshSessionAndAppliesDiff(t *testing.T) {
	m, file, req := cxAgentApp(t)
	answer := `Extracted the loop into a helper.\n\n--- a/a.go\n+++ b/a.go\n@@ -3,4 +3,4 @@\n-\tif a > 0 {\n+\tif a > 0 { // simpler\n\nThat keeps the signature.`
	argsFile, cwdFile := fakeCxClaude(t, `{"type":"result","subtype":"success","is_error":false,"result":"`+answer+`","session_id":"fresh-1","num_turns":1}`, "", 0)

	m, run := startCxAgent(t, m, req)
	if run == nil || !hasToast(m, "asking") || len(m.cxAgent.running) != 1 {
		t.Fatalf("the request must start the run and say so: %v", toastTexts(m))
	}
	if seg := m.complexityAgentSegment(); !strings.HasPrefix(seg, "[agent: simplify render 0:0") {
		t.Errorf("segment = %q, want the running function and its clock", seg)
	}
	// A second `a` on the same function while it runs only notifies.
	m, again := startCxAgent(t, m, req)
	if !hasToast(m, "still working on render") || len(m.cxAgent.running) != 1 || cxAgentResult(again) != nil {
		t.Fatalf("a busy function must refuse: toasts=%v", toastTexts(m))
	}

	done := cxAgentResult(run)
	if done == nil || done.err != nil || !done.res.HasDiff || done.res.Retried {
		t.Fatalf("run = %+v", done)
	}
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	if argv[0] != "-p" {
		t.Fatalf("argv = %q, want print mode", argv)
	}
	for _, a := range argv {
		if a == "--resume" || a == "--fork-session" {
			t.Fatalf("a fresh session must not resume: %q", argv)
		}
	}
	want := agentask.SimplifyCommand(askOptions(), agentask.SimplifyPrompt(complexitySimplify(req, displayPath(file), "func render(a int) int {\n\tif a > 0 {\n\t\tfor i := 0; i < a; i++ {\n\t\t\ta--\n\t\t}\n\t}\n\treturn a\n}")))
	if strings.Join(argv, "\x00") != strings.Join(want[1:], "\x00") {
		t.Fatalf("argv =\n%q\nwant\n%q", argv, want[1:])
	}
	prompt := argv[len(argv)-1]
	for _, part := range []string{"Function: render (CCN 3, cognitive 3)", "Complexity sites", "- line 5 for: +1 ccn, +2 cognitive (of which 1 nesting)", "Best extraction candidate: lines 5–7 (for)", "```go\nfunc render(a int) int {", agentask.SimplifyInstruction, "You have no tools"} {
		if !strings.Contains(prompt, part) {
			t.Errorf("prompt lacks %q:\n%s", part, prompt)
		}
	}
	cwd, _ := os.ReadFile(cwdFile)
	wantCwd, _ := filepath.EvalSymlinks(projectRoot())
	if gotCwd, _ := filepath.EvalSymlinks(strings.TrimSpace(string(cwd))); gotCwd != wantCwd {
		t.Errorf("ran in %q, want the project root %q", gotCwd, wantCwd)
	}

	tm, cmd := m.Update(*done)
	m = asModel(tm)
	// The diff landed in the buffer as one edit: dirty, the disk untouched,
	// the function's file in front.
	ed := m.activeEditor()
	if ed == nil || canonicalPath(ed.Path()) != canonicalPath(file) {
		t.Fatalf("the function's file must be in front, got %v", ed)
	}
	if !strings.Contains(ed.Text(), "\tif a > 0 { // simpler\n") || !ed.Dirty() {
		t.Fatalf("the diff must be applied, dirty:\n%s", ed.Text())
	}
	if data, _ := os.ReadFile(file); strings.Contains(string(data), "simpler") {
		t.Fatal("the apply must not write the file")
	}
	// The re-measure reports before → after with the session log and model.
	applied := findCxMsg[cxAgentAppliedMsg](cmd)
	if applied == nil {
		t.Fatal("the apply must re-measure the function")
	}
	tm, _ = m.Update(*applied)
	m = asModel(tm)
	for _, part := range []string{"applied the agent's diff to render (", "excess) — CCN 3 → ", ", cognitive 3 → ", "model sonnet", "fresh-1.jsonl"} {
		if !hasToast(m, part) {
			t.Errorf("toasts lack %q: %v", part, toastTexts(m))
		}
	}
	// The explanation waits in a scratch behind the file, header first.
	body := newestAgentScratch(t)
	for _, part := range []string{"# Simplify render\n", "Model: sonnet · session log: ", "fresh-1.jsonl", "> Applied to the buffer", "Extracted the loop into a helper.\n\n```diff\n--- a/a.go\n+++ b/a.go\n@@ -3,4 +3,4 @@\n-\tif a > 0 {\n+\tif a > 0 { // simpler\n```\n\nThat keeps the signature."} {
		if !strings.Contains(body, part) {
			t.Errorf("scratch lacks %q:\n%s", part, body)
		}
	}
	// u reverts the whole diff in one step.
	m = drainKey(m, tea.KeyPressMsg{Code: 'u', Text: "u"})
	if got := m.activeEditor().Text(); strings.Contains(got, "simpler") {
		t.Fatalf("u must revert the applied diff:\n%s", got)
	}
	if len(m.cxAgent.running) != 0 || m.complexityAgentSegment() != "" {
		t.Error("the slot and the segment must be free again")
	}
	if len(m.askForks) != 1 || m.askForks[0] != "fresh-1" {
		t.Errorf("the fresh session must be excluded from discovery: %v", m.askForks)
	}
	// The slot is free: the same function can be asked again.
	if _, run := startCxAgent(t, m, req); cxAgentResult(run) == nil {
		t.Error("a finished function must accept a new run")
	}
}

// findCxMsg runs the commands of a batch until a T turns up.
func findCxMsg[T any](cmd tea.Cmd) *T {
	pending := []tea.Cmd{cmd}
	for len(pending) > 0 {
		c := pending[0]
		pending = pending[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			pending = append(pending, msg...)
		case T:
			return &msg
		}
	}
	return nil
}

// newestAgentScratch is the text of the newest simplify scratch.
func newestAgentScratch(t *testing.T) string {
	t.Helper()
	paths, err := scratch.List()
	if err != nil {
		t.Fatal(err)
	}
	var best string
	var bestAt int64
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil || !strings.HasPrefix(string(data), "# Simplify ") {
			continue
		}
		if st, err := os.Stat(p); err == nil && st.ModTime().UnixNano() >= bestAt {
			best, bestAt = string(data), st.ModTime().UnixNano()
		}
	}
	if best == "" {
		t.Fatal("no simplify scratch was written")
	}
	return best
}

// fakeCxRunner swaps the claude runner for queued answers and counts the
// runs; block holds every run until the context is cancelled.
type fakeCxRunner struct {
	answers []agentask.Result
	runs    int
	block   bool
}

func useFakeCxRunner(t *testing.T, f *fakeCxRunner) {
	t.Helper()
	prev := cxAgentRunner
	cxAgentRunner = func(ctx context.Context, _ string, _ []string) (agentask.Result, error) {
		i := f.runs
		f.runs++
		if f.block {
			<-ctx.Done()
			return agentask.Result{}, ctx.Err()
		}
		if i >= len(f.answers) {
			return agentask.Result{}, errors.New("unexpected run")
		}
		return f.answers[i], nil
	}
	t.Cleanup(func() { cxAgentRunner = prev })
}

// cxAgentDone runs req through the fake runner and feeds the result back.
func cxAgentDone(t *testing.T, m Model, req complexitypanel.AskAgentMsg) (Model, tea.Cmd) {
	t.Helper()
	m, run := startCxAgent(t, m, req)
	done := cxAgentResult(run)
	if done == nil {
		t.Fatal("no run result")
	}
	tm, cmd := m.Update(*done)
	return asModel(tm), cmd
}

func TestComplexityAgentHunkMismatchAppliesNothing(t *testing.T) {
	m, file, req := cxAgentApp(t)
	diff := "```diff\n@@ -3 +3 @@\n-\tif a > 0 {\n+\tif a >= 1 {\n@@ -9 +9 @@\n-\treturn b\n+\treturn 0\n```\nWhy."
	useFakeCxRunner(t, &fakeCxRunner{answers: []agentask.Result{{Answer: diff, ForkID: "s1"}}})
	m, _ = cxAgentDone(t, m, req)
	if !hasToast(m, "does not apply cleanly (hunk 2 of 2) — opened as ") || !hasToast(m, "s1.jsonl") {
		t.Fatalf("toasts = %v", toastTexts(m))
	}
	if ed := m.editorForPath(file); ed != nil && (strings.Contains(ed.Text(), "a >= 1") || ed.Dirty()) {
		t.Fatal("a failed hunk must apply nothing")
	}
	if ed := m.activeEditor(); ed == nil || filepath.Ext(ed.Path()) != ".md" {
		t.Fatal("the scratch must open in front")
	}
	if body := newestAgentScratch(t); !strings.Contains(body, "does not apply cleanly") || !strings.Contains(body, "git apply --3way") {
		t.Errorf("scratch = %s", body)
	}
}

// TestComplexityAgentElidedDiffRetriesThenNotesIt covers #3050: a diff with
// a "..." placeholder gets the one follow-up asking for the complete diff;
// when that one is elided too, the apply misses and the scratch's note
// names the left-out lines, not an edited file.
func TestComplexityAgentElidedDiffRetriesThenNotesIt(t *testing.T) {
	m, file, req := cxAgentApp(t)
	elided := "```diff\n@@ -1,40 +1,40 @@\n \tif a > 0 {\n \t\t... (rest unchanged)\n-\treturn b\n+\treturn 0\n```\nWhy."
	f := &fakeCxRunner{answers: []agentask.Result{{Answer: elided, ForkID: "s1"}, {Answer: elided, ForkID: "s1"}}}
	useFakeCxRunner(t, f)
	m, _ = cxAgentDone(t, m, req)
	if f.runs != 2 {
		t.Fatalf("an elided diff must get exactly one follow-up, ran %d", f.runs)
	}
	if !hasToast(m, "does not apply cleanly (hunk 1 of 1, the diff leaves lines out) — opened as ") {
		t.Fatalf("toasts = %v", toastTexts(m))
	}
	if ed := m.editorForPath(file); ed != nil && ed.Dirty() {
		t.Fatal("nothing may be applied")
	}
	body := newestAgentScratch(t)
	for _, part := range []string{"does not match the buffer", "The diff leaves lines out — hunk 1 stands a placeholder in for lines (\"... (rest unchanged)\")", "also after the follow-up asking for the complete diff", "git apply --3way"} {
		if !strings.Contains(body, part) {
			t.Errorf("scratch lacks %q:\n%s", part, body)
		}
	}
	if strings.Contains(body, "edited since") {
		t.Errorf("a forward miss is not an edited file:\n%s", body)
	}
}

func TestComplexityAgentNoDiffRetriesOnceThenScratch(t *testing.T) {
	m, file, req := cxAgentApp(t)
	f := &fakeCxRunner{answers: []agentask.Result{
		{Answer: `<invoke name="Read"><parameter name="file_path">a.go</parameter></invoke>`, ForkID: "s1"},
		{Answer: "I cannot see the file.", ForkID: "s1"},
	}}
	useFakeCxRunner(t, f)
	m, _ = cxAgentDone(t, m, req)
	if f.runs != 2 {
		t.Fatalf("a diff-less answer must get exactly one follow-up, ran %d", f.runs)
	}
	if !hasToast(m, "the agent answered without a diff — opened as ") {
		t.Fatalf("toasts = %v", toastTexts(m))
	}
	if ed := m.editorForPath(file); ed != nil && ed.Dirty() {
		t.Fatal("nothing may be applied")
	}
	if body := newestAgentScratch(t); !strings.Contains(body, "I cannot see the file.") || !strings.Contains(body, "also after the follow-up") {
		t.Errorf("scratch = %s", body)
	}
}

func TestComplexityAgentRetryFindsDiff(t *testing.T) {
	m, file, req := cxAgentApp(t)
	f := &fakeCxRunner{answers: []agentask.Result{
		{Answer: "Let me read the file first.", ForkID: "s1"},
		{Answer: "```diff\n@@\n-\treturn a\n+\treturn a // done\n```", ForkID: "s1"},
	}}
	useFakeCxRunner(t, f)
	m, _ = cxAgentDone(t, m, req)
	if ed := m.editorForPath(file); f.runs != 2 || ed == nil || !strings.Contains(ed.Text(), "return a // done") {
		t.Fatalf("the follow-up's diff must apply (runs %d)", f.runs)
	}
}

func TestComplexityAgentApplyOffOpensScratchOnly(t *testing.T) {
	m, file, req := cxAgentApp(t)
	prev := config.Get()
	cfg := *prev
	cfg.Complexity.AgentApply = false
	config.Set(&cfg)
	t.Cleanup(func() { config.Set(prev) })
	useFakeCxRunner(t, &fakeCxRunner{answers: []agentask.Result{{Answer: "```diff\n@@\n-\treturn a\n+\treturn 0\n```", ForkID: "s1"}}})
	m, _ = cxAgentDone(t, m, req)
	if !hasToast(m, "the agent's answer for render opened as ") {
		t.Fatalf("toasts = %v", toastTexts(m))
	}
	if ed := m.editorForPath(file); ed != nil && ed.Dirty() {
		t.Fatal("agent_apply off must not touch the buffer")
	}
	if ed := m.activeEditor(); ed == nil || filepath.Ext(ed.Path()) != ".md" {
		t.Fatal("the scratch must open in front")
	}
}

func TestComplexityAgentCancel(t *testing.T) {
	m, _, req := cxAgentApp(t)
	f := &fakeCxRunner{block: true}
	useFakeCxRunner(t, f)
	m, run := startCxAgent(t, m, req)
	// The pane marks the running row and esc on it asks for the cancel.
	if line := m.statusLine(); !strings.Contains(line, "COMPLEXITY") || !strings.Contains(line, "[agent: simplify render 0:0") {
		t.Fatalf("the focused Complexity window's status line must show the run: %q", line)
	}
	p := m.complexityPanel()
	if !strings.Contains(p.View(), "⟳ render") {
		t.Fatalf("the running row must carry the mark:\n%s", p.View())
	}
	tm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = asModel(tm)
	cancel := findCxMsg[complexitypanel.CancelAgentMsg](cmd)
	if cancel == nil || cancel.Key() != req.Key() {
		t.Fatalf("esc on the running row = %+v", cancel)
	}
	tm, _ = m.Update(*cancel)
	m = asModel(tm)
	if !hasToast(m, "cancelled the agent for render") || len(m.cxAgent.running) != 0 || m.complexityAgentSegment() != "" {
		t.Fatalf("cancel: toasts=%v running=%d", toastTexts(m), len(m.cxAgent.running))
	}
	if strings.Contains(p.View(), "⟳ render") {
		t.Fatal("the mark must go with the cancel")
	}
	// The cancelled run ends at once; its result is dropped.
	done := cxAgentResult(run)
	if done == nil || !errors.Is(done.err, context.Canceled) {
		t.Fatalf("cancelled run = %+v", done)
	}
	// A fresh run starts on the same row, and the stale result never frees
	// its slot.
	m, run2 := startCxAgent(t, m, req)
	if len(m.cxAgent.running) != 1 || run2 == nil {
		t.Fatal("a must start a fresh run after the cancel")
	}
	tm, _ = m.Update(*done)
	m = asModel(tm)
	if len(m.cxAgent.running) != 1 {
		t.Fatal("the cancelled run's result must not free the fresh run's slot")
	}
	// complexity.cancelAgent from the palette cancels the selected row's run.
	tm, _ = m.Update(ComplexityCancelAgentMsg{})
	m = asModel(tm)
	if len(m.cxAgent.running) != 0 {
		t.Fatal("complexity.cancelAgent must cancel the run")
	}
	if cxAgentResult(run2) == nil {
		t.Fatal("the second run must end on its cancel")
	}
	tm, _ = m.Update(ComplexityCancelAgentMsg{})
	if m = asModel(tm); !hasToast(m, "the agent is not running") {
		t.Fatalf("toasts = %v", toastTexts(m))
	}
}

func TestComplexityAgentSegment(t *testing.T) {
	m, _ := timeApp(t)
	if m.complexityAgentSegment() != "" {
		t.Fatal("no run, no segment")
	}
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	m.nowFn = func() time.Time { return now }
	m.cxAgent = &complexityAgentState{running: map[string]*cxAgentRun{
		"a": {id: 1, req: complexitypanel.AskAgentMsg{Name: "process_batch"}, started: now.Add(-42 * time.Second)},
	}}
	if got := m.complexityAgentSegment(); got != "[agent: simplify process_batch 0:42]" {
		t.Fatalf("segment = %q", got)
	}
	m.cxAgent.running["b"] = &cxAgentRun{id: 2, req: complexitypanel.AskAgentMsg{Name: "other"}, started: now.Add(-3 * time.Second)}
	m.cxAgent.running["a"].started = now.Add(-(2*time.Minute + 5*time.Second))
	if got := m.complexityAgentSegment(); got != "[agent: simplify process_batch 2:05 +1]" {
		t.Fatalf("segment = %q", got)
	}
	// The status line carries it.
	f := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(f, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPathAt(f, 0, 0)
	m = asModel(tm)
	if line := m.statusLine(); !strings.Contains(line, "agent: simplify process_batch") {
		t.Errorf("status line lacks the segment: %q", line)
	}
}

func TestComplexityAgentTickRearmsWhileRunning(t *testing.T) {
	m, _ := timeApp(t)
	m.cxAgent = &complexityAgentState{running: map[string]*cxAgentRun{"a": {id: 1}}}
	if m.armComplexityAgentTick() == nil || m.armComplexityAgentTick() != nil {
		t.Fatal("the tick arms once")
	}
	if cmd := m.handleComplexityAgentTick(cxAgentTickMsg{gen: m.modelGen}); cmd == nil {
		t.Fatal("a running agent re-arms the tick")
	}
	delete(m.cxAgent.running, "a")
	if cmd := m.handleComplexityAgentTick(cxAgentTickMsg{gen: m.modelGen}); cmd != nil || m.cxAgent.tickArmed {
		t.Fatal("the tick stops with the last run")
	}
	if cmd := m.handleComplexityAgentTick(cxAgentTickMsg{gen: m.modelGen + 1}); cmd != nil {
		t.Fatal("a departed model's tick is dropped")
	}
}

func TestCxAgentHunksRefusesOtherFile(t *testing.T) {
	d, _ := agentask.ParseAnswerDiff("```diff\n--- a/a.go\n+++ b/a.go\n@@\n-x\n+y\n--- a/b.go\n+++ b/b.go\n@@\n-x\n+y\n```")
	if _, why := cxAgentFileHunks([]string{"/p/a.go"}, d); why != "also changes b.go" {
		t.Fatalf("why = %q", why)
	}
	d, _ = agentask.ParseAnswerDiff("```diff\n--- a/internal/a.go\n+++ b/internal/a.go\n@@\n-x\n+y\n```")
	if edits, why := cxAgentFileHunks([]string{"/p/internal/a.go"}, d); why != "" || len(edits) != 1 || len(edits[0].hunks) != 1 {
		t.Fatalf("edits=%v why=%q", edits, why)
	}
	// A section naming no file belongs to the only path.
	d, _ = agentask.ParseAnswerDiff("```diff\n@@\n-x\n+y\n```")
	if edits, why := cxAgentFileHunks([]string{"/p/a.go"}, d); why != "" || len(edits) != 1 || edits[0].path != "/p/a.go" {
		t.Fatalf("edits=%v why=%q", edits, why)
	}
}

// TestCxAgentFileHunksSortsMultiFileDiff covers #3046's routing: sections
// land on the group's files in diff order (repeated sections merged); a
// foreign file, an unnamed section and an ambiguous suffix refuse.
func TestCxAgentFileHunksSortsMultiFileDiff(t *testing.T) {
	paths := []string{"/p/x/a.go", "/p/y/b.go"}
	d, _ := agentask.ParseAnswerDiff("```diff\n--- a/y/b.go\n+++ b/y/b.go\n@@\n-x\n+y\n--- a/x/a.go\n+++ b/x/a.go\n@@\n-x\n+y\n--- a/y/b.go\n+++ b/y/b.go\n@@\n-z\n+w\n```")
	edits, why := cxAgentFileHunks(paths, d)
	if why != "" || len(edits) != 2 || edits[0].path != "/p/y/b.go" || len(edits[0].hunks) != 2 || edits[1].path != "/p/x/a.go" {
		t.Fatalf("edits=%+v why=%q", edits, why)
	}
	d, _ = agentask.ParseAnswerDiff("```diff\n--- a/x/a.go\n+++ b/x/a.go\n@@\n-x\n+y\n--- a/z/c.go\n+++ b/z/c.go\n@@\n-x\n+y\n```")
	if _, why := cxAgentFileHunks(paths, d); why != "also changes z/c.go, which holds no occurrence of the clone group" {
		t.Fatalf("why = %q", why)
	}
	d, _ = agentask.ParseAnswerDiff("```diff\n@@\n-x\n+y\n```")
	if _, why := cxAgentFileHunks(paths, d); why != "has a section naming no file" {
		t.Fatalf("why = %q", why)
	}
	d, _ = agentask.ParseAnswerDiff("```diff\n--- a/a.go\n+++ b/a.go\n@@\n-x\n+y\n```")
	if _, why := cxAgentFileHunks([]string{"/p/x/a.go", "/p/y/a.go"}, d); !strings.Contains(why, "matches several") {
		t.Fatalf("why = %q", why)
	}
}

func TestExcessDelta(t *testing.T) {
	for d, want := range map[int]string{-3: "−3", 2: "+2", 0: "±0"} {
		if got := excessDelta(d); got != want {
			t.Errorf("excessDelta(%d) = %q, want %q", d, got, want)
		}
	}
}

func TestComplexityAskAgentMissingClaude(t *testing.T) {
	m, _, req := cxAgentApp(t)
	t.Setenv("PATH", t.TempDir())
	m, run := startCxAgent(t, m, req)
	done := cxAgentResult(run)
	if done == nil || !errors.Is(done.err, agentask.ErrNotInstalled) {
		t.Fatalf("result = %+v", done)
	}
	tm, _ := m.Update(*done)
	m = asModel(tm)
	if !hasToast(m, askInstallHint) {
		t.Fatalf("toasts = %v", toastTexts(m))
	}
	if ed := m.activeEditor(); ed != nil && ed.HasFile() {
		t.Error("nothing must open on a failure")
	}
}

func TestComplexityAskAgentFailureNotifiesStderr(t *testing.T) {
	m, _, req := cxAgentApp(t)
	fakeCxClaude(t, "", "quota exceeded for today", 2)
	m, run := startCxAgent(t, m, req)
	done := cxAgentResult(run)
	if done == nil || done.err == nil {
		t.Fatalf("a non-zero exit must fail: %+v", done)
	}
	tm, _ := m.Update(*done)
	m = asModel(tm)
	if !hasToast(m, "quota exceeded for today") {
		t.Fatalf("toasts = %v", toastTexts(m))
	}
}

func TestComplexityAskAgentCommandNeedsRow(t *testing.T) {
	m, _ := timeApp(t)
	m = dismissOnboarding(m)
	tm, cmd := m.Update(ComplexityAskAgentMsg{})
	m = asModel(tm)
	if cxAgentResult(cmd) != nil || !hasToast(m, "select a function row") {
		t.Fatalf("without a panel the command must say so: %v", toastTexts(m))
	}
}

func TestFenceDiff(t *testing.T) {
	cases := map[string]string{
		"```diff\n--- a\n+++ b\n```": "```diff\n--- a\n+++ b\n```",
		"no diff here":               "no diff here",
		"Intro.\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\nWhy: shorter.":      "Intro.\n\n```diff\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n```\n\nWhy: shorter.",
		"diff --git a/x b/x\nindex 1..2\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n a": "```diff\ndiff --git a/x b/x\nindex 1..2\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n a\n```",
	}
	for in, want := range cases {
		if got := fenceDiff(in); got != want {
			t.Errorf("fenceDiff(%q) =\n%q\nwant\n%q", in, got, want)
		}
	}
}
