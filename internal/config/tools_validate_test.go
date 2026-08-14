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
