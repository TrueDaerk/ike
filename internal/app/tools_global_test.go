package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/layout"
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

// slotGlobalSettings extends globalToolSettings with a slot template sharing
// the Z slot between a non-global mate, the global sqlx and a second global
// tool (#1901), so the slot rules survive the per-switch config reload.
const slotGlobalSettings = globalToolSettings + `
[[tools.custom]]
name = "mate"
command = "sleep"
args = ["60"]

[[tools.custom]]
name = "gtwo"
command = "sleep"
args = ["60"]
global = true

[tools.layout]
template = ["XEEH", "XEEH", "TTZZ"]
assign = ["X=explorer", "Z=mate", "Z=sqlx", "Z=gtwo"]
`

// globalToolModel builds a switch-capable model whose user config declares
// the global "sqlx" tool (backed by a sleep process that outlives the test
// assertions). The settings live in a sandboxed $HOME/.ike — not
// IKE_CONFIG_DIR, which would also redirect every project's layout store to
// one shared file — so they survive the per-switch config reload while
// layouts stay per-project like production.
func globalToolModel(t *testing.T) Model {
	return globalToolModelWith(t, globalToolSettings)
}

// globalToolModelWith is globalToolModel over an explicit settings payload.
func globalToolModelWith(t *testing.T, settings string) Model {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ike"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ike", "settings.toml"), []byte(settings), 0o644); err != nil {
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

// TestGlobalToolSlotTabDetachReattach: a global tool hosted as a slot tab
// (#1901) detaches tab-wise on switch — the host keeps its other tabs — and
// re-attaches as a tab in the target workspace once its slot mate is open
// there too. The live session never restarts.
func TestGlobalToolSlotTabDetachReattach(t *testing.T) {
	_, b := twoRoots(t)
	m := globalToolModelWith(t, slotGlobalSettings)

	m = step(m, ToolOpenMsg{Name: "mate"})
	hostKey := m.toolPane("mate").Key()
	m = step(m, ToolOpenMsg{Name: "sqlx"})
	host := m.activeWS().Panes.Get(hostKey)
	if host == nil || host.Kind() != pane.KindEditor || host.TabCount() != 2 {
		t.Fatalf("precondition: sqlx must tab into the mate slot pane, got %#v", host)
	}
	sessTerm := *host.TabTerminal(host.ActiveTab())
	t.Cleanup(func() { sessTerm.Close() })
	sess := sessTerm.SessionKey()

	m = step(m, project.SwitchProjectMsg{Root: b})
	parked, ok := m.ws.PeekGlobalTool("sqlx")
	if !ok || !parked.Running() {
		t.Fatal("the tab-hosted global tool must park live on the manager at switch")
	}
	rootA := m.ws.Background()[0]
	aHost := m.ws.Peek(rootA).Panes.Get(hostKey)
	if aHost == nil || aHost.TabCount() != 1 {
		t.Fatalf("the source host must keep its other tab, got %#v", aHost)
	}
	aMate := aHost.TabTerminal(0)
	if aMate == nil || aMate.Tool() != "mate" {
		t.Fatal("the remaining tab must be the non-global mate")
	}
	aMateTerm := *aMate
	t.Cleanup(func() { aMateTerm.Close() })

	// In B: the mate opens dedicated in its slot, then sqlx joins it as a
	// focused tab carrying the very same session.
	m = step(m, ToolOpenMsg{Name: "mate"})
	bHostKey := m.toolPane("mate").Key()
	m = step(m, ToolOpenMsg{Name: "sqlx"})
	locs := m.toolLocations("sqlx")
	if len(locs) != 1 || locs[0].key != bHostKey || locs[0].tab < 0 {
		t.Fatalf("sqlx locations in B = %+v, want a tab in %q", locs, bHostKey)
	}
	bHost := m.activeWS().Panes.Get(bHostKey)
	bMateTerm := *bHost.TabTerminal(0)
	t.Cleanup(func() { bMateTerm.Close() })
	if got := bHost.TabTerminal(locs[0].tab).SessionKey(); got != sess {
		t.Fatalf("session in B = %q, want the live instance %q", got, sess)
	}
	if !bHost.TabTerminal(locs[0].tab).Running() {
		t.Fatal("the re-attached session must be the live one")
	}
	if _, ok := m.ws.PeekGlobalTool("sqlx"); ok {
		t.Fatal("an attached global tool must leave the manager stash")
	}
}

// TestGlobalOnlyTabHostDetachesWhole: two global tools sharing one slot as
// tabs leave no husk behind on switch — the emptied host's leaf leaves the
// parked tree like a dedicated pane's, and both sessions park live.
func TestGlobalOnlyTabHostDetachesWhole(t *testing.T) {
	_, b := twoRoots(t)
	m := globalToolModelWith(t, slotGlobalSettings)
	leavesBefore := len(layout.Leaves(m.activeWS().Tree))

	m = step(m, ToolOpenMsg{Name: "sqlx"})
	hostKey := m.toolPane("sqlx").Key()
	m = step(m, ToolOpenMsg{Name: "gtwo"})
	host := m.activeWS().Panes.Get(hostKey)
	if host == nil || host.Kind() != pane.KindEditor || host.TabCount() != 2 {
		t.Fatalf("precondition: both global tools must share the slot as tabs, got %#v", host)
	}
	s1, s2 := *host.TabTerminal(0), *host.TabTerminal(1)
	t.Cleanup(func() { s1.Close(); s2.Close() })

	m = step(m, project.SwitchProjectMsg{Root: b})
	for _, name := range []string{"sqlx", "gtwo"} {
		parked, ok := m.ws.PeekGlobalTool(name)
		if !ok || !parked.Running() {
			t.Fatalf("%s must park live on the manager at switch", name)
		}
	}
	aws := m.ws.Peek(m.ws.Background()[0])
	if aws.Panes.Get(hostKey) != nil {
		t.Fatal("the emptied host must close with the detach")
	}
	if n := len(layout.Leaves(aws.Tree)); n != leavesBefore {
		t.Fatalf("parked tree leaves = %d, want %d (no husk pane may remain)", n, leavesBefore)
	}
}

// TestGlobalToolSlotTabRestoreAdoptsParked: revisiting an evicted workspace
// whose layout recorded the global tool as a slot tab (#1901) re-attaches the
// parked live session as that tab instead of spawning a duplicate.
func TestGlobalToolSlotTabRestoreAdoptsParked(t *testing.T) {
	_, b := twoRoots(t)
	m := globalToolModelWith(t, slotGlobalSettings)
	m = step(m, ToolOpenMsg{Name: "mate"})
	hostKey := m.toolPane("mate").Key()
	m = step(m, ToolOpenMsg{Name: "sqlx"})
	host := m.activeWS().Panes.Get(hostKey)
	if host == nil || host.TabCount() != 2 {
		t.Fatalf("precondition: sqlx must tab into the mate slot pane, got %#v", host)
	}
	sessTerm := *host.TabTerminal(host.ActiveTab())
	t.Cleanup(func() { sessTerm.Close() })
	sess := sessTerm.SessionKey()

	m = step(m, project.SwitchProjectMsg{Root: b})
	rootA := m.ws.Background()[0]
	if cmd := m.closeWorkspace(m.ws.Drop(rootA)); cmd != nil {
		cmd() // evict A: the revisit below restores from its layout.json
	}
	m = step(m, project.SwitchProjectMsg{Root: rootA})
	defer closeLeafTerminals(m)
	locs := m.toolLocations("sqlx")
	if len(locs) != 1 || locs[0].tab < 0 {
		t.Fatalf("sqlx must restore as a slot tab, locations %+v", locs)
	}
	inst := m.activeWS().Panes.Get(locs[0].key)
	if got := inst.TabTerminal(locs[0].tab).SessionKey(); got != sess {
		t.Fatalf("restored session = %q, want the parked instance %q", got, sess)
	}
	if _, ok := m.ws.PeekGlobalTool("sqlx"); ok {
		t.Fatal("the adopted session must leave the manager stash")
	}
	if mates := m.toolLocations("mate"); len(mates) != 1 || mates[0].key != locs[0].key {
		t.Fatalf("mate must restore in the same host, locations %+v", mates)
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
