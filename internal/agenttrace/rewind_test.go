package agenttrace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rewind_test.go covers the rewind handling of #2860 against rewind.jsonl:
// a prompt whose parentUuid names an earlier line than the tail (Claude
// Code's esc esc) abandons the events after that line; the parser records
// the rewind and keeps parentUuid on every event; BuildTree and BuildPath
// follow the live branch and show the abandoned one behind a marker; a
// tool_result of a parallel call (parent = its own tool_use) is no rewind.

func TestParserDetectsRewindAndKeepsParents(t *testing.T) {
	s := parseFixture(t, "rewind.jsonl")
	if len(s.Rewinds) != 1 {
		t.Fatalf("rewinds = %+v, want exactly one (parallel tool results are no rewind)", s.Rewinds)
	}
	rw := s.Rewinds[0]
	if rw.From != 4 || rw.To != 8 {
		t.Fatalf("rewind = %+v, want events [4,8) abandoned", rw)
	}
	for i, ev := range s.Events {
		want := i >= 4 && i < 8
		if ev.Abandoned != want {
			t.Errorf("event %d (%s %q) abandoned=%v, want %v", i, ev.Kind, Label(ev.Text), ev.Abandoned, want)
		}
	}
	if s.Events[0].ParentUUID != "" || s.Events[1].ParentUUID != "u1" || s.Events[8].ParentUUID != "a2" {
		t.Errorf("parents = %q %q %q", s.Events[0].ParentUUID, s.Events[1].ParentUUID, s.Events[8].ParentUUID)
	}
	if s.Turns() != 3 {
		t.Errorf("turns = %d (the abandoned prompt still counts)", s.Turns())
	}
	// The parallel Edit and Read both completed.
	for _, i := range []int{10, 11} {
		if tool := s.Events[i].Tool; tool == nil || !tool.Done {
			t.Errorf("event %d: parallel call not completed: %+v", i, s.Events[i].Tool)
		}
	}
}

func TestBuildTreeShowsAbandonedBranchBehindMarker(t *testing.T) {
	s := parseFixture(t, "rewind.jsonl")
	got := outline(BuildTree(s))
	want := []string{
		"turn:t1:#1 Add a greeting to main.go",
		" decision:e1:I'll edit main.go.",
		"  tool:e2:Edit",
		"   file:e2/f0:edit",
		" decision:e3:Done: main.go greets.",
		"rewind:rw8:↶ rewound",
		" turn:rw8/t2:#2 Now delete everything in util.go",
		"  decision:e5:Clearing util.go.",
		"   tool:e6:Write",
		"    file:e6/f0:edit",
		"  decision:e7:util.go is now empty.",
		"turn:t3:#3 Add a doc comment to util.go instead",
		" decision:e9:Reading and documenting util.go.",
		"  tool:e10:Edit",
		"   file:e10/f0:edit",
		"  tool:e11:Read",
		"   file:e11/f0:read",
		" decision:e12:util.go documented.",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("tree =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	tree := BuildTree(s)
	if tree[1].Detail != "1 turn abandoned" || tree[1].At.IsZero() {
		t.Errorf("rewind node = %+v", tree[1])
	}
}

func TestBuildPathShowsAbandonedBranchBehindMarker(t *testing.T) {
	s := parseFixture(t, "rewind.jsonl")
	path := BuildPath(s)
	got := stops(path)
	want := []string{
		"prompt:t1:#1 Add a greeting to main.go:" + local(15, 0),
		"change:e2/f0:main.go:edit :3 +1 −0",
		"answer:t1/end:Done: main.go greets.:" + local(15, 0),
		"rewind:rw8:↶ rewound:1 turn abandoned",
		"prompt:t3:#3 Add a doc comment to util.go instead:" + local(15, 2),
		"change:e10/f0:util.go:edit :3 +1 −0",
		"answer:t3/end:util.go documented.:" + local(15, 2),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("path =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	rw := path[3]
	if !rw.Selectable() {
		t.Error("the rewind marker must be selectable, so it can be expanded")
	}
	branch := stops(rw.Branch)
	wantBranch := []string{
		"prompt:rw8/t2:#2 Now delete everything in util.go:" + local(15, 1),
		"change:e6/f0:util.go:edit :1 +0 −2",
		"answer:rw8/t2/end:util.go is now empty.:" + local(15, 1),
	}
	if strings.Join(branch, "\n") != strings.Join(wantBranch, "\n") {
		t.Fatalf("branch =\n%s\nwant\n%s", strings.Join(branch, "\n"), strings.Join(wantBranch, "\n"))
	}
	// An abandoned turn never shows as working: its answer settled.
	if rw.Branch[2].Pending {
		t.Error("an abandoned turn's answer must not be pending")
	}
}

// TestRewindArrivesIncrementally: the rewind lands while tailing — the
// events already shown turn abandoned in place and the line reports a
// change, so the host re-reads the tree.
func TestRewindArrivesIncrementally(t *testing.T) {
	before := parseFixturePrefix(t, "rewind.jsonl", `"uuid":"u3"`)
	if len(before.Rewinds) != 0 || before.Events[4].Abandoned {
		t.Fatalf("before the rewind: %+v", before.Rewinds)
	}
	tree := BuildTree(before)
	if len(tree) != 2 || tree[1].Key != "t2" {
		t.Fatalf("live tree before = %v", outline(tree))
	}
	p := NewParser()
	p.ReadFile = fakeRead
	lines := fixtureLines(t, "rewind.jsonl")
	for _, l := range lines[:10] {
		p.Line([]byte(l))
	}
	if !p.Line([]byte(lines[10])) {
		t.Fatal("the rewinding prompt must report a change")
	}
	s := p.Session()
	if len(s.Rewinds) != 1 || !s.Events[4].Abandoned || !s.Events[7].Abandoned {
		t.Fatalf("after the rewind: %+v", s.Rewinds)
	}
	// The abandoned turn keeps its old event keys under the marker, the new
	// turn takes fresh ones.
	keys := map[string]bool{}
	Walk(BuildTree(s), func(n *Node) { keys[n.Key] = true })
	for _, k := range []string{"rw8", "rw8/t2", "e6", "e6/f0", "t3"} {
		if !keys[k] {
			t.Errorf("key %s missing from %v", k, keys)
		}
	}
}

// fixtureLines returns a fixture's lines.
func fixtureLines(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}
