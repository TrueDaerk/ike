package config

import (
	"strings"
	"testing"
)

// [[tools.custom]] placement validation (#1889): the home dock edges pass
// untouched; anything else — including pre-#1588 legacy values — clears to
// the adaptive default with a diagnostic.
func TestValidateToolPlacement(t *testing.T) {
	c := defaults()
	c.Tools.Custom = []ToolEntry{
		{Name: "a", Command: "a", Placement: "bottom"},
		{Name: "b", Command: "b", Placement: "left"},
		{Name: "c", Command: "c", Placement: ""},
		{Name: "d", Command: "d", Placement: "below"},
	}
	diags := validate(c)
	for i, want := range []string{"bottom", "left", "", ""} {
		if got := c.Tools.Custom[i].Placement; got != want {
			t.Fatalf("entry %d placement = %q, want %q", i, got, want)
		}
	}
	bad := 0
	for _, d := range diags {
		if d.Field == "tools.custom.placement" {
			bad++
		}
	}
	if bad != 1 {
		t.Fatalf("want 1 placement diagnostic, got %d (%v)", bad, diags)
	}
}

// [[tools.custom]] global vs multiple (#1890): the two flags are mutually
// exclusive — a config declaring both keeps global, drops multiple and gets
// one diagnostic; entries with only one of the flags pass untouched.
func TestValidateToolGlobalMultipleExclusive(t *testing.T) {
	c := defaults()
	c.Tools.Custom = []ToolEntry{
		{Name: "a", Command: "a", Global: true, Multiple: true},
		{Name: "b", Command: "b", Global: true},
		{Name: "c", Command: "c", Multiple: true},
	}
	diags := validate(c)
	if !c.Tools.Custom[0].Global || c.Tools.Custom[0].Multiple {
		t.Fatalf("global+multiple entry must keep global and drop multiple, got %+v", c.Tools.Custom[0])
	}
	if !c.Tools.Custom[1].Global || c.Tools.Custom[1].Multiple {
		t.Fatalf("global-only entry must pass untouched, got %+v", c.Tools.Custom[1])
	}
	if c.Tools.Custom[2].Global || !c.Tools.Custom[2].Multiple {
		t.Fatalf("multiple-only entry must pass untouched, got %+v", c.Tools.Custom[2])
	}
	bad := 0
	for _, d := range diags {
		if d.Field == "tools.custom.multiple" {
			bad++
		}
	}
	if bad != 1 {
		t.Fatalf("want 1 multiple diagnostic, got %d (%v)", bad, diags)
	}
}

