package config

import "testing"

// debug.session_end validation (#2190): keep/close pass untouched, anything
// else falls back to "keep" with a diagnostic.
func TestValidateDebugSessionEnd(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		diag     bool
	}{
		{in: "keep", want: "keep"},
		{in: "close", want: "close"},
		{in: "explode", want: "keep", diag: true},
		{in: "", want: "keep", diag: true},
	} {
		c := defaults()
		c.Debug.SessionEnd = tc.in
		diags := validate(c)
		if c.Debug.SessionEnd != tc.want {
			t.Fatalf("session_end %q validated to %q, want %q", tc.in, c.Debug.SessionEnd, tc.want)
		}
		got := 0
		for _, d := range diags {
			if d.Field == "debug.session_end" {
				got++
			}
		}
		if want := map[bool]int{true: 1, false: 0}[tc.diag]; got != want {
			t.Fatalf("session_end %q: %d diagnostics, want %d (%v)", tc.in, got, want, diags)
		}
	}
}

// The shipped default keeps the finished debug area open (the historic #689
// behavior); the flat key backs the Settings UI entry.
func TestDefaultDebugSessionEnd(t *testing.T) {
	if got := defaults().Debug.SessionEnd; got != "keep" {
		t.Fatalf("default debug.session_end = %q, want keep", got)
	}
	if v, ok := defaults().Flat()["debug.session_end"]; !ok || v != "keep" {
		t.Fatalf("flat debug.session_end = %q (ok=%v), want keep", v, ok)
	}
}
