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

// switch_toolplacement_test.go covers #2141: the project-switch and startup
// restore paths must put tools back in the pane the project's layout records,
// not wherever the runtime open rules would drop them. The manual layout
// apply (applySnapshot) always did — its snapshot is the truth — while the
// switch-in re-attach of a parked global session (#1903) followed slot, home
// and "group with any other tool" rules alone, so a tabbed group dissolved
// into the tool pane next door.

// issue2141Tools declares the issue's four global tools; template and assign
// are supplied per test so both the slotted and the unslotted arrangement are
// covered.
const issue2141Tools = `
[[tools.custom]]
name = "claude"
command = "sleep"
args = ["60"]
global = true

[[tools.custom]]
name = "lazygit"
command = "sleep"
args = ["60"]
global = true

[[tools.custom]]
name = "sql"
command = "sleep"
args = ["60"]
global = true

[[tools.custom]]
name = "yarn"
command = "sleep"
args = ["60"]
global = true
`

// issue2141Slots is issue2141Tools plus the issue's slot configuration: claude
// pinned to T, the three tabbed tools to Z, the explorer to X.
const issue2141Slots = issue2141Tools + `
[tools.layout]
assign = ["T=claude", "Z=lazygit", "Z=sql", "Z=yarn", "X=explorer"]
template = ["XEEEE", "XEEEE", "TTZZZ"]
`

// writeIssue2141Layout plants the issue's saved layout in root: claude as the
// dedicated "terminal" pane, lazygit/sql/yarn as the tab list of the "editor"
// tools host (the kind: "tools" pane of the issue), the explorer and a plain
// editor beside them.
func writeIssue2141Layout(t *testing.T, root string) {
	t.Helper()
	tree := &layout.Split{Orient: layout.Vertical, Ratio: 2.0 / 3.0,
		A: &layout.Split{Orient: layout.Horizontal, Ratio: 0.25,
			A: &layout.Leaf{Pane: pane.ExplorerKey},
			B: &layout.Leaf{Pane: "editor:2"}},
		B: &layout.Split{Orient: layout.Horizontal, Ratio: 0.5,
			A: &layout.Leaf{Pane: "terminal"},
			B: &layout.Leaf{Pane: "editor"}}}
	encoded, err := layout.Encode(tree)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(persistedLayout{Tree: encoded, Panes: map[string]paneIdentity{
		pane.ExplorerKey: {Kind: "explorer"},
		"editor:2":       {Kind: "editor"},
		"terminal":       {Kind: "tool", Tool: "claude"},
		"editor":         {Kind: "tools", Tools: []string{"lazygit", "sql", "yarn"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".ike"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".ike", "layout.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// soleToolPane resolves the single pane hosting the named tool, failing when
// the tool is missing or duplicated.
func soleToolPane(t *testing.T, m Model, name string) string {
	t.Helper()
	locs := m.toolLocations(name)
	if len(locs) != 1 {
		t.Fatalf("tool %q must be open exactly once, got %v", name, locs)
	}
	return locs[0].key
}

// assertIssue2141Grouping checks the issue's arrangement: the three Z tools
// share one pane and claude keeps its own.
func assertIssue2141Grouping(t *testing.T, m Model) (tools, claude string) {
	t.Helper()
	claude = soleToolPane(t, m, "claude")
	tools = soleToolPane(t, m, "lazygit")
	for _, name := range []string{"sql", "yarn"} {
		if key := soleToolPane(t, m, name); key != tools {
			t.Fatalf("tool %q must share the lazygit pane %q, got %q", name, tools, key)
		}
	}
	if tools == claude {
		t.Fatalf("the tabbed tools must not land in the claude pane %q", claude)
	}
	return tools, claude
}

// TestSwitchBackRestoresSavedToolTabPane (#2141): a project left with claude
// in its own pane and lazygit/sql/yarn tabbed in another gets that split back
// when it is switched into again. Before the fix all four global sessions
// re-attached through the "group with any other tool pane" rule and piled up
// as tabs of the claude pane.
func TestSwitchBackRestoresSavedToolTabPane(t *testing.T) {
	a, b := twoRoots(t)
	writeIssue2141Layout(t, a)
	m := globalToolModelWith(t, issue2141Tools)
	assertIssue2141Grouping(t, m)

	m = step(m, project.SwitchProjectMsg{Root: b})
	m = step(m, project.SwitchProjectMsg{Root: a})
	defer closeLeafTerminals(m)
	assertIssue2141Grouping(t, m)
}

// TestSwitchIntoProjectRestoresSavedToolTabPane (#2141): switching into a
// project whose layout hosts the tools as tabs restores them in that pane,
// not next to the tool the switch carried in.
func TestSwitchIntoProjectRestoresSavedToolTabPane(t *testing.T) {
	_, b := twoRoots(t)
	writeIssue2141Layout(t, b)
	m := globalToolModelWith(t, issue2141Tools)
	for _, name := range []string{"claude", "lazygit", "sql", "yarn"} {
		m = step(m, ToolOpenMsg{Name: name})
	}

	m = step(m, project.SwitchProjectMsg{Root: b})
	defer closeLeafTerminals(m)
	tools, _ := assertIssue2141Grouping(t, m)
	if tools != "editor" {
		t.Fatalf("the tools host must be the saved %q pane, got %q", "editor", tools)
	}
}

// TestSwitchBackKeepsSlottedToolTabsInTheirSlot (#2141): with the issue's
// [tools.layout] in force the returning sessions land in the Z slot pane and
// claude in T — the slot rule and the saved placement agree, and neither is
// lost on the way back.
func TestSwitchBackKeepsSlottedToolTabsInTheirSlot(t *testing.T) {
	a, b := twoRoots(t)
	m := globalToolModelWith(t, issue2141Slots)
	for _, name := range []string{"claude", "lazygit", "sql", "yarn"} {
		m = step(m, ToolOpenMsg{Name: name})
	}
	assertIssue2141Grouping(t, m)

	m = step(m, project.SwitchProjectMsg{Root: b})
	m = step(m, project.SwitchProjectMsg{Root: a})
	defer closeLeafTerminals(m)
	tools, claude := assertIssue2141Grouping(t, m)
	res := m.slotResidents()
	if res["Z"] != tools {
		t.Fatalf("Z slot = %q, want the tools host %q (residents %v)", res["Z"], tools, res)
	}
	if res["T"] != claude {
		t.Fatalf("T slot = %q, want the claude pane %q (residents %v)", res["T"], claude, res)
	}
}

// TestRestoreKeepsSavedToolTabPane (#2141): the startup restore of the same
// layout — no parked session in play — reproduces the saved arrangement too,
// which is what makes a plain IKE restart come back as it was left.
func TestRestoreKeepsSavedToolTabPane(t *testing.T) {
	a, _ := twoRoots(t)
	writeIssue2141Layout(t, a)
	m := globalToolModelWith(t, issue2141Tools)
	defer closeLeafTerminals(m)
	tools, _ := assertIssue2141Grouping(t, m)
	if tools != "editor" {
		t.Fatalf("the tools host must be the saved %q pane, got %q", "editor", tools)
	}
}
