package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/agentask"
	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/scratch"
)

// complexity_extract_test.go covers the Duplicates tab's `a` (#3045): a
// same-file clone group runs the simplify path with the extract prompt and
// its diff lands in the buffer as one edit; a group spanning several files
// is refused, from the pane and from the palette.

const cxExtractSrc = "package p\n\nfunc a(x []int) int {\n\ts := 0\n\tfor _, v := range x {\n\t\ts += v\n\t}\n\treturn s\n}\n\n" +
	"func b(y []int) int {\n\tt := 0\n\tfor _, v := range y {\n\t\tt += v\n\t}\n\treturn t\n}\n"

// cxExtractApp opens the Complexity window on the Duplicates tab holding
// groups, the first one selected.
func cxExtractApp(t *testing.T, groups []complexity.CloneGroup) Model {
	t.Helper()
	m, _ := timeApp(t)
	m = dismissOnboarding(m)
	out, _ := m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	p := m.complexityPanel()
	if p == nil {
		t.Fatal("no panel")
	}
	p.SetTab(complexitypanel.TabDuplicates)
	out, _ = m.Update(complexityDuplicatesMsg{started: m.cxDup.seen, minTokens: 50, groups: groups})
	return out.(Model)
}

func TestComplexityExtractSameFileDuplicates(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(file, []byte(cxExtractSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	m := cxExtractApp(t, []complexity.CloneGroup{{Lang: "go", Tokens: 60, Occurrences: []complexity.Occurrence{
		{Path: file, Line: 3, EndLine: 7}, {Path: file, Line: 11, EndLine: 15}}}})
	tm, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = asModel(tm)
	req := findCxMsg[complexitypanel.AskAgentMsg](cmd)
	if req == nil || len(req.Dup) != 2 || req.Path != file || req.Line != 3 || req.EndLine != 15 || req.Tokens != 60 {
		t.Fatalf("a on a same-file group = %+v", req)
	}

	diff := "```diff\n--- a/a.go\n+++ b/a.go\n" +
		"@@ -3,7 +3,3 @@\n func a(x []int) int {\n-\ts := 0\n-\tfor _, v := range x {\n-\t\ts += v\n-\t}\n-\treturn s\n+\treturn sum(x)\n }\n" +
		"@@ -11,7 +7,13 @@\n func b(y []int) int {\n-\tt := 0\n-\tfor _, v := range y {\n-\t\tt += v\n-\t}\n-\treturn t\n+\treturn sum(y)\n }\n" +
		"+\n+func sum(x []int) int {\n+\ts := 0\n+\tfor _, v := range x {\n+\t\ts += v\n+\t}\n+\treturn s\n+}\n" +
		"```\nBoth loops now call sum."
	var prompt string
	prev := cxAgentRunner
	cxAgentRunner = func(_ context.Context, _ string, argv []string) (agentask.Result, error) {
		prompt = argv[len(argv)-1]
		return agentask.Result{Answer: diff, ForkID: "x1"}, nil
	}
	t.Cleanup(func() { cxAgentRunner = prev })

	m, run := startCxAgent(t, m, *req)
	if !hasToast(m, "to extract 2 duplicates of lines 4–8…") {
		t.Fatalf("toasts = %v", toastTexts(m))
	}
	if seg := m.complexityAgentSegment(); !strings.HasPrefix(seg, "[agent: extract 2 duplicates of lines 4–8 ") {
		t.Errorf("segment = %q", seg)
	}
	if v := m.complexityPanel().View(); !strings.Contains(v, "a.go:4–8 ⟳") {
		t.Errorf("the running group must carry the mark:\n%s", v)
	}
	done := cxAgentResult(run)
	if done == nil || done.err != nil || !done.res.HasDiff {
		t.Fatalf("run = %+v", done)
	}
	for _, part := range []string{"Duplicate code: 2 occurrences of the same 60 tokens", "Occurrence 1, lines 4–8:\n```go\n\ts := 0\n",
		"Occurrence 2, lines 12–16:\n```go\n\tt := 0\n", "The whole file, for context:", agentask.ExtractInstruction} {
		if !strings.Contains(prompt, part) {
			t.Errorf("prompt lacks %q:\n%s", part, prompt)
		}
	}

	tm, cmd = m.Update(*done)
	m = asModel(tm)
	ed := m.activeEditor()
	if ed == nil || canonicalPath(ed.Path()) != canonicalPath(file) {
		t.Fatalf("the group's file must be in front, got %v", ed)
	}
	if got := ed.Text(); !strings.Contains(got, "\treturn sum(x)\n") || !strings.Contains(got, "\treturn sum(y)\n") || !strings.Contains(got, "func sum(x []int) int {") || !ed.Dirty() {
		t.Fatalf("the diff must be applied, dirty:\n%s", got)
	}
	if data, _ := os.ReadFile(file); string(data) != cxExtractSrc {
		t.Fatal("the apply must not write the file")
	}
	applied := findCxMsg[cxAgentAppliedMsg](cmd)
	if applied == nil || applied.after != nil {
		t.Fatalf("applied = %+v", applied)
	}
	tm, _ = m.Update(*applied)
	m = asModel(tm)
	if !hasToast(m, "applied the agent's diff extracting the 2 duplicates of lines 4–8 of ") || !hasToast(m, "x1.jsonl") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
	if body := newestExtractScratch(t); !strings.Contains(body, "# Extract 2 duplicates of lines 4–8\n") || !strings.Contains(body, ": lines 4–8, 12–16 — 60 tokens each") {
		t.Errorf("scratch = %s", body)
	}
	if len(m.cxAgent.running) != 0 {
		t.Error("the slot must be free again")
	}
}

func TestComplexityExtractRefusesMultiFileGroup(t *testing.T) {
	dir := t.TempDir()
	m := cxExtractApp(t, []complexity.CloneGroup{{Lang: "go", Tokens: 60, Occurrences: []complexity.Occurrence{
		{Path: filepath.Join(dir, "a.go"), Line: 3, EndLine: 7}, {Path: filepath.Join(dir, "b.go"), Line: 11, EndLine: 15}}}})
	tm, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = asModel(tm)
	if req := findCxMsg[complexitypanel.AskAgentMsg](cmd); req != nil {
		t.Fatalf("a multi-file group must not start the agent: %+v", req)
	}
	tm, _ = m.Update(ComplexityAskAgentMsg{})
	m = asModel(tm)
	if !hasToast(m, "this clone group spans 2 files — a extracts duplicates within one file only") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
	if m.cxAgent != nil && len(m.cxAgent.running) != 0 {
		t.Error("nothing may run")
	}
}

// newestExtractScratch is the text of the newest extract scratch.
func newestExtractScratch(t *testing.T) string {
	t.Helper()
	paths, err := scratch.List()
	if err != nil {
		t.Fatal(err)
	}
	var best string
	var bestAt int64
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil || !strings.HasPrefix(string(data), "# Extract ") {
			continue
		}
		if st, err := os.Stat(p); err == nil && st.ModTime().UnixNano() >= bestAt {
			best, bestAt = string(data), st.ModTime().UnixNano()
		}
	}
	if best == "" {
		t.Fatal("no extract scratch was written")
	}
	return best
}
