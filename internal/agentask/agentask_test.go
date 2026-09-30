package agentask

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"ike/internal/agenttrace"
	"ike/internal/diff"
	"ike/internal/theme"
)

// session is a two-turn transcript in memory: a prompt, a thinking block,
// an assistant decision and an Edit call with one file.
func session() *agenttrace.Session {
	t0 := time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)
	return &agenttrace.Session{
		ID: "sess-1", CWD: "/proj",
		Events: []agenttrace.Event{
			{Kind: agenttrace.KindUser, Turn: 1, At: t0, Text: "Add a greeting"},
			{Kind: agenttrace.KindAssistant, Turn: 1, At: t0.Add(time.Second), Text: "I should look first", Reasoning: true},
			{Kind: agenttrace.KindAssistant, Turn: 1, At: t0.Add(2 * time.Second), Text: "Editing main.go to add the greeting."},
			{Kind: agenttrace.KindTool, Turn: 1, At: t0.Add(3 * time.Second), Tool: &agenttrace.Tool{
				Name: "Edit", Title: "/proj/main.go", Done: true,
				Paths: []agenttrace.FileRef{{Path: "/proj/main.go", Line: 3, Op: agenttrace.OpEdit}},
			}},
			{Kind: agenttrace.KindUser, Turn: 2, At: t0.Add(time.Minute), Text: "Now test it"},
		},
	}
}

func find(nodes []agenttrace.Node, key string) *agenttrace.Node {
	var hit *agenttrace.Node
	agenttrace.Walk(nodes, func(n *agenttrace.Node) {
		if n.Key == key {
			hit = n
		}
	})
	return hit
}

func TestNodeContextFileNode(t *testing.T) {
	s := session()
	tree := agenttrace.BuildTree(s)
	c := NodeContext(s, find(tree, "e3/f0"))
	if c.Turn != 1 || c.At.IsZero() || c.Path != "/proj/main.go" || c.Line != 3 || c.Op != "edit" {
		t.Fatalf("context = %+v", c)
	}
	if c.Tool != "Edit /proj/main.go" {
		t.Fatalf("tool = %q", c.Tool)
	}
	// The decision the call followed from — never the thinking block.
	if c.Assistant != "Editing main.go to add the greeting." {
		t.Fatalf("assistant = %q", c.Assistant)
	}
	lines := c.Lines()
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"turn: #1 (", "file: /proj/main.go:3 (edit)", "tool: Edit /proj/main.go", "assistant said: Editing main.go"} {
		if !strings.Contains(joined, want) {
			t.Errorf("lines lack %q:\n%s", want, joined)
		}
	}
}

func TestNodeContextDecisionTurnAndNil(t *testing.T) {
	s := session()
	tree := agenttrace.BuildTree(s)
	c := NodeContext(s, find(tree, "e2"))
	if c.Kind != "decision" || c.Assistant != "Editing main.go to add the greeting." || c.Path != "" || c.Tool != "" {
		t.Fatalf("decision context = %+v", c)
	}
	turn := NodeContext(s, find(tree, "t2"))
	if turn.Turn != 2 || turn.Kind != "" || turn.Assistant != "" || !strings.Contains(turn.Label, "Now test it") {
		t.Fatalf("turn context = %+v", turn)
	}
	if !NodeContext(s, nil).Empty() || !NodeContext(nil, nil).Empty() {
		t.Fatal("nil node must yield an empty context")
	}
	// A long assistant text is clipped.
	long := session()
	long.Events[2].Text = strings.Repeat("x", MaxAssistant+50)
	if c := NodeContext(long, find(agenttrace.BuildTree(long), "e2")); len([]rune(c.Assistant)) != MaxAssistant {
		t.Fatalf("assistant not clipped: %d runes", len([]rune(c.Assistant)))
	}
}

