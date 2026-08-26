package config

import "testing"

// TestWatchdogSecondsValidation pins the perf.watchdog_seconds bounds
// (#2163): negative values disable with a diagnostic, oversized ones clamp,
// and the documented opt-out 0 passes silently.
func TestWatchdogSecondsValidation(t *testing.T) {
	cases := []struct {
		in, want int
		diag     bool
	}{
		{in: -5, want: 0, diag: true},
		{in: 0, want: 0, diag: false},
		{in: 15, want: 15, diag: false},
		{in: 601, want: 600, diag: true},
	}
	for _, c := range cases {
		cfg := defaults()
		cfg.Perf.WatchdogSeconds = c.in
		diags := validate(cfg)
		if cfg.Perf.WatchdogSeconds != c.want {
			t.Errorf("watchdog %d: got %d, want %d", c.in, cfg.Perf.WatchdogSeconds, c.want)
		}
		found := false
		for _, d := range diags {
			if d.Field == "perf.watchdog_seconds" {
				found = true
			}
		}
		if found != c.diag {
			t.Errorf("watchdog %d: diagnostic presence = %v, want %v", c.in, found, c.diag)
		}
	}
}
