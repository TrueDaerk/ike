package config

import "testing"

// diff.placement validation (#2507): the two placements pass untouched, an
// unknown value (a typo, an older spelling) falls back to "focused" with a
// diagnostic naming the field.
func TestValidateDiffPlacement(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		diag     bool
	}{
		{in: "focused", want: "focused"},
		{in: "split", want: "split"},
		{in: "beside", want: "focused", diag: true},
		{in: "", want: "focused", diag: true},
	} {
		c := defaults()
		c.Diff.Placement = tc.in
		diags := validate(c)
		if c.Diff.Placement != tc.want {
			t.Fatalf("placement %q validated to %q, want %q", tc.in, c.Diff.Placement, tc.want)
		}
		got := 0
		for _, d := range diags {
			if d.Field == "diff.placement" {
				got++
			}
		}
		if want := map[bool]int{true: 1, false: 0}[tc.diag]; got != want {
			t.Fatalf("placement %q: %d diagnostics, want %d (%v)", tc.in, got, want, diags)
		}
	}
}

// The shipped default opens a diff where the user is looking (#2507).
func TestDefaultDiffPlacement(t *testing.T) {
	if got := defaults().Diff.Placement; got != "focused" {
		t.Fatalf("default diff.placement = %q, want focused", got)
	}
	if got, ok := defaults().Flat()["diff.placement"]; !ok || got != "focused" {
		t.Fatalf("flat diff.placement = %q (ok=%v), want focused", got, ok)
	}
}
