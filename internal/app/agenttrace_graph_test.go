package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
	"ike/internal/tracepanel"
)

// agenttrace_graph_test.go covers the app half of the graph view (#2858):
// the pane opens in the configured view, agent.trace.view and the pane's
// 't' toggle it and a reopened pane remembers the pick, a live append in
// graph mode keeps the selected and expanded box, and enter on a prompt
// box shows the text in the shell.

func TestAgentTraceViewToggleAndRemember(t *testing.T) {
	m, dir := traceApp(t)
	target := filepath.Join(t.TempDir(), "x.go")
	if err := os.WriteFile(filepath.Join(dir, "sess-g.jsonl"), []byte(transcriptLines("sess-g", projectRoot(), target, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	m = openTraceGraph(t, m)
	p := m.agentTracePanel()
	if p.ViewMode() != tracepanel.ViewGraph || len(p.Path()) != 3 {
		t.Fatalf("view=%v path=%d", p.ViewMode(), len(p.Path()))
	}
	if view := ansiSeq.ReplaceAllString(p.View(), ""); !strings.Contains(view, "┌?") || !strings.Contains(view, "x.go") || !strings.Contains(view, "┌✎") {
		t.Fatalf("graph not drawn:\n%s", view)
	}
	// The command toggles the open pane.
	out, _ := m.Update(AgentTraceViewMsg{})
	m = out.(Model)
	if p.ViewMode() != tracepanel.ViewTree || !m.traceViewSet {
		t.Fatal("agent.trace.view must switch to the tree")
	}
	// The pane's own 't' switches back; the next tick records the pick.
	p.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	if p.ViewMode() != tracepanel.ViewGraph {
		t.Fatal("t in the pane must switch to the graph")
	}
	out, _ = m.Update(traceTickMsg{gen: m.traceTickGen})
	m = out.(Model)
	if m.traceView != tracepanel.ViewGraph {
		t.Fatalf("the tick must remember the pane's view, got %v", m.traceView)
	}
	// A closed pane reopens in the remembered view.
	out, _ = m.Update(AgentTraceViewMsg{})
	m = out.(Model)
	m.activeWS().Panes.Close(pane.AgentTraceKey)
	out, _ = m.Update(AgentTraceToggleMsg{})
	m = out.(Model)
	if p = m.agentTracePanel(); p == nil || p.ViewMode() != tracepanel.ViewTree {
		t.Fatal("a reopened pane must come back in the view it was closed in")
	}
}

func TestAgentTraceGraphLiveAppendKeepsSelectionAndExpansion(t *testing.T) {
	m, dir := traceApp(t)
	target := filepath.Join(t.TempDir(), "x.go")
	if err := os.WriteFile(target, []byte("a\nb\nc\nd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sess-g2.jsonl")
	if err := os.WriteFile(path, []byte(transcriptLines("sess-g2", projectRoot(), target, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	m = openTraceGraph(t, m)
	p := m.agentTracePanel()
	// The pane opens on the newest box (the running turn's answer); the
	// user selects the edit and expands it.
	if cur := p.CurrentStop(); cur == nil || cur.Key != "t1/end" || !cur.Pending {
		t.Fatalf("newest box = %+v", cur)
	}
	p.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	p.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if cur := p.CurrentStop(); cur == nil || cur.Key != "e2/f0" || p.Expanded() != "e2/f0" {
		t.Fatalf("selection %+v expanded %q", cur, p.Expanded())
	}
	// Enter opens the file at the hunk's line through openPathAt.
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on the change box yielded nothing")
	}
	out, _ := m.Update(cmd())
	m = out.(Model)
	ed := m.activeEditor()
	if ed == nil || ed.Path() != target {
		t.Fatalf("enter did not open %s", target)
	}
	if line, _ := ed.CursorPos(); line != 2 {
		t.Fatalf("cursor line = %d, want 2", line)
	}

	// The agent appends a second turn; the next read keeps the state.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(transcriptLines("sess-g2", projectRoot(), target, 2)); err != nil {
		t.Fatal(err)
	}
	f.Close()
	out, _ = m.Update(traceTickMsg{gen: m.traceTickGen})
	m = out.(Model)
	m.traceBusy = false
	msg := m.traceReadCmd()().(traceReadMsg)
	if msg.added == 0 || len(msg.stops) != 6 {
		t.Fatalf("read added %d events, %d stops", msg.added, len(msg.stops))
	}
	out, _ = m.Update(msg)
	m = out.(Model)
	p = m.agentTracePanel()
	if cur := p.CurrentStop(); cur == nil || cur.Key != "e2/f0" || p.Expanded() != "e2/f0" {
		t.Fatalf("append lost the state: %+v / %q", cur, p.Expanded())
	}
	if keys := pathKeys(p); keys != "t1,e2/f0,t1/end,t2,e5/f0,t2/end" {
		t.Fatalf("path after append = %s", keys)
	}
	if p.Path()[2].Pending {
		t.Fatal("turn 1's answer must settle once turn 2 started")
	}
	// Enter on the prompt box shows its text in the shell.
	p.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	cmd = p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on the prompt yielded nothing")
	}
	out, _ = m.Update(cmd())
	m = out.(Model)
	if !m.shell.IsOpen() || !strings.Contains(m.shell.Content().Title(), "Prompt #1") || !strings.Contains(ansiSeq.ReplaceAllString(m.shell.Content().Render(60), ""), "Turn 1 prompt") {
		t.Fatalf("shell: open=%v title=%q", m.shell.IsOpen(), m.shell.Content().Title())
	}
}

func pathKeys(p *tracepanel.Model) string {
	var keys []string
	for _, st := range p.Path() {
		keys = append(keys, st.Key)
	}
	return strings.Join(keys, ",")
}
