package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/agenttrace"
	"ike/internal/changefeed"
	"ike/internal/pane"
	"ike/internal/tracepanel"
)

// agenttrace_panel_test.go covers the app half of the Agent Trace tool
// window (#2840): the toggle state machine, the lookup → read → tree
// pipeline against a transcript in a temporary Claude projects root, open-
// on-enter through openPathAt, the live append keeping the selection, the
// empty state and the followed-terminal choice.

// traceApp builds a sized model with CLAUDE_CONFIG_DIR pointing at a
// temporary root and returns the projects directory of the test's cwd.
func traceApp(t *testing.T) (Model, string) {
	t.Helper()
	m := sized(t, 120, 40)
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	dir := filepath.Join(cfg, "projects", agenttrace.EncodeCWD(projectRoot()))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return m, dir
}

// transcriptLines is a two-line-per-turn Claude transcript: a prompt, an
// assistant text and an Edit of target whose result places the hunk at line
// 3 (1-based). turn numbers the uuids so appended turns stay unique.
func transcriptLines(id, cwd, target string, turn int) string {
	n := string(rune('0' + turn))
	head := `"isSidechain":false,"cwd":"` + cwd + `","sessionId":"` + id + `","version":"2.1.280",`
	return `{` + head + `"type":"user","message":{"role":"user","content":"Turn ` + n + ` prompt"},"uuid":"u` + n + `","timestamp":"2026-09-30T14:0` + n + `:00.000Z"}` + "\n" +
		`{` + head + `"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Editing turn ` + n + `."}]},"uuid":"a` + n + `","timestamp":"2026-09-30T14:0` + n + `:01.000Z"}` + "\n" +
		`{` + head + `"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_` + n + `","name":"Edit","input":{"file_path":"` + target + `","old_string":"c","new_string":"d"}}]},"uuid":"e` + n + `","timestamp":"2026-09-30T14:0` + n + `:02.000Z"}` + "\n" +
		`{` + head + `"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_` + n + `","type":"tool_result","content":"ok"}]},"uuid":"r` + n + `","timestamp":"2026-09-30T14:0` + n + `:03.000Z","toolUseResult":{"structuredPatch":[{"oldStart":3,"newStart":3,"lines":["-c","+d"]}]}}` + "\n"
}

// openTrace toggles the pane open and runs the lookup and the first read
// synchronously, returning the model with the tree filled (or the empty
// state shown). The pane opens in the graph view by default (#2858); the
// tree tests here pin the tree view, the graph tests open through
// openTraceGraph.
func openTrace(t *testing.T, m Model) Model {
	t.Helper()
	return openTraceView(t, m, tracepanel.ViewTree)
}

// openTraceGraph is openTrace in the default graph view.
func openTraceGraph(t *testing.T, m Model) Model {
	t.Helper()
	return openTraceView(t, m, tracepanel.ViewGraph)
}

func openTraceView(t *testing.T, m Model, view tracepanel.ViewMode) Model {
	t.Helper()
	out, _ := m.Update(AgentTraceToggleMsg{})
	m = out.(Model)
	if m.agentTracePanel() == nil || m.activeWS().Panes.Focused() != pane.AgentTraceKey {
		t.Fatalf("toggle must open + focus the pane (focus=%q)", m.activeWS().Panes.Focused())
	}
	if m.agentTracePanel().ViewMode() != tracepanel.ViewGraph {
		t.Fatal("the pane must open in the graph view by default (agent.trace.view)")
	}
	m.agentTracePanel().SetViewMode(view)
	located := m.traceLocateCmd()()
	out, cmd := m.Update(located)
	m = out.(Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			out, _ = m.Update(msg)
			m = out.(Model)
		}
	}
	traceToTop(m.agentTracePanel())
	return m
}

