package config

import "testing"

// run.placement validation (#1905): the Run tool's home positions pass
// untouched, the pre-#1905 "new_terminal" migrates silently to the bottom
// dock it always meant, and anything else falls back to "bottom" with a
// diagnostic.
func TestValidateRunPlacement(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		diag     bool
	}{
		{in: "bottom", want: "bottom"},
		{in: "left", want: "left"},
		{in: "right", want: "right"},
		{in: "top", want: "top"},
		{in: "in_pane", want: "in_pane"},
		{in: "new_terminal", want: "bottom"}, // legacy, no diagnostic
		{in: "below", want: "bottom", diag: true},
		{in: "", want: "bottom", diag: true},
	} {
		c := defaults()
		c.Run.Placement = tc.in
		diags := validate(c)
		if c.Run.Placement != tc.want {
			t.Fatalf("placement %q validated to %q, want %q", tc.in, c.Run.Placement, tc.want)
		}
		got := 0
		for _, d := range diags {
			if d.Field == "run.placement" {
				got++
			}
		}
		if want := map[bool]int{true: 1, false: 0}[tc.diag]; got != want {
			t.Fatalf("placement %q: %d diagnostics, want %d (%v)", tc.in, got, want, diags)
		}
	}
}

// The shipped default is the bottom dock (#1905): the Run tool gets its own
// place instead of landing in the editor pane's tab strip.
func TestDefaultRunPlacement(t *testing.T) {
	if got := defaults().Run.Placement; got != "bottom" {
		t.Fatalf("default run.placement = %q, want bottom", got)
	}
}
