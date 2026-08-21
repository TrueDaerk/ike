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

// layouts_redesign_test.go covers the #2042 layout model: the saved layout is
// the whole truth — multi-tool panes restore with exactly their saved tab
// set, tool hosts re-slot by tab composition instead of queue order, the HTTP
// response viewer is a fixed-position tool (never editor content), and the
// project switch groups arriving tools instead of scattering them.

// makeToolHost converts the named tool's dedicated pane into a tab host and
// adopts the other named tools' panes as tabs, returning the host instance.
func makeToolHost(t *testing.T, m *Model, first string, rest ...string) *pane.Instance {
	t.Helper()
	host := m.toolPane(first)
	if host == nil || !host.ConvertToTabHost() {
		t.Fatalf("tool pane %q must convert to a tab host", first)
	}
	for _, name := range rest {
		src := m.toolPane(name)
		if src == nil {
			t.Fatalf("tool pane %q missing", name)
		}
		m.adoptTerminalPane(src.Key(), host.Key())
	}
	if !toolTabHost(host) {
		t.Fatalf("pane must be a pure tool-tab host, tabs=%d", host.TabCount())
	}
	return host
}

// hostToolNames lists the tool names hosted as tabs, in tab order.
func hostToolNames(inst *pane.Instance) []string {
	tools, _ := editorPaneTools(inst)
	return tools
}

// totalToolSessions counts every live tool session in the tree — dedicated
// panes and hosted tabs.
func totalToolSessions(m Model) int {
	total := 0
	for key := range leafSet(m) {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil {
			continue
		}
		switch inst.Kind() {
		case pane.KindTerminal:
			if inst.Terminal().Tool() != "" {
				total++
			}
		case pane.KindEditor:
			for i := 0; i < inst.TabCount(); i++ {
				if tt := inst.TabTerminal(i); tt != nil && tt.Tool() != "" {
					total++
				}
			}
		}
	}
	return total
}

// TestApplyLayoutMatchesToolHostsByComposition (#2042): with two multi-tool
// hosts in the layout, apply re-slots each live host into the slot whose
// saved tool list it matches — never by blind queue order, which would swap
// the two tool areas when registry order and slot order diverge.
func TestApplyLayoutMatchesToolHostsByComposition(t *testing.T) {
	withTools(t, sleepTool("alpha"), sleepTool("beta"), sleepTool("gamma"), sleepTool("delta"))
	m := sized(t, 120, 40)
	m = step(m, ToolOpenMsg{Name: "alpha"})
	m = step(m, ToolOpenMsg{Name: "beta"})
	hostA := makeToolHost(t, &m, "alpha", "beta")
	m = step(m, ToolOpenMsg{Name: "gamma"})
	m = step(m, ToolOpenMsg{Name: "delta"})
	hostB := makeToolHost(t, &m, "gamma", "delta")
	snap, ok := snapshotLayout(m.activeWS().Tree, m.activeWS().Panes)
	if !ok {
		t.Fatal("snapshot failed")
	}
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": snap}})

	// Close the alpha+beta host and rebuild it, so its registry position now
	// trails the gamma+delta host — a FIFO match would hand the gamma+delta
	// host to the alpha+beta slot.
	hostA.CloseTerminalTabs()
	m.activeWS().Panes.Close(hostA.Key())
	m.closePane(hostA.Key())
	m = step(m, ToolOpenMsg{Name: "alpha"})
	m = step(m, ToolOpenMsg{Name: "beta"})
	makeToolHost(t, &m, "alpha", "beta")

	m = step(m, ApplyLayoutMsg{Name: "dev"})
	defer closeLeafTerminals(m)
	if got := hostToolNames(m.activeWS().Panes.Get(hostB.Key())); len(got) != 2 || got[0] != "gamma" || got[1] != "delta" {
		t.Fatalf("the gamma+delta host must keep exactly its own tabs, got %v", got)
	}
	if total := totalToolSessions(m); total != 4 {
		t.Fatalf("apply must not restart or duplicate tools, got %d sessions", total)
	}
}

// TestApplyLayoutRestoresMissingToolTab (#2042): a multi-tool pane restores
// with exactly its saved tab set — a tab closed since the save comes back on
// apply, in the same host pane.
func TestApplyLayoutRestoresMissingToolTab(t *testing.T) {
	withTools(t, sleepTool("alpha"), sleepTool("beta"))
	m := sized(t, 120, 40)
	m = step(m, ToolOpenMsg{Name: "alpha"})
	m = step(m, ToolOpenMsg{Name: "beta"})
	host := makeToolHost(t, &m, "alpha", "beta")
	snap, ok := snapshotLayout(m.activeWS().Tree, m.activeWS().Panes)
	if !ok {
		t.Fatal("snapshot failed")
	}
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": snap}})

	// Close the beta tab; the host keeps hosting alpha alone.
	for i := 0; i < host.TabCount(); i++ {
		if tt := host.TabTerminal(i); tt != nil && tt.Tool() == "beta" {
			tt.Close()
			host.CloseTab(i)
			break
		}
	}
	if got := hostToolNames(host); len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("precondition: host must hold alpha alone, got %v", got)
	}

	m = step(m, ApplyLayoutMsg{Name: "dev"})
	defer closeLeafTerminals(m)
	got := hostToolNames(m.activeWS().Panes.Get(host.Key()))
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("apply must restore the missing beta tab in place, got %v", got)
	}
}

