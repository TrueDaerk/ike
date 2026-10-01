package agenttrace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// parseFixturePrefix parses the fixture up to (exclusive) the first line
// containing marker.
func parseFixturePrefix(t *testing.T, name, marker string) *Session {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	p := NewParser()
	p.ReadFile = fakeRead
	for _, l := range strings.Split(string(data), "\n") {
		if strings.Contains(l, marker) {
			break
		}
		p.Line([]byte(l))
	}
	return p.Session()
}

// path_test.go covers BuildPath (#2858) against the fixture session: the
// prompt → changes → answer sequence per turn, the ops and keys the stops
// carry, the implicit answer of a turn that ended on a tool call, the
// separator marker, and key stability across an incremental append.

// local formats a fixture time (UTC, 2026-09-30) the way a stop's detail
// shows it: in the local zone.
func local(h, m int) string {
	return time.Date(2026, 9, 30, h, m, 0, 0, time.UTC).Local().Format("15:04")
}

// stops flattens a path into "kind:key:label:detail" lines.
func stops(path []Stop) []string {
	out := make([]string, 0, len(path))
	for _, s := range path {
		out = append(out, s.Kind.String()+":"+s.Key+":"+s.Label+":"+s.Detail)
	}
	return out
}

func TestBuildPathBasicFixture(t *testing.T) {
	s := parseFixture(t, "basic.jsonl")
	got := stops(BuildPath(s))
	want := []string{
		"prompt:t1:#1 Add a greeting to main.go:" + local(14, 0),
		"change:e4/f0:main.go:edit :3 +1 −0",
		"change:e5/f0:hello.go:create +1 −0",
		"change:e7/f0:main.go:edit :2 ×2 +3 −1",
		"change:e8/f0:notes.ipynb:delete",
		"answer:t1/end:I'll read main.go, then add the greeting.:ended on a tool call",
		"prompt:t2:#2 /verify main.go:" + local(14, 1),
		"separator:e10:context compacted:",
		"answer:t2/end:Done: main.go greets, hello.go added.:" + local(14, 2),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("path =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestBuildPathStopDetails(t *testing.T) {
	s := parseFixture(t, "basic.jsonl")
	path := BuildPath(s)
	byKey := map[string]Stop{}
	for _, st := range path {
		byKey[st.Key] = st
	}
	edit := byKey["e4/f0"]
	if edit.Ref == nil || edit.Ref.Path != "/Users/dev/src/proj/main.go" || edit.Ref.Line != 3 || edit.Ref.Op != OpEdit {
		t.Errorf("edit ref = %+v", edit.Ref)
	}
	if len(edit.Events) != 1 || edit.Events[0] != 4 || edit.At.IsZero() {
		t.Errorf("edit events = %v at %v", edit.Events, edit.At)
	}
	if len(edit.Calls) != 1 || edit.Calls[0].Name != "Edit" || edit.Calls[0].Error || edit.Calls[0].Pending {
		t.Errorf("edit calls = %+v", edit.Calls)
	}
	// The reasoning and text that preceded the first change belong to it;
	// the next change starts afresh.
	if len(edit.Context) != 2 || !strings.HasPrefix(edit.Context[0], "The user wants a greeting") || !strings.HasPrefix(edit.Context[1], "I'll read main.go") {
		t.Errorf("edit context = %q", edit.Context)
	}
	if create := byKey["e5/f0"]; len(create.Context) != 0 || !create.HasDiff || create.Added != 1 || create.Ref.Op != OpCreate {
		t.Errorf("create stop = %+v", create)
	}
	if multi := byKey["e7/f0"]; multi.Count != 2 || !multi.HasDiff || multi.Added != 3 || multi.Removed != 1 {
		// Reconstructed from old/new strings against the content the
		// earlier Edit left (#2859): two import lines, one changed line.
		t.Errorf("multi-edit stop = %+v", multi)
	}
	if del := byKey["e8/f0"]; del.Ref.Op != OpDelete || !del.Selectable() {
		t.Errorf("delete stop = %+v", del)
	}
	if prompt := byKey["t1"]; !strings.Contains(prompt.Text, "Add a greeting") || strings.Contains(prompt.Text, "system-reminder") || prompt.Events[0] != 0 {
		t.Errorf("prompt stop = %+v", prompt)
	}
	if ans := byKey["t1/end"]; !ans.Implicit || ans.Pending || len(ans.Events) != 1 || ans.Events[0] != 2 {
		t.Errorf("implicit answer = %+v", ans)
	}
	if ans := byKey["t2/end"]; ans.Implicit || ans.Pending || ans.Events[0] != 11 || ans.Text != "Done: main.go greets, hello.go added." {
		t.Errorf("explicit answer = %+v", ans)
	}
	if sep := byKey["e10"]; sep.Selectable() || sep.Turn != 2 {
		t.Errorf("separator = %+v", sep)
	}
}

// TestBuildPathRunningTurnAndAppend: while the session runs, a turn that
// ended on a tool call has a pending answer; the keys of everything before
// stay the same once more events land, and the answer key never changes.
func TestBuildPathRunningTurnAndAppend(t *testing.T) {
	part := parseFixturePrefix(t, "basic.jsonl", "/verify")
	path := BuildPath(part)
	last := path[len(path)-1]
	if last.Kind != StopAnswer || last.Key != "t1/end" || !last.Pending || !last.Implicit || last.Detail != "working …" {
		t.Fatalf("running turn's answer = %+v", last)
	}
	whole := BuildPath(parseFixture(t, "basic.jsonl"))
	for i := range path {
		if whole[i].Key != path[i].Key || whole[i].Kind != path[i].Kind {
			t.Fatalf("stop %d changed: %s → %s", i, path[i].Key, whole[i].Key)
		}
	}
	if whole[len(path)-1].Pending {
		t.Fatal("the answer must settle once the next turn starts")
	}
}

func TestBuildPathSkipsAskTurnsAndReadsOnly(t *testing.T) {
	s := &Session{Events: []Event{
		{Kind: KindUser, Turn: 1, Text: "do it", At: time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)},
		{Kind: KindTool, Turn: 1, Tool: &Tool{Name: "Read", Done: true, Paths: []FileRef{{Path: "/p/a.go", Op: OpRead}}}},
		{Kind: KindTool, Turn: 1, Tool: &Tool{Name: "Bash", Done: true, IsError: true}},
		{Kind: KindTool, Turn: 1, Tool: &Tool{Name: "Write", Paths: []FileRef{{Path: "/p/b.go", Op: OpWrite}}}},
		{Kind: KindAssistant, Turn: 1, Text: "wrote   b.go\nmore"},
		{Kind: KindUser, Turn: 2, Text: AskMarker + " why?"},
		{Kind: KindAssistant, Turn: 2, Text: "because"},
	}}
	got := stops(BuildPath(s))
	want := []string{
		"prompt:t1:#1 do it:" + local(1, 0),
		"change:e3/f0:b.go:write …",
		"answer:t1/end:wrote b.go:",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("path =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if BuildPath(nil) != nil {
		t.Fatal("nil session must yield nil")
	}
}

func TestCollapse(t *testing.T) {
	if got := Collapse("\n\n  a   b\t c \nnext"); got != "a b c" {
		t.Fatalf("collapse = %q", got)
	}
}