// traceToTop walks the cursor up onto the first row, the way a user
// browsing the history would — the pane opens on the newest row (#2857),
// and the tests below navigate from the top.
func traceToTop(p *tracepanel.Model) {
	rows := p.Rows()
	if len(rows) == 0 {
		return
	}
	first := strings.TrimSpace(rows[0])
	for range rows {
		if cur := p.Current(); cur == nil || cur.Key == first {
			return
		}
		p.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	}
}

func TestAgentTraceToggleLifecycle(t *testing.T) {
	m, _ := traceApp(t)
	before := m.activeWS().Panes.Focused()

	out, _ := m.Update(AgentTraceToggleMsg{})
	m = out.(Model)
	if !m.toolWindowOpen(pane.KindAgentTrace) || m.activeWS().Panes.Focused() != pane.AgentTraceKey {
		t.Fatalf("first toggle must open + focus the pane (focus=%q)", m.activeWS().Panes.Focused())
	}
	if m.traceGen == 0 || m.traceTickGen == 0 {
		t.Fatal("opening must start a lookup generation and a poll chain")
	}

	out, _ = m.Update(AgentTraceToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != before {
		t.Fatalf("focus = %q, want %q", m.activeWS().Panes.Focused(), before)
	}

	out, cmd := m.Update(AgentTraceToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != pane.AgentTraceKey || cmd == nil {
		t.Fatal("third toggle must re-focus the pane and re-locate")
	}
}

func TestAgentTraceReadsTranscriptAndOpensFile(t *testing.T) {
	m, dir := traceApp(t)
	target := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(target, []byte("a\nb\nc\nd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sess-1.jsonl")
	if err := os.WriteFile(path, []byte(transcriptLines("sess-1", projectRoot(), target, 1)), 0o644); err != nil {
		t.Fatal(err)
	}

	m = openTrace(t, m)
	p := m.agentTracePanel()
	if !p.HasSession() {
		t.Fatalf("pane shows no session:\n%s", p.View())
	}
	if info := p.Info(); info.ID != "sess-1" || info.Transcript != path || info.Turns != 1 || info.FromHook {
		t.Fatalf("info = %+v", info)
	}
	rows := p.Rows()
	if strings.Join(rows, ",") != "t1, e1,  e2,   e2/f0" {
		t.Fatalf("rows = %v", rows)
	}
	if m.traceReader == nil || m.traceReader.Path() != path {
		t.Fatal("reader not tailing the located transcript")
	}

	// Down twice lands on the Edit call; enter opens its file at the hunk.
	for range 2 {
		p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on the Edit row yielded nothing")
	}
	msg, ok := cmd().(tracepanel.OpenLocationMsg)
	if !ok || msg.Path != target || msg.Line != 2 {
		t.Fatalf("enter = %#v", msg)
	}
	out, _ := m.Update(msg)
	m = out.(Model)
	ed := m.activeEditor()
	if ed == nil || ed.Path() != target {
		t.Fatalf("open must land in %s", target)
	}
	if line, _ := ed.CursorPos(); line != 2 {
		t.Fatalf("cursor line = %d, want 2", line)
	}
}

func TestAgentTraceLiveAppendKeepsSelection(t *testing.T) {
	m, dir := traceApp(t)
	target := filepath.Join(t.TempDir(), "x.go")
	path := filepath.Join(dir, "sess-2.jsonl")
	if err := os.WriteFile(path, []byte(transcriptLines("sess-2", projectRoot(), target, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	m = openTrace(t, m)
	p := m.agentTracePanel()
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // e1, the decision
	if cur := p.Current(); cur == nil || cur.Key != "e1" {
		t.Fatalf("cursor on %+v", cur)
	}

	// The agent appends a second turn; the next poll reads it.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(transcriptLines("sess-2", projectRoot(), target, 2)); err != nil {
		t.Fatal(err)
	}
	f.Close()

	out, cmd := m.Update(traceTickMsg{gen: m.traceTickGen})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("the tick must read and re-arm while the pane is open")
	}
	// The tick's batch is opaque; run the read the way the tick does.
	m.traceBusy = false
	msg := m.traceReadCmd()().(traceReadMsg)
	if msg.added == 0 {
		t.Fatal("the append added no events")
	}
	out, _ = m.Update(msg)
	m = out.(Model)
	p = m.agentTracePanel()
	rows := p.Rows()
	if strings.Join(rows, ",") != "t1, e1,  e2,   e2/f0,t2, e4,  e5,   e5/f0" {
		t.Fatalf("rows after append = %v", rows)
	}
	if cur := p.Current(); cur == nil || cur.Key != "e1" {
		t.Fatalf("selection lost across the append: %+v", cur)
	}
	if p.Info().Turns != 2 {
		t.Fatalf("turns = %d", p.Info().Turns)
	}

	// A tick from a retired chain, or after the pane closed, ends it.
	if _, cmd := m.Update(traceTickMsg{gen: m.traceTickGen - 1}); cmd != nil {
		t.Fatal("a stale tick must not re-arm")
	}
	m.activeWS().Panes.Close(pane.AgentTraceKey)
	if _, cmd := m.Update(traceTickMsg{gen: m.traceTickGen}); cmd != nil {
		t.Fatal("a tick after close must not re-arm")
	}
}

func TestAgentTraceEmptyStateAndActions(t *testing.T) {
	m, _ := traceApp(t)
	m = openTrace(t, m)
	p := m.agentTracePanel()
	if p.HasSession() {
		t.Fatal("empty projects root found a session")
	}
	view := p.View()
	if !strings.Contains(view, "No agent session") || !strings.Contains(view, "[Install Claude hooks]") {
		t.Fatalf("empty state:\n%s", view)
	}
	// The dialog's actions route to the hook installer and a fresh lookup.
	if _, cmd := m.Update(tracepanel.InstallHooksMsg{}); cmd == nil {
		t.Fatal("install action yielded no command")
	}
	out, cmd := m.Update(tracepanel.RefreshMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("rescan yielded no command")
	}
	if _, ok := cmd().(traceLocatedMsg); !ok {
		t.Fatal("rescan must run the lookup")
	}
	// Closed pane: nothing to relocate.
	m.activeWS().Panes.Close(pane.AgentTraceKey)
	if _, cmd := m.Update(tracepanel.RefreshMsg{}); cmd != nil {
		t.Fatal("rescan on a closed pane must be a no-op")
	}
}

func TestAgentTraceFollowsTheAgentToolPane(t *testing.T) {
	m, key := openWatcher(t)
	cwd := m.toolPane("watcher").Terminal().Cwd()
	if got := m.traceTargetNow(); got.key != key {
		t.Fatalf("focused tool pane not followed: %+v", got)
	}
	// Focus leaves for the explorer: the trace still follows the last tool
	// pane the keyboard sat in.
	m.setFocus(pane.ExplorerKey)
	if got := m.traceTargetNow(); got.key != key || !agentSameDir(got.cwd, cwd) {
		t.Fatalf("recent tool pane not followed: %+v", got)
	}
	// A hook binding for that terminal wins over discovery in the lookup.
	m = sendAgentEvent(m, agentEvent("SessionStart", cwd))
	if s, ok := m.agentSessions[key]; !ok || s.ID != "sess-1" {
		t.Fatalf("event did not bind to the tool pane: %+v", m.agentSessions)
	}
	m.traceGen++
	located := m.traceLocateCmd()().(traceLocatedMsg)
	if located.err != nil || located.sess.ID != "sess-1" || !located.sess.FromHook {
		t.Fatalf("lookup = %+v, %v", located.sess, located.err)
	}
	// Without any terminal the project root is scanned.
	m.toolPane("watcher").Terminal().Close()
	m.activeWS().Panes.Close(m.toolPane("watcher").Key())
	m.recentToolTerm = ""
	if got := m.traceTargetNow(); got.key != "" || got.cwd != projectRoot() {
		t.Fatalf("no-terminal target = %+v", got)
	}
}

// TestAgentTraceChangeFeedLinkBothWays covers #2838 end to end: a feed
// entry written by the followed terminal inside the Edit call's window links
// to its file node; D on the node opens the feed on that entry, V asks the
// feed's revert, and t in the feed jumps back to the node — directly while
// the pane shows the session, and through the pending jump a read applies.
// An entry the feed could not attribute stays unlinked.
func TestAgentTraceChangeFeedLinkBothWays(t *testing.T) {
	m, dir := traceApp(t)
	target := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(target, []byte("a\nb\nd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(filepath.Dir(target), "other.go")
	if err := os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), []byte(transcriptLines("sess-1", projectRoot(), target, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	m = openTrace(t, m)
	p := m.agentTracePanel()
	if !p.HasSession() {
		t.Fatal("no session")
	}
	wrote := time.Date(2026, 9, 30, 14, 1, 2, 500_000_000, time.UTC)
	m.feed.Add(changefeed.Entry{
		Path: target, Time: wrote, Kind: changefeed.Changed,
		Before: "a\nb\nc\n", Origin: changefeed.FromBuffer, Source: "claude", SourceKey: "term-1",
	})
	m.feed.Add(changefeed.Entry{ // unattributed: several processes were busy
		Path: other, Time: wrote, Kind: changefeed.Changed,
		Before: "x", Origin: changefeed.FromBuffer,
	})
	m.traceFollow = traceTarget{key: "term-1", cwd: projectRoot()}
	m.syncTraceLinks()
	if m.traceLinks.Path(target) != "e2/f0" || p.Links().Node("e2/f0") != target {
		t.Fatalf("links = %+v", m.traceLinks)
	}
	if m.traceLinks.Path(other) != "" {
		t.Fatal("an unattributed change was linked")
	}

	// Trace → feed: D opens the feed's mini-diff on the entry.
	if !p.Select("e2/f0") {
		t.Fatal("select failed")
	}
	msg, ok := p.Update(tea.KeyPressMsg{Code: 'D', Text: "D"})().(tracepanel.ChangeDiffMsg)
	if !ok || msg.Path != target {
		t.Fatalf("D = %#v", msg)
	}
	out, _ := m.Update(msg)
	m = out.(Model)
	if sel, _ := m.changeFeedSel(); !m.changeFeedOpen() || sel.Path != target || len(m.cfDiff.Hunks) == 0 {
		t.Fatalf("feed open=%v on %q, hunks=%d", m.changeFeedOpen(), sel.Path, len(m.cfDiff.Hunks))
	}
	if !strings.Contains(m.changeFeedBody(160), "t: agent trace") {
		t.Fatal("the feed detail does not offer the back-link")
	}

	// Feed → trace: t focuses the pane on the node.
	p.Select("t1")
	out, _ = m.updateChangeFeed(tea.KeyPressMsg{Code: 't', Text: "t"})
	m = out.(Model)
	p = m.agentTracePanel()
	if m.changeFeedOpen() || m.activeWS().Panes.Focused() != pane.AgentTraceKey || p.Current().Key != "e2/f0" {
		t.Fatalf("t: feed open=%v focus=%q node=%+v", m.changeFeedOpen(), m.activeWS().Panes.Focused(), p.Current())
	}

	// A jump that had to open the pane lands with the next read.
	p.Select("t1")
	m.traceJump = "e2/f0"
	m.syncTraceLinks()
	if m.traceJump != "" || p.Current().Key != "e2/f0" {
		t.Fatalf("pending jump: %q, node %+v", m.traceJump, p.Current())
	}

	// V asks the feed's own revert confirmation.
	rev, ok := p.Update(tea.KeyPressMsg{Code: 'V', Text: "V"})().(tracepanel.ChangeRevertMsg)
	if !ok {
		t.Fatalf("V = %#v", rev)
	}
	out, _ = m.Update(rev)
	m = out.(Model)
	if m.cfRevert != target || !m.changeFeedRevertOpen() {
		t.Fatalf("revert prompt for %q", m.cfRevert)
	}
}
