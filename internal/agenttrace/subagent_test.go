package agenttrace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// subagent_test.go covers #2861: every change to an existing file shows up —
// Edit with a plain-string (rejected) result, Edit with replace_all,
// MultiEdit, Write on an existing file — and a subagent's transcript under
// <session-id>/subagents/ joins the session below its Agent call.

// editsFS is util.go as it is after the edits.jsonl session.
var editsFS = map[string]string{
	"/Users/dev/src/proj/util.go": "package util\n\n// Total is the running total.\nvar total = 1\n\nfunc b1() {}\n",
}

func editsRead(path string) ([]byte, error) {
	if s, ok := editsFS[path]; ok {
		return []byte(s), nil
	}
	return nil, os.ErrNotExist
}

func TestEditsFixtureYieldsEditRows(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "edits.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	p := NewParser()
	p.ReadFile = editsRead
	if err := p.Feed(f); err != nil {
		t.Fatal(err)
	}
	s := p.Session()
	tree := BuildTree(s)
	got := outline(tree)
	want := []string{
		"turn:t1:#1 Tidy up util.go",
		" decision:e1:Rename the package first.",
		"  tool:e2:Edit",
		"   file:e2/f0:edit",
		"  tool:e3:Edit",
		"   file:e3/f0:edit",
		"  tool:e4:MultiEdit",
		"   file:e4/f0:edit",
		"   file:e4/f1:edit",
		"  tool:e5:Write",
		"   file:e5/f0:edit",
		"  tool:e6:Write",
		"   file:e6/f0:create",
		" decision:e7:util.go is tidy.",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("tree =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	const util = "/Users/dev/src/proj/util.go"
	wantRefs := []FileRef{
		{util, 1, OpEdit}, // rejected: the old text is still in the file
		{util, 3, OpEdit}, // replace_all: the patch's first hunk
		{util, 6, OpEdit}, // MultiEdit: found in the file
		{util, 9, OpEdit}, // MultiEdit: not found, the second hunk's line
		{util, 2, OpEdit}, // Write update: the patch's line
		{"/Users/dev/src/proj/util_test.go", 0, OpCreate},
	}
	if refs := s.Files(); len(refs) != len(wantRefs) {
		t.Fatalf("Files = %+v", refs)
	} else {
		for i := range refs {
			if refs[i] != wantRefs[i] {
				t.Errorf("ref %d = %+v, want %+v", i, refs[i], wantRefs[i])
			}
		}
	}
	rejected := s.Events[2].Tool
	if !rejected.IsError || !rejected.Done || rejected.Result != nil {
		t.Errorf("rejected edit: error=%v done=%v result=%q", rejected.IsError, rejected.Done, rejected.Result)
	}
	for _, i := range []int{3, 4, 5, 6} {
		if r := s.Events[i].Tool.Result; !strings.Contains(string(r), "structuredPatch") {
			t.Errorf("event %d keeps no structured result: %q", i, r)
		}
	}
	if !strings.Contains(string(s.Events[5].Tool.Result), `"originalFile"`) {
		t.Error("Write update result lost originalFile")
	}
}

// copySession copies the subagent fixture into a temp projects directory
// and returns the main transcript's path there.
func copySession(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("testdata", "subagent"))); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "44444444-4444-4444-8444-444444444444.jsonl")
}

var subFS = map[string]string{
	"/Users/dev/src/proj/util.go":    "package util\n\nfunc b1() {}\n",
	"/Users/dev/src/proj/version.go": "package version\n\nconst Version = \"0.1.7\"\n",
}

func subRead(path string) ([]byte, error) {
	if s, ok := subFS[path]; ok {
		return []byte(s), nil
	}
	return nil, os.ErrNotExist
}

