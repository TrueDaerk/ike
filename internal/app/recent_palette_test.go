package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/palette"
	"ike/internal/telemetry"
)

// recent_palette_test.go covers the root-model half of #2399: the dismissal
// telemetry event and the per-project preselection memory.

// TestRecentDismissRecordsTelemetry is the acceptance criterion for the
// dismissal event: esc out of the recent-files dialog lands as its own
// "palette.dismiss" event (#2408 moved it off the pseudo-command #2399 used)
// carrying the mode and the typed query length.
func TestRecentDismissRecordsTelemetry(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	tm, _ := m.Update(ShowRecentFilesMsg{})
	m = tm.(Model)
	// Type a filter, then give up on it — the streak shape the event exists
	// to make visible.
	for _, r := range "main" {
		tm, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = tm.(Model)
	}
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.palette.IsOpen() {
		t.Fatal("esc must close the dialog")
	}

	got := eventsOf(usageEvents(t, m), telemetry.TypePaletteDismiss)
	if len(got) != 1 {
		t.Fatalf("want one %s event, got %v", telemetry.TypePaletteDismiss, got)
	}
	if want := string(palette.RecentPrefix); got[0].Data["mode"] != want {
		t.Fatalf("mode = %q, want %q", got[0].Data["mode"], want)
	}
	if got[0].Data["query_len"] != "4" {
		t.Fatalf("query_len = %q, want \"4\"", got[0].Data["query_len"])
	}
	if _, ok := got[0].Data["ms"]; !ok {
		t.Fatalf("dismissal must carry the open duration: %v", got[0])
	}
}

// TestOtherPaletteDismissalsRecordTheirMode is the #2408 widening: every mode
// reports its dismissals, each stamped with its own prefix — the command
// palette's esc must not be filed as a recent-files one.
func TestOtherPaletteDismissalsRecordTheirMode(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	m.palette.SetSize(100, 40)
	m.palette.Open(palette.Context{})
	tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.palette.IsOpen() {
		t.Fatal("esc must close the palette")
	}

	got := eventsOf(usageEvents(t, m), telemetry.TypePaletteDismiss)
	if len(got) != 1 {
		t.Fatalf("want one %s event, got %v", telemetry.TypePaletteDismiss, got)
	}
	if got[0].Data["mode"] == string(palette.RecentPrefix) {
		t.Fatalf("a command-mode dismissal was filed as recent-files: %v", got[0])
	}
	if got[0].Data["query_len"] != "0" {
		t.Fatalf("query_len = %q, want \"0\"", got[0].Data["query_len"])
	}
}

// TestRecentPickPersists checks that the preselection memory survives a
// restart: it is written on the pick, not on the next session save, because
// the palette has no model to snapshot the session from.
func TestRecentPickPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "recentpick.json")

	p := loadRecentPick(path)
	if key, _ := p.Get(); key != "" {
		t.Fatalf("a missing file must load empty, got %q", key)
	}
	p.Set("/proj/a.go", false)

	re := loadRecentPick(path)
	key, project := re.Get()
	if key != "/proj/a.go" || project {
		t.Fatalf("reloaded pick = (%q, %v), want the file pick", key, project)
	}
	re.Set("/other/project", true)
	if key, project := loadRecentPick(path).Get(); key != "/other/project" || !project {
		t.Fatalf("reloaded pick = (%q, %v), want the project pick", key, project)
	}

	// A malformed file degrades to "no preselection" rather than an error: a
	// ranking aid must never disrupt the session.
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if key, _ := loadRecentPick(path).Get(); key != "" {
		t.Fatalf("a corrupt file must load empty, got %q", key)
	}
	// A pathless store is inert but usable (the no-home-directory fallback).
	var inert *recentPick
	inert.Set("x", false)
	if key, _ := inert.Get(); key != "" {
		t.Fatalf("a nil store must stay empty, got %q", key)
	}
}

// TestRecentRankingSetting covers the settings gate's reader, including the
// documented default and the unknown-value fallback validation applies.
func TestRecentRankingSetting(t *testing.T) {
	if !recentRankingFrecency(nil) {
		t.Fatal("no config must default to frecency ranking")
	}
	for _, c := range []struct {
		value string
		want  bool
	}{{"frecency", true}, {"recency", false}, {" Recency ", false}, {"", true}} {
		cfg := &config.Config{}
		cfg.Palette.Recent.Ranking = c.value
		if got := recentRankingFrecency(cfg); got != c.want {
			t.Errorf("ranking %q = %v, want %v", c.value, got, c.want)
		}
	}
}

// TestRecentPaletteOpensLocked keeps the dialog's entry point honest: the
// command still opens the palette locked to the recent-files mode, so the
// #2399 additions ride on the same open.
func TestRecentPaletteOpensLocked(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	tm, _ := m.Update(ShowRecentFilesMsg{})
	m = tm.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("palette.recentFiles must open the palette")
	}
	// esc records the dismissal the telemetry test above consumes.
	m.palette.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	d, ok := m.palette.TakeDismissal()
	if !ok || d.Prefix != palette.RecentPrefix {
		t.Fatalf("dismissal = (%+v, %v), want a recent-files one", d, ok)
	}
}
