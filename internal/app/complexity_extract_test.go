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

// The multi-file fixture (#3046): the same loop in a.go and b.go.
const (
	cxExtractA = "package p\n\nfunc a(x []int) int {\n\ts := 0\n\tfor _, v := range x {\n\t\ts += v\n\t}\n\treturn s\n}\n"
	cxExtractB = "package p\n\nfunc b(y []int) int {\n\tt := 0\n\tfor _, v := range y {\n\t\tt += v\n\t}\n\treturn t\n}\n"
	// cxExtractHunkA adds the helper to a.go and calls it there.
	cxExtractHunkA = "--- a/a.go\n+++ b/a.go\n@@ -3,7 +3,13 @@\n func a(x []int) int {\n-\ts := 0\n-\tfor _, v := range x {\n-\t\ts += v\n-\t}\n-\treturn s\n+\treturn sum(x)\n }\n" +
		"+\n+func sum(x []int) int {\n+\ts := 0\n+\tfor _, v := range x {\n+\t\ts += v\n+\t}\n+\treturn s\n+}\n"
	// cxExtractHunkB calls the helper in b.go.
	cxExtractHunkB = "--- a/b.go\n+++ b/b.go\n@@ -3,7 +3,3 @@\n func b(y []int) int {\n-\tt := 0\n-\tfor _, v := range y {\n-\t\tt += v\n-\t}\n-\treturn t\n+\treturn sum(y)\n }\n"
)

// cxExtractTwoFiles writes the multi-file fixture and opens the Duplicates
// tab on its group; it returns the model, both files and the request `a`
// sends.
func cxExtractTwoFiles(t *testing.T) (Model, string, string, complexitypanel.AskAgentMsg) {
	t.Helper()
	dir := t.TempDir()
	fa, fb := filepath.Join(dir, "a.go"), filepath.Join(dir, "b.go")
	if err := os.WriteFile(fa, []byte(cxExtractA), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fb, []byte(cxExtractB), 0o644); err != nil {
		t.Fatal(err)
	}
	m := cxExtractApp(t, []complexity.CloneGroup{{Lang: "go", Tokens: 60, Occurrences: []complexity.Occurrence{
		{Path: fa, Line: 3, EndLine: 7}, {Path: fb, Line: 3, EndLine: 7}}}})
	tm, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = asModel(tm)
	req := findCxMsg[complexitypanel.AskAgentMsg](cmd)
	if req == nil || len(req.Dup) != 2 || req.Path != fa || req.Line != 3 || req.EndLine != 7 || len(req.Paths()) != 2 {
		t.Fatalf("a on a multi-file group = %+v", req)
	}
	if req.Name != "2 duplicates of a.go:4–8 across 2 files" {
		t.Fatalf("name = %q", req.Name)
	}
	return m, fa, fb, *req
}

// cxExtractAnswer swaps the runner for one answer and records the prompt.
func cxExtractAnswer(t *testing.T, answer string) *string {
	t.Helper()
	var prompt string
	prev := cxAgentRunner
	cxAgentRunner = func(_ context.Context, _ string, argv []string) (agentask.Result, error) {
		prompt = argv[len(argv)-1]
		return agentask.Result{Answer: answer, ForkID: "x2"}, nil
	}
	t.Cleanup(func() { cxAgentRunner = prev })
	return &prompt
}