// TestHTTPViewerSnapshotsAsToolAndReslotsOnApply (#2042): the HTTP response
// viewer snapshots under its singleton identity — never as an anonymous
// editor slot — and an apply re-slots the live viewer pane instead of
// treating it as editor content.
func TestHTTPViewerSnapshotsAsToolAndReslotsOnApply(t *testing.T) {
	m := sized(t, 100, 40)
	m.openHTTPPanel()
	if !leafSet(m)[pane.HTTPKey] {
		t.Fatal("precondition: the HTTP viewer pane must be open")
	}
	snap, ok := snapshotLayout(m.activeWS().Tree, m.activeWS().Panes)
	if !ok {
		t.Fatal("snapshot failed")
	}
	id, found := snap.Panes[pane.HTTPKey]
	if !found || id.Kind != "http" {
		t.Fatalf("the viewer must snapshot as the http singleton, got %+v (panes %v)", id, snap.Panes)
	}
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": snap}})

	m = step(m, ApplyLayoutMsg{Name: "dev"})
	if !leafSet(m)[pane.HTTPKey] {
		t.Fatalf("apply must re-slot the live viewer at its saved position, leaves %v", leafSet(m))
	}
	if inst := m.activeWS().Panes.Get(pane.HTTPKey); inst == nil || inst.Kind() != pane.KindHTTP {
		t.Fatal("the http leaf must be backed by the singleton viewer instance")
	}
}

// TestLegacyHTTPContentTabRestoresAsNothing (#2042): a pre-#2042 layout.json
// with the HTTP viewer nested as a content tab restores without the tab and
// without crashing — the viewer is a tool window now and reopens empty via
// http.run anyway.
func TestLegacyHTTPContentTabRestoresAsNothing(t *testing.T) {
	conf, dir := t.TempDir(), t.TempDir()
	t.Chdir(dir)
	tree := &layout.Split{Orient: layout.Horizontal, Ratio: 0.3,
		A: &layout.Leaf{Pane: pane.ExplorerKey}, B: &layout.Leaf{Pane: "editor"}}
	encoded, err := layout.Encode(tree)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(persistedLayout{Tree: encoded, Panes: map[string]paneIdentity{
		pane.ExplorerKey: {Kind: "explorer"},
		"editor": {Kind: "editor",
			CTabs: []contentTabIdentity{{Kind: "http", Index: 1}}, ActiveCTab: 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conf, "layout.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	m := fixedDirApp(t, conf)
	inst := m.activeWS().Panes.Get("editor")
	if inst == nil || inst.Kind() != pane.KindEditor {
		t.Fatalf("editor pane must restore, got %+v", inst)
	}
	for i := 0; i < inst.TabCount(); i++ {
		if c := inst.TabContent(i); c != nil && c.Kind() == pane.KindHTTP {
			t.Fatal("a legacy nested http tab must restore as nothing")
		}
	}
}

// TestSwitchAttachGroupsUnplacedGlobalTools (#2042): two global tools with no
// configured position arrive in the next project grouped as tabs of one pane
// instead of scattering as separate splits over the editor area.
func TestSwitchAttachGroupsUnplacedGlobalTools(t *testing.T) {
	_, b := twoRoots(t)
	m := globalToolModelWith(t, `
[[tools.custom]]
name = "gone"
command = "sleep"
args = ["60"]
global = true

[[tools.custom]]
name = "gtwo"
command = "sleep"
args = ["60"]
global = true
`)
	m = step(m, ToolOpenMsg{Name: "gone"})
	m = step(m, ToolOpenMsg{Name: "gtwo"})
	for _, name := range []string{"gone", "gtwo"} {
		term := toolTerminal(m, name)
		if term == nil {
			t.Fatalf("tool %q must be open in A", name)
		}
		keep := *term
		t.Cleanup(func() { keep.Close() })
	}

	m = step(m, project.SwitchProjectMsg{Root: b})
	oneLocs, twoLocs := m.toolLocations("gone"), m.toolLocations("gtwo")
	if len(oneLocs) != 1 || len(twoLocs) != 1 {
		t.Fatalf("both tools must arrive in B, got %v / %v", oneLocs, twoLocs)
	}
	if oneLocs[0].key != twoLocs[0].key {
		t.Fatalf("arriving tools must group in one pane, got %q and %q", oneLocs[0].key, twoLocs[0].key)
	}
}

// TestSwitchAttachJoinsHomeDockOccupant (#2042): global tools sharing a home
// placement land in one tabbed pane at that dock on switch-in — "all bottom
// right" stays one bottom strip instead of subdividing per tool.
func TestSwitchAttachJoinsHomeDockOccupant(t *testing.T) {
	_, b := twoRoots(t)
	m := globalToolModelWith(t, `
[[tools.custom]]
name = "gone"
command = "sleep"
args = ["60"]
global = true
placement = "bottom"

[[tools.custom]]
name = "gtwo"
command = "sleep"
args = ["60"]
global = true
placement = "bottom"
`)
	m = step(m, ToolOpenMsg{Name: "gone"})
	m = step(m, ToolOpenMsg{Name: "gtwo"})
	for _, name := range []string{"gone", "gtwo"} {
		term := toolTerminal(m, name)
		if term == nil {
			t.Fatalf("tool %q must be open in A", name)
		}
		keep := *term
		t.Cleanup(func() { keep.Close() })
	}

	m = step(m, project.SwitchProjectMsg{Root: b})
	oneLocs, twoLocs := m.toolLocations("gone"), m.toolLocations("gtwo")
	if len(oneLocs) != 1 || len(twoLocs) != 1 {
		t.Fatalf("both tools must arrive in B, got %v / %v", oneLocs, twoLocs)
	}
	if oneLocs[0].key != twoLocs[0].key {
		t.Fatalf("tools sharing the bottom home must share one pane, got %q and %q", oneLocs[0].key, twoLocs[0].key)
	}
}