// [[tools.custom]] guard (#2704): the key is the close/quit guard opt-out and
// defaults to guarded when absent.
func TestToolEntryGuardDefault(t *testing.T) {
	no, yes := false, true
	for _, tc := range []struct {
		name  string
		entry ToolEntry
		want  bool
	}{
		{"absent", ToolEntry{Name: "a"}, true},
		{"explicit true", ToolEntry{Name: "b", Guard: &yes}, true},
		{"explicit false", ToolEntry{Name: "c", Guard: &no}, false},
	} {
		if got := tc.entry.GuardsClose(); got != tc.want {
			t.Errorf("%s: GuardsClose = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// guard = false decodes from a real config file (#2704); an entry without the
// key stays guarded, and the key raises no unknown-setting diagnostic.
func TestToolGuardDecodesFromTOML(t *testing.T) {
	proj := writeProject(t, "[[tools.custom]]\nname = \"sql\"\ncommand = \"sqlite3\"\nguard = false\n\n[[tools.custom]]\nname = \"yarn\"\ncommand = \"yarn\"\n")
	c, diags := Load(Options{ProjectRoot: proj})
	for _, d := range diags {
		if strings.Contains(d.Message, "unknown setting") {
			t.Fatalf("guard must be a known key, diags = %v", diags)
		}
	}
	if len(c.Tools.Custom) != 2 {
		t.Fatalf("tools = %+v, want both entries", c.Tools.Custom)
	}
	if c.Tools.Custom[0].GuardsClose() {
		t.Errorf("guard = false must exempt the entry, got %+v", c.Tools.Custom[0])
	}
	if !c.Tools.Custom[1].GuardsClose() {
		t.Errorf("an absent guard key must stay guarded, got %+v", c.Tools.Custom[1])
	}
}

// A global tool (#1890) is already exempt because closing a project detaches
// it, so guard = false there is dropped with a diagnostic (#2704).
func TestValidateToolGuardIgnoredForGlobal(t *testing.T) {
	no := false
	c := defaults()
	c.Tools.Custom = []ToolEntry{
		{Name: "a", Command: "a", Global: true, Guard: &no},
		{Name: "b", Command: "b", Guard: &no},
	}
	diags := validate(c)
	if c.Tools.Custom[0].Guard != nil || !c.Tools.Custom[0].GuardsClose() {
		t.Fatalf("a global tool must drop guard, got %+v", c.Tools.Custom[0])
	}
	if c.Tools.Custom[1].GuardsClose() {
		t.Fatalf("a project-scoped tool must keep guard = false, got %+v", c.Tools.Custom[1])
	}
	bad := 0
	for _, d := range diags {
		if d.Field == "tools.custom.guard" {
			bad++
		}
	}
	if bad != 1 {
		t.Fatalf("want 1 guard diagnostic, got %d (%v)", bad, diags)
	}
}

// tools.layout (#1897): a well-formed template passes with its assignments
// normalized; malformed assignment entries, unknown slots, the editor
// region and duplicate tool assignments are dropped with diagnostics.
func TestValidateToolLayout(t *testing.T) {
	c := defaults()
	c.Tools.Custom = []ToolEntry{{Name: "lazygit", Command: "lazygit"}}
	c.Tools.Layout = ToolLayout{
		Template: []string{"XEEH", "XEEH", "TTZZ"},
		Assign: []string{
			"X=explorer",
			" T = lazygit ", // whitespace normalizes
			"E=vcs",         // editor region — dropped
			"Q=htop",        // unknown slot — dropped
			"Z=lazygit",     // duplicate tool — dropped
			"broken",        // not SLOT=tool — dropped
		},
	}
	diags := validate(c)
	want := []string{"X=explorer", "T=lazygit"}
	if len(c.Tools.Layout.Assign) != len(want) {
		t.Fatalf("assign = %v, want %v", c.Tools.Layout.Assign, want)
	}
	for i := range want {
		if c.Tools.Layout.Assign[i] != want[i] {
			t.Fatalf("assign = %v, want %v", c.Tools.Layout.Assign, want)
		}
	}
	bad := 0
	for _, d := range diags {
		if d.Field == "tools.layout.assign" {
			bad++
		}
	}
	if bad != 4 {
		t.Fatalf("want 4 assign diagnostics, got %d (%v)", bad, diags)
	}
}

// A template the layout engine rejects disables slot placement wholesale.
func TestValidateToolLayoutBadTemplate(t *testing.T) {
	c := defaults()
	c.Tools.Layout = ToolLayout{Template: []string{"XX", "XE"}, Assign: []string{"X=explorer"}}
	diags := validate(c)
	if len(c.Tools.Layout.Template) != 0 {
		t.Fatalf("broken template must clear, got %v", c.Tools.Layout.Template)
	}
	found := false
	for _, d := range diags {
		if d.Field == "tools.layout.template" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a template diagnostic, got %v", diags)
	}
}

// An assignment naming a tool that is neither a built-in id nor a custom
// tool warns but stays (#1946): the entry is inert, and dropping it would
// rewrite the list on the next settings-UI edit. Every BuiltinAssignTools id
// — terminal and run included — passes silently.
func TestValidateToolLayoutUnknownTool(t *testing.T) {
	c := defaults()
	slots := "ABCDFGHIJKLMNX" // one 1x1 slot per id plus X for the typo; no E
	assign := []string{"X=typo"}
	for i, id := range BuiltinAssignTools() {
		assign = append(assign, string(slots[i])+"="+id)
	}
	c.Tools.Layout = ToolLayout{
		Template: []string{slots, strings.Repeat("E", len(slots))},
		Assign:   assign,
	}
	diags := validate(c)
	if len(c.Tools.Layout.Assign) != len(assign) {
		t.Fatalf("assign = %v, want all %d entries kept", c.Tools.Layout.Assign, len(assign))
	}
	var msgs []string
	for _, d := range diags {
		if d.Field == "tools.layout.assign" {
			msgs = append(msgs, d.Message)
		}
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0], `unknown tool "typo"`) || !strings.Contains(msgs[0], "terminal") {
		t.Fatalf("want exactly the unknown-tool diagnostic naming the valid ids, got %v", msgs)
	}
}
