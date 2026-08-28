package project

import (
	"testing"
	"time"

	"ike/internal/config"
	"ike/internal/palette"
)

// TestPickerMRUOrderSurvivesRestart drives the whole loop the switcher rests
// on (#2317): three opens recorded through the config write-back, the config
// re-read from disk as a fresh session would, and the picker listing from it
// — most recently used first, the project we are standing in dropped.
func TestPickerMRUOrderSurvivesRestart(t *testing.T) {
	opts := testOpts(t)
	alpha, beta, gamma := t.TempDir(), t.TempDir(), t.TempDir()
	t0 := time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)
	// Visited alpha, then beta, then gamma — gamma is where we are now.
	for i, root := range []string{alpha, beta, gamma} {
		if err := RecordOpen(opts, root, t0.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	// A fresh process re-reads the user config from disk; the picker lists
	// from exactly that.
	cfg, _ := config.Load(opts)
	m, _ := newPicker(t, func() []Entry { return History(cfg) })
	// RecordOpen stores the validated (symlink-resolved) root, so compare
	// against the same resolution — t.TempDir hands out /var/… on macOS,
	// which resolves to /private/var/….
	wantAlpha, wantBeta, wantGamma := mustValidate(t, alpha), mustValidate(t, beta), mustValidate(t, gamma)
	items := m.Results("", palette.Context{Root: wantGamma})

	if len(items) != 2 {
		t.Fatalf("expected 2 items (gamma is the current project), got %+v", items)
	}
	if items[0].Detail != CompactPath(wantBeta) {
		t.Errorf("top row = %q, want the previous project %q", items[0].Detail, CompactPath(wantBeta))
	}
	if items[1].Detail != CompactPath(wantAlpha) {
		t.Errorf("second row = %q, want %q", items[1].Detail, CompactPath(wantAlpha))
	}

	// Bounce: switching back to beta re-records it, so from beta the top row
	// is gamma again — chord + enter alternates between the two.
	if err := RecordOpen(opts, beta, t0.Add(4*time.Hour)); err != nil {
		t.Fatal(err)
	}
	cfg2, _ := config.Load(opts)
	m2, _ := newPicker(t, func() []Entry { return History(cfg2) })
	back := m2.Results("", palette.Context{Root: wantBeta})
	if len(back) == 0 || back[0].Detail != CompactPath(wantGamma) {
		t.Fatalf("bounce back should top-list gamma, got %+v", back)
	}
}

// mustValidate resolves root the way RecordOpen stores it.
func mustValidate(t *testing.T, root string) string {
	t.Helper()
	abs, err := Validate(root)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}
