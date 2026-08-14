package config

import "testing"

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
