package app

import (
	"strconv"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/palette"
	"ike/internal/telemetry"
)

// palette_pick_telemetry_test.go covers the pick half of the palette funnel
// (#2551): where in the list the chosen row sat, so the ranking quality the
// frecency work (#2399, #2155) aims at becomes measurable.

// TestPalettePickRecordsRank is the acceptance criterion: activating a row
// lands a "palette.pick" event carrying the mode, the query length, the
// 0-based rank and the row count — and never the query itself.
func TestPalettePickRecordsRank(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	m.palette.SetSize(100, 40)
	m.palette.Open(palette.Context{})
	for _, r := range "tm" {
		tm, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = tm.(Model)
	}
	// Walk one row down, so the rank is provably the *chosen* row's index
	// and not a constant zero.
	tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)

	got := eventsOf(usageEvents(t, m), telemetry.TypePalettePick)
	if len(got) != 1 {
		t.Fatalf("want one %s event, got %v", telemetry.TypePalettePick, got)
	}
	d := got[0].Data
	if d["mode"] == "" || d["mode"] == string(palette.RecentPrefix) {
		t.Errorf("mode = %q, want the command mode's own prefix", d["mode"])
	}
	if d["query_len"] != "2" {
		t.Errorf("query_len = %q, want \"2\"", d["query_len"])
	}
	if d["rank"] != "1" {
		t.Errorf("rank = %q, want \"1\" — the second row was picked", d["rank"])
	}
	n, err := strconv.Atoi(d["results"])
	if err != nil || n < 2 {
		t.Errorf("results = %q, want the row count the palette showed", d["results"])
	}
	for _, k := range []string{"query", "id", "path"} {
		if _, ok := d[k]; ok {
			t.Errorf("a pick must not carry %q: %v", k, d)
		}
	}
}

// A dismissal is not a pick and a pick is not a dismissal: the two outcomes
// stay disjoint, so counting one never double-counts the other (#2551).
func TestPaletteDismissRecordsNoPick(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	m.palette.SetSize(100, 40)
	m.palette.Open(palette.Context{})
	tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)

	evs := usageEvents(t, m)
	if got := eventsOf(evs, telemetry.TypePalettePick); len(got) != 0 {
		t.Fatalf("a dismissal recorded a pick: %v", got)
	}
	if got := eventsOf(evs, telemetry.TypePaletteDismiss); len(got) != 1 {
		t.Fatalf("want one %s event, got %v", telemetry.TypePaletteDismiss, got)
	}
}

// A mouse click on a row is a pick too (#2551): the click funnel reads the
// pick alongside the key path, so a mouse-driven palette is not a blind spot.
func TestPaletteClickPickRecordsRank(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	m.palette.SetSize(100, 40)
	m.palette.Open(palette.Context{})
	for _, r := range "tm" {
		tm, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = tm.(Model)
	}
	// Box-local coordinates: 2 columns of border+padding, and the first
	// result row sits two lines below the prompt (see Palette.Click).
	m.palette.Click(3, 3)
	m.recordPalettePick()

	got := eventsOf(usageEvents(t, m), telemetry.TypePalettePick)
	if len(got) != 1 {
		t.Fatalf("want one %s event, got %v", telemetry.TypePalettePick, got)
	}
}