func TestPromptAndCommand(t *testing.T) {
	if got := Prompt(Context{}, "  why?  "); got != agenttrace.AskMarker+"\nwhy?" {
		t.Fatalf("empty context prompt = %q", got)
	}
	c := Context{Turn: 3, Path: "a.go", Line: 7, Op: "edit", Hunk: "-a\n+b\n"}
	p := Prompt(c, "Why this way?")
	// Every prompt is tagged, so discovery and the tree can tell a fork IKE
	// created (#2844).
	if !agenttrace.IsAskPrompt(p) {
		t.Fatalf("prompt not tagged: %q", p)
	}
	for _, want := range []string{"Context from the session trace", "- turn: #3", "- file: a.go:7 (edit)", "```diff\n-a\n+b\n```", "\nQuestion: Why this way?"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
	argv := Command("sess-1", Options{Model: "opus", MaxTurns: 3}, p)
	want := []string{"claude", "-p", "--resume", "sess-1", "--fork-session", "--model", "opus", "--tools", "", "--max-turns", "3", "--output-format", "json", "--append-system-prompt", SystemPrompt, p}
	if strings.Join(argv, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv = %q", argv)
	}
	// Zero options fall back to the defaults.
	argv = Command("s", Options{}, "q")
	if argv[6] != "sonnet" || argv[10] != "1" {
		t.Fatalf("defaults not applied: %q", argv)
	}
	// A follow-up resumes the fork itself: no second fork, no original id.
	argv = FollowUp("fork-1", Options{Model: "opus", MaxTurns: 2}, "and then?")
	want = []string{"claude", "-p", "--resume", "fork-1", "--model", "opus", "--tools", "", "--max-turns", "2", "--output-format", "json", "--append-system-prompt", SystemPrompt, "and then?"}
	if strings.Join(argv, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("follow-up argv = %q", argv)
	}
}

func TestNodeContextToolOutput(t *testing.T) {
	s := session()
	tool := s.Events[3].Tool
	tool.Title = "/proj/main.go\n  with   spaces"
	tool.Output = "ok: 3 lines changed"
	tree := agenttrace.BuildTree(s)
	c := NodeContext(s, find(tree, "e3"))
	if c.Tool != "Edit /proj/main.go with spaces" || c.Output != "ok: 3 lines changed" || c.OutputNote != "" {
		t.Fatalf("context = %+v", c)
	}
	if !strings.Contains(strings.Join(c.Lines(), "\n"), "tool output:\n```\nok: 3 lines changed\n```") {
		t.Fatalf("lines = %q", c.Lines())
	}
	cases := []struct {
		out       string
		truncated bool
		note      string
	}{
		{"PNG\x00\x01", false, "omitted (binary)"},
		{"\xff\xfe", false, "omitted (binary)"},
		{strings.Repeat("y", MaxOutput+1), false, "too large"},
		{"short but cut", true, "too large"},
	}
	for _, tc := range cases {
		tool.Output, tool.Truncated = tc.out, tc.truncated
		c := NodeContext(s, find(tree, "e3"))
		if c.Output != "" || !strings.Contains(c.OutputNote, tc.note) {
			t.Errorf("output %q: context output=%q note=%q", tc.out, c.Output, c.OutputNote)
		}
		if !strings.Contains(strings.Join(c.Lines(), "\n"), "tool output: omitted") {
			t.Errorf("output %q: note missing from lines", tc.out)
		}
	}
	// A huge title is clipped.
	tool.Title = strings.Repeat("z", MaxTool*2)
	if c := NodeContext(s, find(tree, "e3")); len([]rune(c.Tool)) != MaxTool {
		t.Fatalf("tool not clipped: %d runes", len([]rune(c.Tool)))
	}
}

func TestHunkCaps(t *testing.T) {
	if got := Hunk("a\n", "b\n"); got != "-a\n+b" {
		t.Fatalf("hunk = %q", got)
	}
	// Binary or oversized sides give no hunk at all.
	if Hunk("a\x00", "b") != "" || Hunk("a", "\xff") != "" {
		t.Fatal("binary side must drop the hunk")
	}
	big := strings.Repeat("x\n", MaxDiffInput)
	if Hunk(big, "y\n") != "" {
		t.Fatal("oversized side must drop the hunk")
	}
	// A long line is clipped per line.
	long := strings.Repeat("w", MaxHunkLine*3)
	h := Hunk("a\n", long+"\n")
	for _, l := range strings.Split(h, "\n") {
		if n := len([]rune(l)); n > MaxHunkLine {
			t.Fatalf("line of %d runes survived", n)
		}
	}
	// Many mid-sized lines hit the byte cap before the line cap.
	var after strings.Builder
	for i := 0; i < MaxHunkLines; i++ {
		after.WriteString(strings.Repeat("m", MaxHunkLine-10) + "\n")
	}
	h = Hunk("", after.String())
	if len(h) > MaxHunkBytes+len("\n…") || !strings.HasSuffix(h, "\n…") {
		t.Fatalf("byte cap: %d bytes, tail %q", len(h), h[max(0, len(h)-5):])
	}
}

func TestUnifiedHunks(t *testing.T) {
	res := diff.Compute("a\nb\nc\n", "a\nB\nc\nd\n")
	got := UnifiedHunks(res, 0)
	for _, want := range []string{"-b", "+B", "+d"} {
		if !strings.Contains(got, want) {
			t.Errorf("hunk lacks %q:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(UnifiedHunks(res, 1), "…") {
		t.Fatalf("cap must end in an ellipsis: %q", UnifiedHunks(res, 1))
	}
	if UnifiedHunks(diff.Compute("x\n", "x\n"), 0) != "" {
		t.Fatal("no change, no hunk")
	}
}

func TestParseResult(t *testing.T) {
	r, err := ParseResult([]byte(`{"type":"result","subtype":"success","is_error":false,"duration_ms":1500,"num_turns":1,"result":"**Because**.","session_id":"fork-1","total_cost_usd":0.01}`))
	if err != nil || r.Answer != "**Because**." || r.ForkID != "fork-1" || r.Duration != 1500*time.Millisecond || r.Turns != 1 || r.CostUSD != 0.01 {
		t.Fatalf("result = %+v, err = %v", r, err)
	}
	// A stream of objects takes the result line.
	r, err = ParseResult([]byte("{\"type\":\"system\",\"subtype\":\"init\"}\n{\"type\":\"result\",\"subtype\":\"success\",\"result\":\"ok\",\"session_id\":\"f2\"}\n"))
	if err != nil || r.Answer != "ok" || r.ForkID != "f2" {
		t.Fatalf("stream result = %+v, err = %v", r, err)
	}
	// Harness-reported failures surface as errors, keeping the fork id.
	r, err = ParseResult([]byte(`{"type":"result","subtype":"error_max_turns","is_error":true,"session_id":"f3","errors":["max turns"]}`))
	if err == nil || !strings.Contains(err.Error(), "max turns") || r.ForkID != "f3" {
		t.Fatalf("error result = %+v, err = %v", r, err)
	}
	if _, err := ParseResult([]byte("not json")); err == nil {
		t.Fatal("garbage must fail")
	}
	if _, err := ParseResult(nil); err == nil {
		t.Fatal("empty output must fail")
	}
}

// fakeClaude writes a claude script on a fresh PATH that records its argv
// (NUL-separated) and cwd, prints body and exits with code.
func fakeClaude(t *testing.T, body string, code int) (argsFile, cwdFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake")
	}
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	cwdFile = filepath.Join(dir, "cwd")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\0' \"$a\"; done > \"$FAKE_CLAUDE_ARGS\"\npwd > \"$FAKE_CLAUDE_CWD\"\necho \"$" + EnvAsk + "\" > \"$FAKE_CLAUDE_CWD.env\"\n"
	if code != 0 {
		script += "echo 'No conversation found with session ID: sess-1' >&2\n"
	}
	script += "printf '%s' '" + body + "'\nexit " + itoa(code) + "\n"
	if err := os.WriteFile(filepath.Join(dir, Binary), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("FAKE_CLAUDE_ARGS", argsFile)
	t.Setenv("FAKE_CLAUDE_CWD", cwdFile)
	return argsFile, cwdFile
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestRunForksInSessionDir(t *testing.T) {
	argsFile, cwdFile := fakeClaude(t, `{"type":"result","subtype":"success","result":"answer","session_id":"fork-1"}`, 0)
	dir := t.TempDir()
	argv := Command("sess-1", Defaults, "why?")
	res, err := Run(context.Background(), dir, argv)
	if err != nil || res.Answer != "answer" || res.ForkID != "fork-1" {
		t.Fatalf("run = %+v, %v", res, err)
	}
	args, _ := os.ReadFile(argsFile)
	got := strings.Split(strings.TrimSuffix(string(args), "\x00"), "\x00")
	if strings.Join(got, "\x00") != strings.Join(argv[1:], "\x00") {
		t.Fatalf("fake received %q", got)
	}
	// The fork is flagged for IKE's own agent hooks (#2844).
	if env, _ := os.ReadFile(cwdFile + ".env"); strings.TrimSpace(string(env)) != "1" {
		t.Fatalf("%s = %q in the fork's environment", EnvAsk, env)
	}
	cwd, _ := os.ReadFile(cwdFile)
	if got, _ := filepath.EvalSymlinks(strings.TrimSpace(string(cwd))); got != mustEval(dir) {
		t.Fatalf("fake ran in %q, want %q", got, dir)
	}
}

func mustEval(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func TestRunErrors(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Run(context.Background(), ".", Command("s", Defaults, "q")); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("missing binary: %v", err)
	}
	fakeClaude(t, "", 1)
	_, err := Run(context.Background(), ".", Command("sess-1", Defaults, "q"))
	var re *ResumeError
	if !errors.As(err, &re) || re.SessionID != "sess-1" || !strings.Contains(re.Detail, "No conversation found") {
		t.Fatalf("refused resume: %v", err)
	}
	// A cancelled context reports the cancellation, not the exit.
	fakeClaude(t, `{"type":"result","subtype":"success","result":"x"}`, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, ".", Command("s", Defaults, "q")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestRenderMarkdown(t *testing.T) {
	pal := theme.DefaultPalette()
	out := RenderMarkdown("# Why\n\n- because **a**\n- and b\n", 40, pal)
	if !strings.Contains(out, "Why") || !strings.Contains(out, "because") {
		t.Fatalf("render = %q", out)
	}
	if RenderMarkdown("", 40, nil) != "" {
		t.Fatal("empty in, empty out")
	}
}
