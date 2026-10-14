package app

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/agentask"
	"ike/internal/complexity"
	"ike/internal/complexitypanel"
)

// complexity_agent_test.go covers the Complexity window's `a` (#2964): the
// prompt built from the row's source, sites and candidate, the fresh
// `claude -p` argv run in the project root through a fake claude, the
// answer opened as a markdown scratch with the diff fenced, the busy
// refusal, the missing-claude and failed-run notifications.

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

func TestComplexityAskAgentRunsFreshSessionAndOpensScratch(t *testing.T) {
	m, file, req := cxAgentApp(t)
	answer := `Extracted the loop into a helper.\n\n--- a/a.go\n+++ b/a.go\n@@ -3,4 +3,4 @@\n-\tif a > 0 {\n+\tif a > 0 { // simpler\n\nThat keeps the signature.`
	argsFile, cwdFile := fakeCxClaude(t, `{"type":"result","subtype":"success","is_error":false,"result":"`+answer+`","session_id":"fresh-1","num_turns":1}`, "", 0)

	m, run := startCxAgent(t, m, req)
	if run == nil || !hasToast(m, "asking") || len(m.cxAgent.running) != 1 {
		t.Fatalf("the request must start the run and say so: %v", toastTexts(m))
	}
	// A second `a` on the same function while it runs only notifies.
	m, again := startCxAgent(t, m, req)
	if !hasToast(m, "still working on render") || len(m.cxAgent.running) != 1 || cxAgentResult(again) != nil {
		t.Fatalf("a busy function must refuse: toasts=%v", toastTexts(m))
	}

	done := cxAgentResult(run)
	if done == nil || done.err != nil {
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
	want := agentask.FreshCommand(askOptions(), agentask.SimplifyPrompt(complexitySimplify(req, displayPath(file), "func render(a int) int {\n\tif a > 0 {\n\t\tfor i := 0; i < a; i++ {\n\t\t\ta--\n\t\t}\n\t}\n\treturn a\n}")))
	if strings.Join(argv, "\x00") != strings.Join(want[1:], "\x00") {
		t.Fatalf("argv =\n%q\nwant\n%q", argv, want[1:])
	}
	prompt := argv[len(argv)-1]
	for _, part := range []string{"Function: render (CCN 3, cognitive 3)", "Complexity sites", "- line 5 for: +1 ccn, +2 cognitive (of which 1 nesting)", "Best extraction candidate: lines 5–7 (for)", "```go\nfunc render(a int) int {", agentask.SimplifyInstruction} {
		if !strings.Contains(prompt, part) {
			t.Errorf("prompt lacks %q:\n%s", part, prompt)
		}
	}
	cwd, _ := os.ReadFile(cwdFile)
	wantCwd, _ := filepath.EvalSymlinks(projectRoot())
	if gotCwd, _ := filepath.EvalSymlinks(strings.TrimSpace(string(cwd))); gotCwd != wantCwd {
		t.Errorf("ran in %q, want the project root %q", gotCwd, wantCwd)
	}

	tm, _ := m.Update(*done)
	m = asModel(tm)
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() || filepath.Ext(ed.Path()) != ".md" {
		t.Fatal("the answer must open as a .md scratch")
	}
	data, err := os.ReadFile(ed.Path())
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, part := range []string{"# Simplify render\n", "Extracted the loop into a helper.\n\n```diff\n--- a/a.go\n+++ b/a.go\n@@ -3,4 +3,4 @@\n-\tif a > 0 {\n+\tif a > 0 { // simpler\n```\n\nThat keeps the signature."} {
		if !strings.Contains(body, part) {
			t.Errorf("scratch lacks %q:\n%s", part, body)
		}
	}
	if len(m.cxAgent.running) != 0 {
		t.Error("the slot must be free again")
	}
	if len(m.askForks) != 1 || m.askForks[0] != "fresh-1" {
		t.Errorf("the fresh session must be excluded from discovery: %v", m.askForks)
	}
	// The slot is free: the same function can be asked again.
	if _, run := startCxAgent(t, m, req); cxAgentResult(run) == nil {
		t.Error("a finished function must accept a new run")
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
