package ghissues

import (
	"reflect"
	"strings"
	"testing"
)

// The default filter seeds all three dimensions of a freshly opened pane.
func TestDefaultFilterSeedsThePane(t *testing.T) {
	m := filled(t)
	m.Configure(fakeConfig{"issues.default_filter": `state:all label:feature match:explorer`})
	if m.Filter() != "explorer" {
		t.Fatalf("match = %q, want explorer", m.Filter())
	}
	if got := m.LabelFilter(); !reflect.DeepEqual(got, []string{"feature"}) {
		t.Fatalf("labels = %v, want [feature]", got)
	}
	if m.StateFilter() != FilterAll {
		t.Fatalf("state = %v, want all", m.StateFilter())
	}
	// Only issue 3 carries "feature" and matches "explorer".
	if len(m.rows) != 1 || m.issues[m.rows[0].idx].Number != 3 {
		t.Fatalf("the seeded filter must narrow the listing, got %d rows", len(m.rows))
	}
}

// A filter changed by hand wins over the configured default for the rest of
// the session — the seed rule issues.default_tab/default_sort already follow.
func TestDefaultFilterYieldsToAHandChange(t *testing.T) {
	m := filled(t)
	cfg := fakeConfig{"issues.default_filter": "match:explorer"}
	m.Configure(cfg)
	m.Update(key("f"))
	for _, r := range "markdown" {
		m.Update(key(string(r)))
	}
	m.Update(key("enter"))
	// The seed leaves the cursor behind its own text, so typing appends.
	before := m.Filter()
	if before != "explorermarkdown" {
		t.Fatalf("the hand edit did not reach the match input, got %q", before)
	}
	m.Configure(cfg) // a live config reload
	if m.Filter() != before {
		t.Fatalf("a reload must not re-seed a hand-filtered pane (%q became %q)", before, m.Filter())
	}
}

// Clearing the filter by hand also counts as a hand change: the pane must not
// silently re-narrow itself on the next reload.
func TestClearingTheFilterYieldsTheSeed(t *testing.T) {
	m := filled(t)
	cfg := fakeConfig{"issues.default_filter": "match:explorer"}
	m.Configure(cfg)
	m.Update(key("esc")) // peels the match text
	if m.Filter() != "" {
		t.Fatalf("esc must clear the seeded match text, got %q", m.Filter())
	}
	m.Configure(cfg)
	if m.Filter() != "" {
		t.Fatalf("a reload must not re-seed a filter the user cleared, got %q", m.Filter())
	}
}

// A broken expression leaves the pane unfiltered rather than half filtered.
func TestBrokenDefaultFilterIsIgnored(t *testing.T) {
	m := filled(t)
	m.Configure(fakeConfig{"issues.default_filter": "label:bug author:me"})
	if m.Filter() != "" || len(m.LabelFilter()) != 0 {
		t.Fatalf("a broken expression must not partially apply, got %q / %v", m.Filter(), m.LabelFilter())
	}
}

