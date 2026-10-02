package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/changefeed"
	"ike/internal/tracepanel"
)

// agenttrace_subagent_test.go covers #2861 in the app: a subagent's
// transcript under <session-id>/subagents/ that appears while the pane
// tails the session shows its edit below the Agent row on the next read,
// and that file row links to the change feed (D, V) and carries its own
// context into agent.ask (a).

// subagentMain is a turn whose only call is an Agent spawning a builder.
func subagentMain(id, cwd string) string {
	head := `"isSidechain":false,"cwd":"` + cwd + `","sessionId":"` + id + `","version":"2.1.280",`
	return `{` + head + `"type":"user","message":{"role":"user","content":"Fix main.go"},"uuid":"u1","timestamp":"2026-09-30T14:01:00.000Z"}` + "\n" +
		`{` + head + `"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"A builder fixes it."},{"type":"tool_use","id":"toolu_ag","name":"Agent","input":{"description":"Fix main.go","subagent_type":"builder","prompt":"Fix c."}}]},"uuid":"a1","timestamp":"2026-09-30T14:01:01.000Z"}` + "\n"
}

// subagentLines is the builder's sidechain: a decision and an Edit of
// target whose patch puts the hunk at line 3.
func subagentLines(id, cwd, target string) string {
	head := `"isSidechain":true,"agentId":"x1","cwd":"` + cwd + `","sessionId":"` + id + `","version":"2.1.280",`
	return `{` + head + `"type":"user","message":{"role":"user","content":"Fix c."},"uuid":"s0","timestamp":"2026-09-30T14:01:01.500Z"}` + "\n" +
		`{` + head + `"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Fixing c in main.go."},{"type":"tool_use","id":"toolu_s1","name":"Edit","input":{"file_path":"` + target + `","old_string":"c","new_string":"d"}}]},"uuid":"s1","timestamp":"2026-09-30T14:01:02.000Z"}` + "\n" +
		`{` + head + `"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_s1","type":"tool_result","content":"ok"}]},"uuid":"s2","timestamp":"2026-09-30T14:01:03.000Z","toolUseResult":{"filePath":"` + target + `","structuredPatch":[{"oldStart":3,"newStart":3,"lines":["-c","+d"]}]}}` + "\n"
}

func TestAgentTraceSubagentEditsLinkAndAsk(t *testing.T) {
	askSpinInterval = time.Millisecond
	m, dir := traceApp(t)
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	target := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(target, []byte("a\nb\nd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), []byte(subagentMain("sess-1", projectRoot())), 0o644); err != nil {
		t.Fatal(err)
	}
	m = openTrace(t, m)
	p := m.agentTracePanel()
	if !p.HasSession() {
		t.Fatalf("pane shows no session:\n%s", p.View())
	}
	if p.Select("e2/a2/f0") {
		t.Fatal("subagent row before its transcript exists")
	}

	// The builder starts while the pane tails the session.
	subDir := filepath.Join(dir, "sess-1", "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "agent-x1.meta.json"), []byte(`{"agentType":"builder","description":"Fix main.go","toolUseId":"toolu_ag","spawnDepth":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "agent-x1.jsonl"), []byte(subagentLines("sess-1", projectRoot(), target)), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(m.traceReadCmd()())
	m = out.(Model)
	p = m.agentTracePanel()
	if !p.Select("e2/a2/f0") {
		t.Fatalf("the subagent's edit row is missing:\n%s", strings.Join(p.Rows(), "\n"))
	}
	if cur := p.Current(); cur.Ref == nil || cur.Ref.Path != target || cur.Ref.Line != 3 || cur.Label != "edit" {
		t.Fatalf("subagent file row = %+v", cur)
	}

	// The feed entry the followed terminal wrote inside the subagent's call
	// links to its row.
	m.feed.Add(changefeed.Entry{
		Path: target, Time: time.Date(2026, 9, 30, 14, 1, 2, 500_000_000, time.UTC), Kind: changefeed.Changed,
		Before: "a\nb\nc\n", Origin: changefeed.FromBuffer, Source: "claude", SourceKey: "term-1",
	})
	m.traceFollow = traceTarget{key: "term-1", cwd: projectRoot()}
	m.syncTraceLinks()
	if m.traceLinks.Path(target) != "e2/a2/f0" || p.Links().Node("e2/a2/f0") != target {
		t.Fatalf("links = %+v", m.traceLinks)
	}
	diffMsg, ok := p.Update(tea.KeyPressMsg{Code: 'D', Text: "D"})().(tracepanel.DiffMsg)
	if !ok || diffMsg.Linked != target || diffMsg.Key != "e2/a2/f0" {
		t.Fatalf("D = %#v", diffMsg)
	}
	revMsg, ok := p.Update(tea.KeyPressMsg{Code: 'V', Text: "V"})().(tracepanel.ChangeRevertMsg)
	if !ok || revMsg.Linked != target || revMsg.Key != "e2/a2/f0" {
		t.Fatalf("V = %#v", revMsg)
	}
	m = runTraceDiff(t, m, diffMsg)
	if !m.traceDiffOpen() {
		t.Fatal("D must open the diff view")
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = out.(Model)
	if sel, _ := m.changeFeedSel(); !m.changeFeedOpen() || sel.Path != target {
		t.Fatalf("feed open=%v on %q", m.changeFeedOpen(), sel.Path)
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)

	// a: the question carries the subagent call's own context.
	if cur := m.agentTracePanel().Current(); cur == nil || cur.Key != "e2/a2/f0" {
		t.Fatalf("selection moved: %+v", cur)
	}
	askMsg, ok := m.agentTracePanel().Update(tea.KeyPressMsg{Code: 'a', Text: "a"})().(tracepanel.AskMsg)
	if !ok {
		t.Fatalf("a = %#v", askMsg)
	}
	argsFile, _ := fakeClaude(t, `{"type":"result","subtype":"success","is_error":false,"result":"Because.","session_id":"fork-1"}`, 0)
	out, _ = m.Update(askMsg)
	m = out.(Model)
	if !m.agentAskOpen() {
		t.Fatal("a must open the ask prompt")
	}
	for _, r := range "why?" {
		out, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = out.(Model)
	}
	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	runAsk(t, m, cmd)
	prompt := readArgv(t, argsFile)
	last := prompt[len(prompt)-1]
	for _, s := range []string{"file: " + target + ":3 (edit)", "tool: Edit " + target, "assistant said: Fixing c in main.go."} {
		if !strings.Contains(last, s) {
			t.Errorf("prompt lacks %q:\n%s", s, last)
		}
	}
}