func TestComplexityExtractMultiFileDuplicates(t *testing.T) {
	m, fa, fb, req := cxExtractTwoFiles(t)
	prompt := cxExtractAnswer(t, "```diff\n"+cxExtractHunkA+cxExtractHunkB+"```\nBoth now call sum.")
	m, run := startCxAgent(t, m, req)
	done := cxAgentResult(run)
	if done == nil || done.err != nil || !done.res.HasDiff {
		t.Fatalf("run = %+v", done)
	}
	for _, part := range []string{"Files: " + displayPath(fa) + ", " + displayPath(fb) + " (go)\n", "2 occurrences of the same 60 tokens in 2 files",
		"Occurrence 1, " + displayPath(fa) + " lines 4–8:\n```go\n\ts := 0\n", "Occurrence 2, " + displayPath(fb) + " lines 4–8:\n```go\n\tt := 0\n",
		"The whole file " + displayPath(fb) + ", for context:", agentask.ExtractFilesInstruction} {
		if !strings.Contains(*prompt, part) {
			t.Errorf("prompt lacks %q:\n%s", part, *prompt)
		}
	}

	tm, cmd := m.Update(*done)
	m = asModel(tm)
	// Both buffers carry their part, dirty and unsaved; a.go is in front.
	if ed := m.activeEditor(); ed == nil || canonicalPath(ed.Path()) != canonicalPath(fa) {
		t.Fatalf("the request's file must be in front, got %v", ed)
	}
	edA, edB := m.editorForPath(fa), m.editorForPath(fb)
	if edA == nil || edB == nil {
		t.Fatal("both files must be open")
	}
	if got := edA.Text(); !strings.Contains(got, "\treturn sum(x)\n") || !strings.Contains(got, "func sum(x []int) int {") || !edA.Dirty() {
		t.Fatalf("a.go:\n%s", got)
	}
	if got := edB.Text(); !strings.Contains(got, "\treturn sum(y)\n") || strings.Contains(got, "t += v") || !edB.Dirty() {
		t.Fatalf("b.go:\n%s", got)
	}
	for f, want := range map[string]string{fa: cxExtractA, fb: cxExtractB} {
		if data, _ := os.ReadFile(f); string(data) != want {
			t.Fatalf("the apply must not write %s", f)
		}
	}
	applied := findCxMsg[cxAgentAppliedMsg](cmd)
	if applied == nil || len(applied.files) != 2 {
		t.Fatalf("applied = %+v", applied)
	}
	tm, _ = m.Update(*applied)
	m = asModel(tm)
	if !hasToast(m, "extracting the 2 duplicates of a.go:4–8 across 2 files into a shared helper — changed ") || !hasToast(m, "u in each buffer reverts") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
	if body := newestExtractScratch(t); !strings.Contains(body, "Occurrences: "+displayPath(fa)+":4–8, "+displayPath(fb)+":4–8 — 60 tokens each") || !strings.Contains(body, "> Applied to the buffers of ") {
		t.Errorf("scratch = %s", body)
	}
	// One u per buffer reverts its part.
	m = drainKey(m, tea.KeyPressMsg{Code: 'u', Text: "u"})
	if got := m.editorForPath(fa).Text(); got != strings.TrimSuffix(cxExtractA, "\n") {
		t.Fatalf("u must revert a.go:\n%s", got)
	}
	tm, _ = m.openPathAt(fb, 0, 0)
	m = asModel(tm)
	m = drainKey(m, tea.KeyPressMsg{Code: 'u', Text: "u"})
	if got := m.editorForPath(fb).Text(); got != strings.TrimSuffix(cxExtractB, "\n") {
		t.Fatalf("u must revert b.go:\n%s", got)
	}
}

func TestComplexityExtractMultiFilePartialMissAppliesNothing(t *testing.T) {
	m, fa, fb, req := cxExtractTwoFiles(t)
	// a.go's part applies, b.go's misses: neither buffer may change.
	badB := strings.Replace(cxExtractHunkB, "-\tt := 0\n", "-\tt := 1\n", 1)
	cxExtractAnswer(t, "```diff\n"+cxExtractHunkA+badB+"```\nWhy.")
	m, _ = cxAgentDone(t, m, req)
	if !hasToast(m, "does not apply cleanly (hunk 1 of 1 in "+displayPath(fb)+") — opened as ") {
		t.Fatalf("toasts = %v", toastTexts(m))
	}
	for _, f := range []string{fa, fb} {
		if ed := m.editorForPath(f); ed != nil && (strings.Contains(ed.Text(), "sum(") || ed.Dirty()) {
			t.Fatalf("a partial miss must change no buffer, %s:\n%s", f, ed.Text())
		}
	}
	if ed := m.activeEditor(); ed == nil || filepath.Ext(ed.Path()) != ".md" {
		t.Fatal("the scratch must open in front")
	}
	if body := newestExtractScratch(t); !strings.Contains(body, "does not apply cleanly to the buffer of "+displayPath(fb)) {
		t.Errorf("scratch = %s", body)
	}
}

func TestComplexityExtractMultiFileRefusesForeignFile(t *testing.T) {
	m, fa, fb, req := cxExtractTwoFiles(t)
	foreign := "--- a/c.go\n+++ b/c.go\n@@ -1 +1 @@\n-package p\n+package q\n"
	cxExtractAnswer(t, "```diff\n"+cxExtractHunkA+cxExtractHunkB+foreign+"```\nWhy.")
	m, _ = cxAgentDone(t, m, req)
	if !hasToast(m, "the agent's diff also changes c.go, which holds no occurrence of the clone group — opened as ") {
		t.Fatalf("toasts = %v", toastTexts(m))
	}
	for _, f := range []string{fa, fb} {
		if ed := m.editorForPath(f); ed != nil && ed.Dirty() {
			t.Fatalf("a refused diff must change nothing: %s", f)
		}
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