func TestReaderAttachesSubagentUnderAgentCall(t *testing.T) {
	r := NewReader(copySession(t))
	r.ReadFile = subRead
	if _, err := r.Update(); err != nil {
		t.Fatal(err)
	}
	s := r.Session()
	tree := BuildTree(s)
	got := outline(tree)
	want := []string{
		"turn:t1:#1 Rename the helpers, then bump the version",
		" decision:e1:A builder renames the helpers.",
		"  tool:e2:Agent",
		"   tool:e2/a2:Read",
		"    file:e2/a2/f0:read",
		"   tool:e2/a3:Edit",
		"    file:e2/a3/f0:edit",
		"  tool:e3:Edit",
		"   file:e3/f0:edit",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("tree =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	agent := tree[0].Children[0].Children[0]
	if agent.Detail != "general-purpose · Rename helpers" {
		t.Errorf("agent detail = %q", agent.Detail)
	}
	sub := s.Events[2].Tool.Subagent
	if sub == nil || sub.ID != "a1b2c3" || sub.Type != "general-purpose" || sub.Description != "Rename helpers" || sub.ToolUseID != "toolu_agent" {
		t.Fatalf("subagent = %+v", sub)
	}
	if s.Events[2].Tool.AgentID != "a1b2c3" {
		t.Errorf("AgentID = %q", s.Events[2].Tool.AgentID)
	}
	edit := agent.Children[1].Children[0]
	if edit.Turn != 1 || edit.Event != 2 || len(edit.Agent) != 1 || edit.Agent[0] != 3 {
		t.Errorf("sub file node placed at turn=%d event=%d agent=%v", edit.Turn, edit.Event, edit.Agent)
	}
	if edit.Ref == nil || edit.Ref.Line != 3 || edit.Ref.Op != OpEdit || edit.At.IsZero() || edit.Until.IsZero() {
		t.Errorf("sub file node = %+v ref=%+v", edit, edit.Ref)
	}
	evs, idx := s.Timeline(&edit)
	if evs == nil || evs[idx].Tool == nil || evs[idx].Tool.ID != "toolu_s_edit" {
		t.Fatalf("Timeline(sub edit) = %v, %d", evs, idx)
	}
	// Timeline order: the subagent's read and edit ran before the main
	// session's version bump.
	files := s.Files()
	var paths []string
	for _, f := range files {
		paths = append(paths, filepath.Base(f.Path)+":"+f.Op.String())
	}
	if strings.Join(paths, " ") != "util.go:read util.go:edit version.go:edit" {
		t.Errorf("Files = %v", paths)
	}
	// The main transcript's own sidechain line (old format) stays dropped.
	for _, f := range files {
		if strings.HasSuffix(f.Path, "old.go") {
			t.Error("main-file sidechain edit leaked into the session")
		}
	}
}

func TestLoadReadsSubagents(t *testing.T) {
	s, err := Load(copySession(t))
	if err != nil {
		t.Fatal(err)
	}
	if sub := s.Events[2].Tool.Subagent; sub == nil || len(sub.Session.Events) == 0 {
		t.Fatalf("Load left the subagent out: %+v", sub)
	}
}

// TestReaderPicksUpSubagentWhileTailing: a subagent transcript that appears
// mid-turn is found by the next Update; without its meta file it joins by
// the agent id once the Agent call's result names it, and it keeps growing.
func TestReaderPicksUpSubagentWhileTailing(t *testing.T) {
	src := copySession(t)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	mainLines := strings.SplitAfter(string(data), "\n")
	subDir := filepath.Join(strings.TrimSuffix(src, ".jsonl"), "subagents")
	subData, err := os.ReadFile(filepath.Join(subDir, "agent-a1b2c3.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	subLines := strings.SplitAfter(string(subData), "\n")

	dir := t.TempDir()
	path := filepath.Join(dir, "44444444-4444-4444-8444-444444444444.jsonl")
	r := NewReader(path)
	r.ReadFile = subRead
	appendFile(t, path, mainLines[0]+mainLines[1])
	if _, err := r.Update(); err != nil {
		t.Fatal(err)
	}
	agent := r.Session().Events[2].Tool
	if agent.Name != "Agent" || agent.Subagent != nil {
		t.Fatalf("agent call = %+v", agent)
	}

	// The subagent starts: its transcript appears, no meta file yet.
	newSub := filepath.Join(dir, "44444444-4444-4444-8444-444444444444", "subagents")
	if err := os.MkdirAll(newSub, 0o755); err != nil {
		t.Fatal(err)
	}
	subPath := filepath.Join(newSub, "agent-a1b2c3.jsonl")
	appendFile(t, subPath, subLines[0]+subLines[1])
	rev := r.Revision()
	if _, err := r.Update(); err != nil {
		t.Fatal(err)
	}
	if agent.Subagent != nil {
		t.Fatal("attached before anything named the spawning call")
	}
	if r.Revision() == rev {
		t.Error("revision unchanged although the subagent transcript was read")
	}

	// The meta file names the call: attached on the next read.
	if err := os.WriteFile(filepath.Join(newSub, "agent-a1b2c3.meta.json"), []byte(`{"agentType":"Explore","description":"Rename helpers","toolUseId":"toolu_agent"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, err := r.Update(); err != nil || n == 0 {
		t.Fatalf("attach update: n=%d err=%v", n, err)
	}
	if agent.Subagent == nil || agent.Subagent.Type != "Explore" {
		t.Fatalf("subagent = %+v", agent.Subagent)
	}
	if got := len(BuildTree(r.Session())[0].Children[0].Children[0].Children); got != 1 {
		t.Fatalf("agent row children = %d, want the read", got)
	}

	// The subagent edits: the row appears on the next read.
	appendFile(t, subPath, subLines[2]+subLines[3]+subLines[4])
	if n, err := r.Update(); err != nil || n == 0 {
		t.Fatalf("growth update: n=%d err=%v", n, err)
	}
	var edits int
	Walk(BuildTree(r.Session()), func(n *Node) {
		if n.Kind == NodeFile && n.Ref.Op == OpEdit && strings.HasSuffix(n.Path, "util.go") {
			edits++
		}
	})
	if edits != 1 {
		t.Errorf("subagent edit rows = %d, want 1", edits)
	}
}

func TestReaderJoinsSubagentByAgentIDAndNests(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	subDir := filepath.Join(dir, "s1", "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentCall := func(side bool, id string) string {
		sc := "false"
		if side {
			sc = "true"
		}
		return `{"type":"assistant","isSidechain":` + sc + `,"uuid":"` + id + `","timestamp":"2026-09-30T14:00:02.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"` + id + `","name":"Agent","input":{"description":"` + id + `"}}]}}` + "\n"
	}
	agentResult := func(id, agentID string) string {
		return `{"type":"user","uuid":"r` + id + `","timestamp":"2026-09-30T14:00:09.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"` + id + `","content":"done"}]},"toolUseResult":{"status":"completed","agentId":"` + agentID + `"}}` + "\n"
	}
	edit := `{"type":"assistant","isSidechain":true,"uuid":"e1","timestamp":"2026-09-30T14:00:04.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_e","name":"Edit","input":{"file_path":"/w/x.go","old_string":"a","new_string":"b"}}]}}` + "\n"
	appendFile(t, path, promptLine+agentCall(false, "toolu_outer")+agentResult("toolu_outer", "outer"))
	// The outer subagent has no meta file: it joins by the reported id.
	appendFile(t, filepath.Join(subDir, "agent-outer.jsonl"), agentCall(true, "toolu_inner"))
	// The inner one was spawned by the outer subagent's call.
	appendFile(t, filepath.Join(subDir, "agent-inner.jsonl"), edit)
	if err := os.WriteFile(filepath.Join(subDir, "agent-inner.meta.json"), []byte(`{"agentType":"builder","toolUseId":"toolu_inner"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewReader(path)
	if _, err := r.Update(); err != nil {
		t.Fatal(err)
	}
	got := outline(BuildTree(r.Session()))
	want := []string{
		"turn:t1:#1 first",
		" decision:e1/x:tool calls",
		"  tool:e1:Agent",
		"   tool:e1/a0:Agent",
		"    tool:e1/a0/a0:Edit",
		"     file:e1/a0/a0/f0:edit",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("tree =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if refs := r.Session().Files(); len(refs) != 1 || refs[0].Path != "/w/x.go" {
		t.Errorf("Files = %+v", refs)
	}
}

// TestReaderReattachesAfterMainRestart: a replaced main transcript starts a
// fresh Session; the subagents hang under its calls again.
func TestReaderReattachesAfterMainRestart(t *testing.T) {
	path := copySession(t)
	r := NewReader(path)
	r.ReadFile = subRead
	if _, err := r.Update(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Update(); err != nil {
		t.Fatal(err)
	}
	if sub := r.Session().Events[2].Tool.Subagent; sub == nil || len(sub.Session.Events) == 0 {
		t.Fatalf("subagent not re-attached after restart: %+v", sub)
	}
}
