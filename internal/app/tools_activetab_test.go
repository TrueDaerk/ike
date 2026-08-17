package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/project"
)

// tools_activetab_test.go — the active tool tab is per-project state (#1906):
// layout.json records which tool tab was selected, the restore re-selects it,
// and the global-tool re-attach on switch-in (#1903) never overrides the tab
// the arriving project itself had active.

// localToolSettings declares two project-local tools whose processes outlive
// the assertions — the settings layer fixedDirApp's IKE_CONFIG_DIR resolves.
const localToolSettings = `
[[tools.custom]]
name = "alpha"
command = "sleep"
args = ["60"]

[[tools.custom]]
name = "beta"
command = "sleep"
args = ["60"]
`

// toolLayout writes settings.toml plus a layout.json into conf: explorer plus
// one editor pane carrying the given identity.
func toolLayout(t *testing.T, conf string, id paneIdentity) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(conf, "settings.toml"), []byte(localToolSettings), 0o644); err != nil {
		t.Fatal(err)
	}
	tree := &layout.Split{Orient: layout.Horizontal, Ratio: 0.3,
		A: &layout.Leaf{Pane: pane.ExplorerKey}, B: &layout.Leaf{Pane: "editor"}}
	encoded, err := layout.Encode(tree)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(persistedLayout{Tree: encoded, Panes: map[string]paneIdentity{
		pane.ExplorerKey: {Kind: "explorer"},
		"editor":         id,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conf, "layout.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// readToolLayout reads back the persisted identity of the "editor" pane.
func readToolLayout(t *testing.T, conf string) paneIdentity {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(conf, "layout.json"))
	if err != nil {
		t.Fatal(err)
	}
	var p persistedLayout
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	return p.Panes["editor"]
}

// activeToolTab returns the tool name of the pane's active tab, "" when the
// active tab hosts no tool session.
func activeToolTab(inst *pane.Instance) string {
	if t := inst.TabTerminal(inst.ActiveTab()); t != nil {
		return t.Tool()
	}
	return ""
}

// TestActiveToolTabRestoresFromLayout: a saved ActiveTool selects that tool's
// tab on restore instead of leaving the last restored tool active.
func TestActiveToolTabRestoresFromLayout(t *testing.T) {
	conf, dir := t.TempDir(), t.TempDir()
	t.Chdir(dir)
	toolLayout(t, conf, paneIdentity{Kind: "editor", Tools: []string{"alpha", "beta"}, ActiveTool: 1})

	m := fixedDirApp(t, conf)
	defer closeLeafTerminals(m)
	inst := m.activeWS().Panes.Get("editor")
	if inst == nil || inst.TabCount() != 2 {
		t.Fatalf("both tool tabs must restore, got %#v", inst)
	}
	if got := activeToolTab(inst); got != "alpha" {
		t.Fatalf("active tool tab = %q, want alpha (the saved selection)", got)
	}
}

// TestActiveToolTabRoundTripsThroughSave: selecting another tool tab persists,
// so the next start comes back on it — and a pane mixing a file tab with tool
// tabs keeps the distinction between "a file is active" and "a tool is".
func TestActiveToolTabRoundTripsThroughSave(t *testing.T) {
	conf, dir := t.TempDir(), t.TempDir()
	file := writeTemp(t, dir, "a.txt", "aaa\n")
	t.Chdir(dir)
	toolLayout(t, conf, paneIdentity{Kind: "editor", Tabs: []string{file},
		Tools: []string{"alpha", "beta"}, ActiveTool: 0})

	m := fixedDirApp(t, conf)
	defer closeLeafTerminals(m)
	inst := m.activeWS().Panes.Get("editor")
	if inst == nil || inst.TabCount() != 3 {
		t.Fatalf("the mixed host must restore file + tool tabs, got %#v", inst)
	}
	if got := activeToolTab(inst); got != "" {
		t.Fatalf("active tab = tool %q, want the file tab (ActiveTool 0)", got)
	}
	if id := readToolLayout(t, conf); id.ActiveTool != 0 {
		t.Fatalf("saved ActiveTool = %d, want 0 while a file tab is active", id.ActiveTool)
	}

	// Select the second tool tab and persist: ActiveTool indexes Tools 1-based.
	inst.ActivateTab(2)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	if id := readToolLayout(t, conf); id.ActiveTool != 2 {
		t.Fatalf("saved ActiveTool = %d, want 2 (beta)", id.ActiveTool)
	}

	m2 := fixedDirApp(t, conf)
	defer closeLeafTerminals(m2)
	inst2 := m2.activeWS().Panes.Get("editor")
	if got := activeToolTab(inst2); got != "beta" {
		t.Fatalf("active tool tab after restart = %q, want beta", got)
	}
}

// TestActiveToolTabFallbackWhenToolMissing: a remembered tool that no longer
// restores (unconfigured since the save) leaves the pane on a live tab
// instead of pointing at nothing.
func TestActiveToolTabFallbackWhenToolMissing(t *testing.T) {
	conf, dir := t.TempDir(), t.TempDir()
	t.Chdir(dir)
	toolLayout(t, conf, paneIdentity{Kind: "editor", Tools: []string{"alpha", "gone"}, ActiveTool: 2})

	m := fixedDirApp(t, conf)
	defer closeLeafTerminals(m)
	inst := m.activeWS().Panes.Get("editor")
	if inst == nil || inst.TabCount() != 1 {
		t.Fatalf("only the configured tool may restore, got %#v", inst)
	}
	if inst.ActiveTab() != 0 || activeToolTab(inst) != "alpha" {
		t.Fatalf("active tab = %d (%q), want the surviving alpha tab",
			inst.ActiveTab(), activeToolTab(inst))
	}
}

// selectToolTab activates the tab hosting the named tool and returns its host
// pane key.
func selectToolTab(t *testing.T, m Model, name string) string {
	t.Helper()
	locs := m.toolLocations(name)
	if len(locs) != 1 || locs[0].tab < 0 {
		t.Fatalf("%s must be hosted as exactly one tab, locations %+v", name, locs)
	}
	if !m.activeWS().Panes.Get(locs[0].key).ActivateTab(locs[0].tab) {
		t.Fatalf("activating the %s tab must succeed", name)
	}
	return locs[0].key
}

// assertActiveTool checks that the pane hosting name has it as its active tab.
func assertActiveTool(t *testing.T, m Model, name, ctx string) {
	t.Helper()
	locs := m.toolLocations(name)
	if len(locs) != 1 {
		t.Fatalf("%s: %s must live in exactly one place, locations %+v", ctx, name, locs)
	}
	if locs[0].tab < 0 {
		return // a dedicated pane: the tool is all there is to select
	}
	inst := m.activeWS().Panes.Get(locs[0].key)
	if got := activeToolTab(inst); got != name {
		t.Fatalf("%s: active tool tab = %q, want %q", ctx, got, name)
	}
}

// TestActiveToolTabStickyPerProject: two global tools share one slot pane as
// tabs in both projects; each project keeps the tab the user selected in it
// across repeated back-and-forth switches (#1906).
func TestActiveToolTabStickyPerProject(t *testing.T) {
	_, b := twoRoots(t)
	m := globalToolModelWith(t, slotGlobalSettings)

	m = step(m, ToolOpenMsg{Name: "sqlx"})
	m = step(m, ToolOpenMsg{Name: "gtwo"})
	s1, s2 := *toolTerminal(m, "sqlx"), *toolTerminal(m, "gtwo")
	t.Cleanup(func() { s1.Close(); s2.Close() })
	selectToolTab(t, m, "sqlx") // project A settles on sqlx

	m = step(m, project.SwitchProjectMsg{Root: b})
	rootA := m.ws.Background()[0]
	selectToolTab(t, m, "gtwo") // project B settles on gtwo

	for i := 0; i < 2; i++ {
		m = step(m, project.SwitchProjectMsg{Root: rootA})
		assertActiveTool(t, m, "sqlx", "back in A")
		m = step(m, project.SwitchProjectMsg{Root: b})
		assertActiveTool(t, m, "gtwo", "back in B")
	}
}

// TestGlobalToolAttachKeepsActiveTab: a global tool arriving on switch-in
// (#1903) joins the slot pane as a tab but must not steal the selection the
// target project had — only the project that had the tool active gets it back.
func TestGlobalToolAttachKeepsActiveTab(t *testing.T) {
	_, b := twoRoots(t)
	m := globalToolModelWith(t, slotGlobalSettings)

	// B owns a non-global mate tool in the shared slot, and never selects sqlx.
	m = step(m, project.SwitchProjectMsg{Root: b})
	rootA := m.ws.Background()[0]
	m = step(m, ToolOpenMsg{Name: "mate"})
	mate := *toolTerminal(m, "mate")
	t.Cleanup(mate.Close)

	// A opens the global sqlx; switching back to B brings it along as a tab.
	m = step(m, project.SwitchProjectMsg{Root: rootA})
	m = step(m, ToolOpenMsg{Name: "sqlx"})
	sqlx := *toolTerminal(m, "sqlx")
	t.Cleanup(sqlx.Close)

	m = step(m, project.SwitchProjectMsg{Root: b})
	locs := m.toolLocations("sqlx")
	if len(locs) != 1 || locs[0].tab < 0 {
		t.Fatalf("sqlx must follow into B as a slot tab, locations %+v", locs)
	}
	assertActiveTool(t, m, "mate", "B after the sqlx attach")
}

// TestActiveToolTabSurvivesClosedTool: the tool a project had active is closed
// everywhere (#1903) while another project is in front; switching back must
// fall back to a live tab instead of panicking or blanking the pane.
func TestActiveToolTabSurvivesClosedTool(t *testing.T) {
	_, b := twoRoots(t)
	m := globalToolModelWith(t, slotGlobalSettings)

	// A's slot pane hosts the project-local mate plus both global tools, and
	// settles on gtwo — the tab that will be gone on the way back.
	m = step(m, ToolOpenMsg{Name: "mate"})
	hostKey := m.toolPane("mate").Key()
	m = step(m, ToolOpenMsg{Name: "sqlx"})
	m = step(m, ToolOpenMsg{Name: "gtwo"})
	mate, s1, s2 := *toolTerminal(m, "mate"), *toolTerminal(m, "sqlx"), *toolTerminal(m, "gtwo")
	t.Cleanup(func() { mate.Close(); s1.Close(); s2.Close() })
	if host := m.activeWS().Panes.Get(hostKey); host == nil || host.TabCount() != 3 {
		t.Fatalf("precondition: all three tools must share the slot pane, got %#v", host)
	}
	selectToolTab(t, m, "gtwo")

	m = step(m, project.SwitchProjectMsg{Root: b})
	rootA := m.ws.Background()[0]
	// Close gtwo in B: the session ends everywhere, so A's remembered tab is
	// gone when it comes back.
	locs := m.toolLocations("gtwo")
	if len(locs) != 1 {
		t.Fatalf("gtwo must follow into B, locations %+v", locs)
	}
	if locs[0].tab < 0 {
		m.closePane(locs[0].key)
	} else {
		m.closeTab(m.activeWS().Panes.Get(locs[0].key), locs[0].tab)
	}

	m = step(m, project.SwitchProjectMsg{Root: rootA})
	if got := len(m.toolLocations("gtwo")); got != 0 {
		t.Fatalf("gtwo locations after the close = %d, want 0", got)
	}
	host := m.activeWS().Panes.Get(hostKey)
	if host == nil || host.TabCount() != 2 {
		t.Fatalf("A's host must come back with mate + sqlx, got %#v", host)
	}
	if got := activeToolTab(host); got != "mate" {
		t.Fatalf("active tab after the remembered tool vanished = %q, want mate "+
			"(the selection A held before the attach)", got)
	}
}