func TestSavedFiltersApplyFromTheOverlay(t *testing.T) {
	m := filled(t)
	m.Configure(fakeConfig{"issues.saved_filters": "triage=label:bug,stale=state:all match:markdown"})
	if got := m.SavedFilters(); !reflect.DeepEqual(got, []string{"triage", "stale"}) {
		t.Fatalf("saved filters = %v, want [triage stale]", got)
	}
	if m.SavedFilter() != savedNone {
		t.Fatalf("a fresh pane must sit on %s, got %q", savedNone, m.SavedFilter())
	}
	m.Update(key("f"))
	// match, state, sort, group, saved — the saved row is the last fixed one.
	saved := m.fovFixedRows() - 1
	if m.fovKind(saved) != fovSaved {
		t.Fatalf("row %d is not the saved row", saved)
	}
	for m.ovCursor != saved {
		m.Update(key("down"))
	}
	m.Update(key("space"))
	if m.SavedFilter() != "triage" {
		t.Fatalf("the first cycle must apply triage, got %q", m.SavedFilter())
	}
	if got := m.LabelFilter(); !reflect.DeepEqual(got, []string{"bug"}) {
		t.Fatalf("triage must select the bug label, got %v", got)
	}
	m.Update(key("space"))
	if m.SavedFilter() != "stale" {
		t.Fatalf("the second cycle must apply stale, got %q", m.SavedFilter())
	}
	// Switching filters replaces every dimension — triage's label is gone.
	if len(m.LabelFilter()) != 0 {
		t.Fatalf("a saved filter must replace the previous one, labels = %v", m.LabelFilter())
	}
	if m.Filter() != "markdown" || m.StateFilter() != FilterAll {
		t.Fatalf("stale must apply match and state, got %q / %v", m.Filter(), m.StateFilter())
	}
	// And back round to (none), which clears the filter again.
	m.Update(key("space"))
	if m.SavedFilter() != savedNone || m.Filter() != "" || m.StateFilter() != FilterOpen {
		t.Fatalf("(none) must clear the filter, got %q / %q / %v",
			m.SavedFilter(), m.Filter(), m.StateFilter())
	}
}

// The saved row names what the filter *is*, not what was last picked: a hand
// edit after picking one drops the row back to "(none)".
func TestSavedRowFollowsTheLiveFilter(t *testing.T) {
	m := filled(t)
	m.Configure(fakeConfig{"issues.saved_filters": "triage=label:bug"})
	if cmd := m.applySaved(1); cmd != nil {
		t.Fatal("applying a client-side filter must not refetch")
	}
	if m.SavedFilter() != "triage" {
		t.Fatalf("after applying triage the row reads %q", m.SavedFilter())
	}
	m.Update(key("f"))
	m.Update(key("x")) // types into the match input
	m.Update(key("enter"))
	if m.SavedFilter() != savedNone {
		t.Fatalf("a hand edit must leave the row at %s, got %q", savedNone, m.SavedFilter())
	}
	// A default filter that happens to be a saved one reads as that one.
	m2 := filled(t)
	m2.Configure(fakeConfig{
		"issues.saved_filters":  "triage=label:bug",
		"issues.default_filter": "label:bug",
	})
	if m2.SavedFilter() != "triage" {
		t.Fatalf("the seeded filter is triage, row reads %q", m2.SavedFilter())
	}
}

// Without configured saved filters the overlay has no saved row at all.
func TestNoSavedRowWithoutSavedFilters(t *testing.T) {
	m := filled(t)
	m.Update(key("f"))
	for i := 0; i < m.fovFixedRows(); i++ {
		if m.fovKind(i) == fovSaved {
			t.Fatal("the saved row must stay hidden while no saved filter is configured")
		}
	}
	if m.fovFixedRows() != 4 {
		t.Fatalf("the issue tab has 4 fixed rows without saved filters, got %d", m.fovFixedRows())
	}
}

// The saved row renders its current pick, so cycling is never blind.
func TestSavedRowRenders(t *testing.T) {
	m := filled(t)
	m.Configure(fakeConfig{"issues.saved_filters": "triage=label:bug"})
	m.Update(key("f"))
	if v := m.View(); !strings.Contains(v, "saved: ‹ "+savedNone+" ›") {
		t.Fatalf("the saved row must show its pick, view = %q", v)
	}
}

// A broken or duplicate entry is skipped rather than taking the pane down.
func TestBrokenSavedFiltersAreSkipped(t *testing.T) {
	m := filled(t)
	m.Configure(fakeConfig{"issues.saved_filters": "broken,triage=label:bug,=label:x"})
	if got := m.SavedFilters(); !reflect.DeepEqual(got, []string{"triage"}) {
		t.Fatalf("only the parsable entry may survive, got %v", got)
	}
}
