package agenttrace

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tree_test.go covers BuildTree (#2840) against the fixture sessions: the
// turn → decision → tool → file grouping, the file references the rows
// carry, the status suffixes, and key stability across an incremental
// append.

// outline flattens a tree into "depth:kind:key:label" lines.
func outline(nodes []Node) []string {
	var out []string
	var walk func(ns []Node, depth int)
	walk = func(ns []Node, depth int) {
		for _, n := range ns {
			out = append(out, strings.Repeat(" ", depth)+n.Kind.String()+":"+n.Key+":"+n.Label)
			walk(n.Children, depth+1)
		}
	}
	walk(nodes, 0)
	return out
}

func TestBuildTreeGroupsBasicFixture(t *testing.T) {
	s := parseFixture(t, "basic.jsonl")
	got := outline(BuildTree(s))
	want := []string{
		"turn:t1:#1 Add a greeting to main.go",
		" decision:e1:The user wants a greeting. I should read main.go first.",
		" decision:e2:I'll read main.go, then add the greeting.",
		"  tool:e3:Read",
		"   file:e3/f0:read",
		"  tool:e4:Edit",
		"   file:e4/f0:edit",
		"  tool:e5:Write",
		"   file:e5/f0:create",
		"  tool:e6:Bash",
		"  tool:e7:MultiEdit",
		"   file:e7/f0:edit",
		"   file:e7/f1:edit",
		"  tool:e8:NotebookEdit",
		"   file:e8/f0:delete",
		"turn:t2:#2 /verify main.go",
		" separator:e10:— context compacted —",
		" decision:e11:Done: main.go greets, hello.go added.",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("tree =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestBuildTreeDetailsAndRefs(t *testing.T) {
	s := parseFixture(t, "basic.jsonl")
	tree := BuildTree(s)
	byKey := map[string]*Node{}
	Walk(tree, func(n *Node) { byKey[n.Key] = n })

	if d := byKey["e1"].Detail; d != "thinking" {
		t.Errorf("thinking block detail = %q", d)
	}
	if d := byKey["t1"].Detail; d == "" {
		t.Error("turn carries no time")
	}
	// A single-file tool call opens its file itself.
	edit := byKey["e4"]
	if edit.Ref == nil || edit.Ref.Path != "/Users/dev/src/proj/main.go" || edit.Ref.Line != 3 || edit.Path != edit.Ref.Path {
		t.Errorf("Edit ref = %+v path=%q", edit.Ref, edit.Path)
	}
	if f := byKey["e4/f0"]; f.Ref == nil || *f.Ref != *edit.Ref {
		t.Errorf("file ref = %+v, want %+v", f.Ref, edit.Ref)
	}
	// A multi-file call counts its files and opens nothing itself.
	multi := byKey["e7"]
	if multi.Ref != nil || multi.Detail != "2 files" {
		t.Errorf("MultiEdit = ref %+v detail %q", multi.Ref, multi.Detail)
	}
	if f := byKey["e7/f1"]; f.Ref == nil || f.Ref.Line != 5 {
		t.Errorf("second MultiEdit ref = %+v", f.Ref)
	}
	// A failed Bash shows its title and the error mark.
	bash := byKey["e6"]
	if !bash.Error || bash.Detail != "Build and vet the module ✗ error" {
		t.Errorf("Bash = error %v detail %q", bash.Error, bash.Detail)
	}
	if byKey["e8/f0"].Ref.Op != OpDelete {
		t.Errorf("notebook op = %s", byKey["e8/f0"].Ref.Op)
	}
}

// TestBuildTreeDropsAskTurns: the turns agent.ask put to a fork (#2844) —
// question and answer — stay out of the tree; the copied history remains.
func TestBuildTreeDropsAskTurns(t *testing.T) {
	s := &Session{Events: []Event{
		{Kind: KindUser, Turn: 1, Text: "go"},
		{Kind: KindAssistant, Turn: 1, Text: "done"},
		{Kind: KindUser, Turn: 2, Text: AskMarker + "\nContext …\n\nQuestion: why?"},
		{Kind: KindAssistant, Turn: 2, Text: "because"},
		{Kind: KindUser, Turn: 3, Text: AskMarker + "\nand then?"},
		{Kind: KindTool, Turn: 3, Tool: &Tool{Name: "Read", Title: "x"}},
	}}
	got := outline(BuildTree(s))
	want := []string{"turn:t1:#1 go", " decision:e1:done"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("tree =\n%s", strings.Join(got, "\n"))
	}
}

func TestBuildTreeImplicitDecisionAndPending(t *testing.T) {
	s := &Session{Events: []Event{
		{Kind: KindUser, Turn: 1, Text: "go"},
		{Kind: KindTool, Turn: 1, Tool: &Tool{Name: "Bash", Title: "ls"}},
		{Kind: KindSeparator, Turn: 0, Text: "resumed"},
	}}
	got := outline(BuildTree(s))
	want := []string{
		"turn:t1:#1 go",
		" decision:e1/x:tool calls",
		"  tool:e1:Bash",
		"turn:t0:session start",
		" separator:e2:— resumed —",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("tree =\n%s", strings.Join(got, "\n"))
	}
	tool := BuildTree(s)[0].Children[0].Children[0]
	if !tool.Pending || tool.Detail != "ls …" {
		t.Errorf("pending call = %+v", tool)
	}
}

func TestBuildTreeKeysSurviveAppend(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "basic.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(data, []byte("\n"))
	cut := -1
	for i, l := range lines {
		if bytes.Contains(l, []byte("/verify")) {
			cut = i
			break
		}
	}
	if cut < 0 {
		t.Fatal("fixture has no second prompt")
	}
	p := NewParser()
	p.ReadFile = fakeRead
	for _, l := range lines[:cut] {
		p.Line(l)
	}
	first := outline(BuildTree(p.Session()))
	if len(first) == 0 || !strings.HasPrefix(first[0], "turn:t1:") || strings.Contains(strings.Join(first, "\n"), "turn:t2") {
		t.Fatalf("first half = %v", first)
	}
	for _, l := range lines[cut:] {
		p.Line(l)
	}
	second := outline(BuildTree(p.Session()))
	if len(second) <= len(first) {
		t.Fatalf("append added nothing: %d → %d rows", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("row %d changed across the append: %q → %q", i, first[i], second[i])
		}
	}
}

func TestLabel(t *testing.T) {
	if got := Label("\n\n  first line  \nsecond"); got != "first line" {
		t.Errorf("Label = %q", got)
	}
	long := strings.Repeat("x", MaxLabel+10)
	if got := Label(long); len([]rune(got)) != MaxLabel || !strings.HasSuffix(got, "…") {
		t.Errorf("long label = %q (%d runes)", got, len([]rune(got)))
	}
	if Label("") != "" {
		t.Error("empty label")
	}
}
