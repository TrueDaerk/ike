package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/project"
	"ike/internal/registry"
	"ike/internal/terminal"
)

// tools_global_test.go — global tool instances (#1890): a [[tools.custom]]
// entry with global = true runs as one process-wide session owned by the
// workspace manager. It detaches from the workspace on every switch and the
// switch-in re-attaches it automatically (#1903) — the pane follows the user
// across projects; an explicit close ends it everywhere, and a session that
// exits while parked comes back as the #810 exited overlay.

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

// quickGlobalSettings declares a global tool whose process ends on its own
// shortly after the spawn — the exited-while-parked case (#1903).
const quickGlobalSettings = `
[[tools.custom]]
name = "quick"
command = "sleep"
args = ["0.3"]
global = true
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

// toolTerminal resolves the named tool's live terminal in the active
// workspace, dedicated pane or hosted tab alike; nil when absent.
func toolTerminal(m Model, name string) *terminal.Model {
	locs := m.toolLocations(name)
	if len(locs) == 0 {
		return nil
	}
	inst := m.activeWS().Panes.Get(locs[0].key)
	if inst == nil {
		return nil
	}
	if locs[0].tab < 0 {
		return inst.Terminal()
	}
	return inst.TabTerminal(locs[0].tab)
}

// TestGlobalToolFollowsSwitch: open in A, switch to B — the pane arrives in
// B automatically carrying the very same live session (#1903); switching
// back and forth stays stable: one pane per view, no duplicate process, no
// drift.
func TestGlobalToolFollowsSwitch(t *testing.T) {
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
	rootA := ""

	for i, root := range []string{b, "", b, ""} {
		if root == "" {
			root = rootA
		}
		m = step(m, project.SwitchProjectMsg{Root: root})
		if rootA == "" {
			rootA = m.ws.Background()[0]
		}
		if got := countToolPanes(m, "sqlx"); got != 1 {
			t.Fatalf("switch %d: tool panes = %d, want 1 (the pane must follow)", i, got)
		}
		term := toolTerminal(m, "sqlx")
		if got := term.SessionKey(); got != sess {
			t.Fatalf("switch %d: session = %q, want the one instance %q", i, got, sess)
		}
		if !term.Running() {
			t.Fatalf("switch %d: the followed session must still run", i)
		}
		if _, ok := m.ws.PeekGlobalTool("sqlx"); ok {
			t.Fatalf("switch %d: an attached global tool must leave the manager stash", i)
		}
	}
}

// TestGlobalToolSwitchKeepsFocus: the auto-attach on switch-in must not
// steal focus — the switch decides focus, the tool just becomes visible.
func TestGlobalToolSwitchKeepsFocus(t *testing.T) {
	_, b := twoRoots(t)
	m := globalToolModel(t)
	m = step(m, ToolOpenMsg{Name: "sqlx"})
	sessTerm := *m.toolPane("sqlx").Terminal()
	t.Cleanup(func() { sessTerm.Close() })

	m = step(m, project.SwitchProjectMsg{Root: b})
	inst := m.toolPane("sqlx")
	if inst == nil {
		t.Fatal("the pane must follow into B")
	}
	if m.activeWS().Panes.Focused() == inst.Key() {
		t.Fatal("the auto-attached tool must not take focus on switch-in")
	}
}

// TestGlobalToolSurvivesWorkspaceTeardown: tearing down the workspace the
// tool was opened in (the eviction / close-from-list path) does not end the
// session — it followed the switch out of that workspace already.
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
	term := toolTerminal(m, "sqlx")
	if term == nil || !term.Running() {
		t.Fatal("tearing down the origin workspace must not end the global tool session")
	}
}

// TestGlobalToolRestoreAdoptsParkedSession: revisiting an evicted workspace
// whose saved layout still lists the global tool re-attaches the session in
// the saved slot instead of spawning a duplicate — the restore seam and the
// switch-in auto-attach (#1903) never double up.
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
		t.Fatalf("restored session = %q, want the live instance %q", got, sess)
	}
	if got := countToolPanes(m, "sqlx"); got != 1 {
		t.Fatalf("tool panes after restore = %d, want 1 (no auto-attach duplicate)", got)
	}
	if _, ok := m.ws.PeekGlobalTool("sqlx"); ok {
		t.Fatal("the adopted session must leave the manager stash")
	}
}

// TestCloseProjectKeepsGlobalTool: project.close with a global tool attached
// neither prompts (the tool survives, so it gates no guard) nor ends the
// session; the tool follows into the resumed project (#1903).
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
	term := toolTerminal(m, "sqlx")
	if term == nil || !term.Running() {
		t.Fatal("the tool must follow into the resumed project, session intact")
	}
	if got := term.SessionKey(); got != sess {
		t.Fatalf("session after close = %q, want %q", got, sess)
	}
}

// TestQuitClosesGlobalToolSessions: quit ends a detached (parked) global
// tool session — no process outlives IKE.
func TestQuitClosesGlobalToolSessions(t *testing.T) {
	twoRoots(t)
	m := globalToolModel(t)
	out, _ := m.Update(ToolOpenMsg{Name: "sqlx"})
	m = out.(Model)
	sessTerm := *m.toolPane("sqlx").Terminal()
	t.Cleanup(func() { sessTerm.Close() })

	// Park the session directly — the mid-switch state, held for good when
	// the incoming project does not declare the tool.
	m.detachGlobalTools()
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

// TestGlobalToolCloseEverywhere: explicitly closing the tool pane in one
// project ends the session and the tool stays gone in every other project —
// a resumed workspace shows no pane and the process never resurrects, until
// tool.<name> deliberately reopens it (#1903).
func TestGlobalToolCloseEverywhere(t *testing.T) {
	_, b := twoRoots(t)
	m := globalToolModel(t)
	m = step(m, ToolOpenMsg{Name: "sqlx"})
	sessTerm := *m.toolPane("sqlx").Terminal()
	t.Cleanup(func() { sessTerm.Close() })
	sess := sessTerm.SessionKey()

	m = step(m, project.SwitchProjectMsg{Root: b})
	rootA := m.ws.Background()[0]
	inst := m.toolPane("sqlx")
	if inst == nil {
		t.Fatal("the pane must follow into B")
	}
	m.closePane(inst.Key())
	if sessTerm.Running() {
		t.Fatal("closing the pane must end the global session (#1890)")
	}
	if _, ok := m.ws.PeekGlobalTool("sqlx"); ok {
		t.Fatal("an explicitly closed tool must not linger in the stash")
	}

	m = step(m, project.SwitchProjectMsg{Root: rootA})
	if got := countToolPanes(m, "sqlx"); got != 0 {
		t.Fatalf("tool panes in resumed A after the close = %d, want 0", got)
	}
	m = step(m, project.SwitchProjectMsg{Root: b})
	if got := countToolPanes(m, "sqlx"); got != 0 {
		t.Fatalf("tool panes back in B after the close = %d, want 0", got)
	}

	// tool.<name> deliberately reopens: a fresh process, following again.
	m = step(m, ToolOpenMsg{Name: "sqlx"})
	fresh := m.toolPane("sqlx")
	if fresh == nil || !fresh.Terminal().Running() {
		t.Fatal("reopening after a close must spawn a fresh session")
	}
	freshTerm := *fresh.Terminal()
	t.Cleanup(func() { freshTerm.Close() })
	if fresh.Terminal().SessionKey() == sess {
		t.Fatal("the reopened tool must be a new session, not the closed one")
	}
}

// TestGlobalToolCloseBeatsStaleLayout: the departing project's layout.json
// was saved with the tool pane still in it; after an explicit close in
// another project, restoring that stale layout (the evicted-workspace
// revisit) must not resurrect the tool (#1903).
func TestGlobalToolCloseBeatsStaleLayout(t *testing.T) {
	_, b := twoRoots(t)
	m := globalToolModel(t)
	m = step(m, ToolOpenMsg{Name: "sqlx"})
	sessTerm := *m.toolPane("sqlx").Terminal()
	t.Cleanup(func() { sessTerm.Close() })

	m = step(m, project.SwitchProjectMsg{Root: b})
	rootA := m.ws.Background()[0]
	if cmd := m.closeWorkspace(m.ws.Drop(rootA)); cmd != nil {
		cmd() // evict A: its layout.json still lists the sqlx pane
	}
	inst := m.toolPane("sqlx")
	if inst == nil {
		t.Fatal("the pane must follow into B")
	}
	m.closePane(inst.Key())

	m = step(m, project.SwitchProjectMsg{Root: rootA})
	defer closeLeafTerminals(m)
	if got := countToolPanes(m, "sqlx"); got != 0 {
		t.Fatalf("stale layout restore spawned %d sqlx panes, want 0", got)
	}
	if locs := m.toolLocations("sqlx"); len(locs) != 0 {
		t.Fatalf("sqlx locations after stale restore = %+v, want none", locs)
	}
}

// TestGlobalToolSlotTabDetachFollows: a global tool hosted as a slot tab
// (#1901) detaches tab-wise on switch — the host keeps its other tabs — and
// follows into the target workspace, landing dedicated in its then-free
// slot. The live session never restarts.
func TestGlobalToolSlotTabDetachFollows(t *testing.T) {
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

	// In B the slot is free: sqlx follows as a dedicated pane at the slot,
	// same session.
	locs := m.toolLocations("sqlx")
	if len(locs) != 1 || locs[0].tab >= 0 {
		t.Fatalf("sqlx locations in B = %+v, want one dedicated pane", locs)
	}
	term := toolTerminal(m, "sqlx")
	if got := term.SessionKey(); got != sess {
		t.Fatalf("session in B = %q, want the live instance %q", got, sess)
	}
	if !term.Running() {
		t.Fatal("the followed session must be the live one")
	}
	if _, ok := m.ws.PeekGlobalTool("sqlx"); ok {
		t.Fatal("an attached global tool must leave the manager stash")
	}
}

// TestGlobalToolArrivesAsSlotTab: when the target workspace's slot pane is
// occupied at switch-in, the following global tool arrives as a tab there —
// the #1901 tab-in-slot rule applied by the auto-attach (#1903).
func TestGlobalToolArrivesAsSlotTab(t *testing.T) {
	_, b := twoRoots(t)
	m := globalToolModelWith(t, slotGlobalSettings)
	m = step(m, ToolOpenMsg{Name: "sqlx"})
	sessTerm := *m.toolPane("sqlx").Terminal()
	t.Cleanup(func() { sessTerm.Close() })
	sess := sessTerm.SessionKey()

	m = step(m, project.SwitchProjectMsg{Root: b})
	rootA := m.ws.Background()[0]
	// Occupy B's Z slot with the non-global mate; sqlx (already followed in
	// dedicated) shares the slot per the runtime rules.
	m = step(m, ToolOpenMsg{Name: "mate"})
	mateLocs := m.toolLocations("mate")
	if len(mateLocs) != 1 {
		t.Fatalf("mate locations in B = %+v, want one", mateLocs)
	}
	mateTerm := *toolTerminal(m, "mate")
	t.Cleanup(func() { mateTerm.Close() })

	// Round-trip: sqlx detaches from B, visits A, and on return finds the
	// slot held by mate's pane — it must join as a tab, not split or dup.
	m = step(m, project.SwitchProjectMsg{Root: rootA})
	if got := countToolPanes(m, "sqlx") + len(m.toolLocations("sqlx")); got == 0 {
		t.Fatal("sqlx must follow into A")
	}
	m = step(m, project.SwitchProjectMsg{Root: b})
	locs := m.toolLocations("sqlx")
	if len(locs) != 1 || locs[0].tab < 0 {
		t.Fatalf("sqlx locations back in B = %+v, want a hosted tab (#1901)", locs)
	}
	hostInst := m.activeWS().Panes.Get(locs[0].key)
	foundMate := false
	for i := 0; i < hostInst.TabCount(); i++ {
		if tt := hostInst.TabTerminal(i); tt != nil && tt.Tool() == "mate" {
			foundMate = true
		}
	}
	if !foundMate {
		t.Fatal("sqlx must share the slot pane with mate")
	}
	if got := toolTerminal(m, "sqlx").SessionKey(); got != sess {
		t.Fatalf("session after tab-join arrival = %q, want %q", got, sess)
	}
}

// TestGlobalOnlyTabHostDetachesWhole: two global tools sharing one slot as
// tabs leave no husk behind on switch — the emptied host's leaf leaves the
// parked tree like a dedicated pane's — and both follow into the target,
// sharing its slot again.
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
		term := toolTerminal(m, name)
		if term == nil || !term.Running() {
			t.Fatalf("%s must follow into B with its live session", name)
		}
		if _, ok := m.ws.PeekGlobalTool(name); ok {
			t.Fatalf("%s must leave the manager stash once attached", name)
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
// whose layout recorded the global tool as a slot tab (#1901) re-attaches
// the session as that tab instead of spawning a duplicate — and the
// switch-in auto-attach adds no second instance.
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

// TestGlobalToolExitedWhileParkedShowsOverlay: a global tool whose process
// ends while the session is parked is not silently reaped (#1903) — the next
// switch-in materializes the pane with the #810 exited state, and Restart
// reruns the command in place as a running global session again.
func TestGlobalToolExitedWhileParkedShowsOverlay(t *testing.T) {
	_, b := twoRoots(t)
	m := globalToolModelWith(t, quickGlobalSettings)
	m = step(m, ToolOpenMsg{Name: "quick"})
	sessTerm := *m.toolPane("quick").Terminal()
	t.Cleanup(func() { sessTerm.Close() })
	sess := sessTerm.SessionKey()

	// Park the session (the mid-switch state) and let the process end there.
	m.detachGlobalTools()
	parked, ok := m.ws.PeekGlobalTool("quick")
	if !ok {
		t.Fatal("test setup: the session must park")
	}
	deadline := time.Now().Add(10 * time.Second)
	for parked.Running() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if parked.Running() {
		t.Fatal("test setup: the parked process must end on its own")
	}
	m = step(m, terminal.ExitedMsg{Key: sess})
	if _, ok := m.ws.PeekGlobalTool("quick"); !ok {
		t.Fatal("the exited session must stay parked, not be reaped")
	}

	// Switch in: the pane materializes showing the exited state.
	m = step(m, project.SwitchProjectMsg{Root: b})
	inst := m.toolPane("quick")
	if inst == nil {
		t.Fatal("the exited tool must materialize a pane on switch-in")
	}
	term := inst.Terminal()
	if term.Running() {
		t.Fatal("the materialized session must be the exited one")
	}
	if !term.IsCommand() || term.Tool() != "quick" {
		t.Fatal("the pane must still be the tool's command session (#810 overlay state)")
	}
	if _, hasCode := term.ExitCode(); !hasCode {
		t.Fatal("the exited pane must carry the exit status for the overlay")
	}

	// Restart reruns the command in place — a running global session again.
	term.Restart()
	restarted := *term
	t.Cleanup(func() { restarted.Close() })
	if !term.Running() {
		t.Fatal("Restart must yield a running session")
	}
	if term.Tool() != "quick" {
		t.Fatal("the restarted session must stay the global tool's")
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
