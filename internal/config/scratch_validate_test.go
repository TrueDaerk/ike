package config

import "testing"

// [scratch] migration (#1963): the #1932 tool-pane keys panel / panel_height
// migrate silently onto the explorer-section keys — panel_height (chrome rows
// removed) seeds section_height unless that was set explicitly, panel is
// dropped — and the new keys validate with bounds and a sort enum.
func TestValidateScratchMigration(t *testing.T) {
	// Legacy panel_height seeds section_height: the old default 8 lands on
	// the new default 5, silently.
	c := defaults()
	c.Scratch.PanelHeight = 8
	c.Scratch.Panel = true
	diags := validate(c)
	if c.Scratch.SectionHeight != 5 || c.Scratch.PanelHeight != 0 || c.Scratch.Panel {
		t.Fatalf("migrated scratch = %+v", c.Scratch)
	}
	for _, d := range diags {
		if d.Field == "scratch.panel_height" || d.Field == "scratch.panel" {
			t.Fatalf("legacy keys must migrate silently, got %v", diags)
		}
	}
	// An explicit section_height wins over the legacy key.
	c = defaults()
	c.Scratch.PanelHeight = 20
	c.Scratch.SectionHeight = 9
	validate(c)
	if c.Scratch.SectionHeight != 9 {
		t.Fatalf("explicit section_height must win, got %d", c.Scratch.SectionHeight)
	}
	// A tiny legacy height clamps to the section minimum.
	c = defaults()
	c.Scratch.PanelHeight = 3
	validate(c)
	if c.Scratch.SectionHeight != 2 {
		t.Fatalf("clamped migration = %d want 2", c.Scratch.SectionHeight)
	}
}

// scratch.section_height bounds and scratch.sort enum (#1963).
func TestValidateScratchSectionValues(t *testing.T) {
	c := defaults()
	c.Scratch.SectionHeight = 0
	c.Scratch.Sort = "size"
	diags := validate(c)
	if c.Scratch.SectionHeight != 5 || c.Scratch.Sort != "name" {
		t.Fatalf("validated scratch = %+v", c.Scratch)
	}
	seen := map[string]bool{}
	for _, d := range diags {
		seen[d.Field] = true
	}
	if !seen["scratch.section_height"] || !seen["scratch.sort"] {
		t.Fatalf("want diagnostics for both bad values, got %v", diags)
	}
	c = defaults()
	c.Scratch.SectionHeight = 99
	validate(c)
	if c.Scratch.SectionHeight != 30 {
		t.Fatalf("height must clamp to 30, got %d", c.Scratch.SectionHeight)
	}
	c = defaults()
	c.Scratch.Sort = "modified"
	if diags := validate(c); c.Scratch.Sort != "modified" || len(diagsFor(diags, "scratch.sort")) != 0 {
		t.Fatalf("modified must validate cleanly: %v", diags)
	}
}

// A leftover [tools.layout] assignment of the removed "scratch" tool drops
// silently (#1963), like the other migrated legacy values.
func TestValidateScratchSlotAssignmentDropped(t *testing.T) {
	c := defaults()
	c.Tools.Layout.Template = []string{"AEB", "AEB"}
	c.Tools.Layout.Assign = []string{"A=scratch", "B=problems"}
	diags := validate(c)
	if len(c.Tools.Layout.Assign) != 1 || c.Tools.Layout.Assign[0] != "B=problems" {
		t.Fatalf("assign = %v want just B=problems", c.Tools.Layout.Assign)
	}
	if len(diagsFor(diags, "tools.layout.assign")) != 0 {
		t.Fatalf("the scratch assignment must drop silently, got %v", diags)
	}
}

// diagsFor filters diagnostics by field.
func diagsFor(ds []Diagnostic, field string) []Diagnostic {
	var out []Diagnostic
	for _, d := range ds {
		if d.Field == field {
			out = append(out, d)
		}
	}
	return out
}
