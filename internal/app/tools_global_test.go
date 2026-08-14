package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/project"
	"ike/internal/registry"
)

// tools_global_test.go — global tool instances (#1890): a [[tools.custom]]
// entry with global = true runs as one process-wide session owned by the
// workspace manager. It detaches from the workspace on every switch, so
// switch, project close and eviction never end it; opening the tool anywhere
// re-attaches the same live session; quitting IKE ends it cleanly.

// globalToolSettings is a user-layer settings file declaring one global tool,
// so the config reload every project switch performs keeps the entry.
const globalToolSettings = `
[[tools.custom]]
name = "sqlx"
command = "sleep"
args = ["60"]
global = true
`

// globalToolModel builds a switch-capable model whose user config declares
// the global "sqlx" tool (backed by a sleep process that outlives the test
// assertions). The settings live in a sandboxed $HOME/.ike — not
// IKE_CONFIG_DIR, which would also redirect every project's layout store to
// one shared file — so they survive the per-switch config reload while
// layouts stay per-project like production.
func globalToolModel(t *testing.T) Model {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ike"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ike", "settings.toml"), []byte(globalToolSettings), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("IKE_CONFIG_DIR", "")
	prev := config.Get()
	cfg, _ := config.Load(config.Discover("."))
	config.Set(cfg)
	t.Cleanup(func() { config.Set(prev) })
	m := NewWith(registry.New(), host.MapConfig{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return tm.(Model)
}

// countToolPanes counts dedicated panes of the named tool in the active
// workspace's registry.
func countToolPanes(m Model, name string) int {
	n := 0
	for _, key := range m.activeWS().Panes.Keys() {
		if p := m.activeWS().Panes.Get(key); p != nil && p.Kind() == pane.KindTerminal && p.Terminal().Tool() == name {
			n++
		}
	}
	return n
}

// TestGlobalToolReattachesAcrossWorkspaces: open in A, switch to B — the
// session detaches to the manager, keeps running, and opening the tool in B
// attaches the very same session instead of spawning a second process.
func TestGlobalToolReattachesAcrossWorkspaces(t *testing.T) {
	_, b := twoRoots(t)
	m := globalToolModel(t)

	out, _ := m.Update(ToolOpenMsg{Name: "sqlx"})
	m = out.(Model)
	inst := m.toolPane("sqlx")
	if inst == nil {
		t.Fatal("tool.sqlx must open a pane in A")
	}
	sessTerm := *inst.Terminal() // copy shares the session; cleanup seam
	t.Cleanup(func() { sessTerm.Close() })
	sess := inst.Terminal().SessionKey()

	// Switch away: the pane leaves the workspace, the session parks live.
	out, _ = m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	if got := countToolPanes(m, "sqlx"); got != 0 {
		t.Fatalf("tool panes in fresh workspace B = %d, want 0", got)
	}
	parked, ok := m.ws.PeekGlobalTool("sqlx")
	if !ok {
		t.Fatal("the global tool must park on the manager at switch")
	}
	if !parked.Running() {
		t.Fatal("the parked global tool session must keep running")
	}
	rootA := m.ws.Background()[0]
	for _, key := range m.ws.Peek(rootA).Panes.Keys() {
		if p := m.ws.Peek(rootA).Panes.Get(key); p != nil && p.Kind() == pane.KindTerminal && p.Terminal().Tool() == "sqlx" {
			t.Fatal("the parked workspace must not hold the global tool pane")
		}
	}

	// Open in B: the same session attaches, no second spawn.
	out, _ = m.Update(ToolOpenMsg{Name: "sqlx"})
	m = out.(Model)
	inst = m.toolPane("sqlx")
	if inst == nil {
		t.Fatal("tool.sqlx must attach a pane in B")
	}
	if got := inst.Terminal().SessionKey(); got != sess {
		t.Fatalf("session in B = %q, want the instance from A %q", got, sess)
	}
	if !inst.Terminal().Running() {
		t.Fatal("the re-attached session must be the live one")
	}
	if _, ok := m.ws.PeekGlobalTool("sqlx"); ok {
		t.Fatal("an attached global tool must leave the manager stash")
	}
	if got := countToolPanes(m, "sqlx"); got != 1 {
		t.Fatalf("tool panes in B = %d, want 1", got)
	}

	// Switch back to A: detaches from B again; A resumes without the pane
	// (it was detached from A's tree on the first switch) and re-attaches on
	// demand.
	out, _ = m.Update(project.SwitchProjectMsg{Root: rootA})
	m = out.(Model)
	if got := countToolPanes(m, "sqlx"); got != 0 {
		t.Fatalf("tool panes in resumed A = %d, want 0 before reopening", got)
	}
	out, _ = m.Update(ToolOpenMsg{Name: "sqlx"})
	m = out.(Model)
	if got := m.toolPane("sqlx").Terminal().SessionKey(); got != sess {
		t.Fatalf("session back in A = %q, want %q", got, sess)
	}
}

// TestGlobalToolSurvivesWorkspaceTeardown: tearing down the workspace the
// tool was opened in (the eviction / close-from-list path) does not end the
// detached session — it already left the workspace at switch time.
func TestGlobalToolSurvivesWorkspaceTeardown(t *testing.T) {
	_, b := twoRoots(t)
	m := globalToolModel(t)
	out, _ := m.Update(ToolOpenMsg{Name: "sqlx"})
	m = out.(Model)
	sessTerm := *m.toolPane("sqlx").Terminal()
	t.Cleanup(func() { sessTerm.Close() })

	out, _ = m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	rootA := m.ws.Background()[0]
	if cmd := m.closeWorkspace(m.ws.Drop(rootA)); cmd != nil {
		cmd() // run the close hooks like the eviction path would
	}
	parked, ok := m.ws.PeekGlobalTool("sqlx")
	if !ok || !parked.Running() {
		t.Fatal("tearing down the origin workspace must not end the global tool session")
	}
}

// TestGlobalToolRestoreAdoptsParkedSession: revisiting an evicted workspace
// whose saved layout still lists the global tool (the layout persisted before
// the detach) re-attaches the parked live session in the saved slot instead
// of spawning a duplicate process.
func TestGlobalToolRestoreAdoptsParkedSession(t *testing.T) {
	_, b := twoRoots(t)
	m := globalToolModel(t)
	out, _ := m.Update(ToolOpenMsg{Name: "sqlx"})
	m = out.(Model)
	sessTerm := *m.toolPane("sqlx").Terminal()
	t.Cleanup(func() { sessTerm.Close() })
	sess := m.toolPane("sqlx").Terminal().SessionKey()

	out, _ = m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	rootA := m.ws.Background()[0]
	if cmd := m.closeWorkspace(m.ws.Drop(rootA)); cmd != nil {
		cmd() // evict A: the revisit below restores from its layout.json
	}
	out, _ = m.Update(project.SwitchProjectMsg{Root: rootA})
	m = out.(Model)
	inst := m.toolPane("sqlx")
	if inst == nil {
		t.Fatal("the restored layout must bring the global tool pane back")
	}
	if got := inst.Terminal().SessionKey(); got != sess {
		t.Fatalf("restored session = %q, want the parked instance %q", got, sess)
	}
	if got := countToolPanes(m, "sqlx"); got != 1 {
		t.Fatalf("tool panes after restore = %d, want 1", got)
	}
	if _, ok := m.ws.PeekGlobalTool("sqlx"); ok {
		t.Fatal("the adopted session must leave the manager stash")
	}
}

// TestCloseProjectKeepsGlobalTool: project.close with a global tool attached
// neither prompts (the tool survives, so it gates no guard) nor ends the
// session; the resumed project re-attaches it on demand.
func TestCloseProjectKeepsGlobalTool(t *testing.T) {
	_, b := twoRoots(t)
	m := globalToolModel(t)
	out, _ := m.Update(project.SwitchProjectMsg{Root: b}) // A parks in background
	m = out.(Model)
	out, _ = m.Update(ToolOpenMsg{Name: "sqlx"})
	m = out.(Model)
	sessTerm := *m.toolPane("sqlx").Terminal()
	t.Cleanup(func() { sessTerm.Close() })
	sess := m.toolPane("sqlx").Terminal().SessionKey()

	out, _ = m.Update(project.CloseProjectMsg{})
	m = out.(Model)
	if m.projectClosePromptOpen() {
		t.Fatal("a running global tool must not gate project.close")
	}
	if len(m.ws.Background()) != 0 {
		t.Fatal("project.close must have closed B and resumed A")
	}
	parked, ok := m.ws.PeekGlobalTool("sqlx")
	if !ok || !parked.Running() {
		t.Fatal("project.close must leave the global tool session running")
	}
	out, _ = m.Update(ToolOpenMsg{Name: "sqlx"})
	m = out.(Model)
	if got := m.toolPane("sqlx").Terminal().SessionKey(); got != sess {
		t.Fatalf("session after close = %q, want %q", got, sess)
	}
}

// TestQuitClosesGlobalToolSessions: quit ends a detached global tool session
// — no process outlives IKE.
func TestQuitClosesGlobalToolSessions(t *testing.T) {
	_, b := twoRoots(t)
	m := globalToolModel(t)
	out, _ := m.Update(ToolOpenMsg{Name: "sqlx"})
	m = out.(Model)
	sessTerm := *m.toolPane("sqlx").Terminal()
	t.Cleanup(func() { sessTerm.Close() })

	out, _ = m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	parked, ok := m.ws.PeekGlobalTool("sqlx")
	if !ok || !parked.Running() {
		t.Fatal("test setup: the detached session must be running")
	}
	if _, cmd := m.quit(); cmd == nil {
		t.Fatal("quit must return the exit command")
	}
	if parked.Running() {
		t.Fatal("quit must end detached global tool sessions")
	}
}

// TestQuitClosesAttachedGlobalTool: quit ends an attached global tool with
// the active workspace's terminals.
func TestQuitClosesAttachedGlobalTool(t *testing.T) {
	twoRoots(t)
	m := globalToolModel(t)
	out, _ := m.Update(ToolOpenMsg{Name: "sqlx"})
	m = out.(Model)
	sessTerm := *m.toolPane("sqlx").Terminal()
	t.Cleanup(func() { sessTerm.Close() })
	if _, cmd := m.quit(); cmd == nil {
		t.Fatal("quit must return the exit command")
	}
	if sessTerm.Running() {
		t.Fatal("quit must end the attached global tool session")
	}
}

// TestGlobalToolSingleInstanceToggle: with the instance attached, re-invoking
// tool.sqlx toggles like any single-instance tool, and New (a stale .new
// binding) never spawns a second instance.
func TestGlobalToolSingleInstanceToggle(t *testing.T) {
	twoRoots(t)
	m := globalToolModel(t)
	out, _ := m.Update(ToolOpenMsg{Name: "sqlx"})
	m = out.(Model)
	sessTerm := *m.toolPane("sqlx").Terminal()
	t.Cleanup(func() { sessTerm.Close() })

	out, _ = m.Update(ToolOpenMsg{Name: "sqlx", New: true})
	m = out.(Model)
	if got := countToolPanes(m, "sqlx"); got != 1 {
		t.Fatalf("tool panes after New on a global tool = %d, want 1", got)
	}
}
