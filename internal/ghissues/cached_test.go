package ghissues

// cached_test.go covers the persistent-cache seeding of the pane (#2108):
// SetCached renders the snapshot immediately and marks it stale, a real
// listing clears the marker, a late seed never overwrites fetched data, and
// the full/incremental split of the fetch requests — 'r' and every
// user-driven refetch demand a full resync, only the on-open fetch may take
// the incremental path.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
)

// cachedSeed is a small snapshot standing in for the persisted cache.
func cachedSeed() ([]forge.Issue, []forge.PR) {
	return []forge.Issue{
			{Number: 1, Title: "cached issue", State: "OPEN", URL: "https://e/1"},
		}, []forge.PR{
			{Number: 9, Title: "cached pr", State: "OPEN", URL: "https://e/pr/9"},
		}
}

func TestSetCachedRendersTheSnapshotMarkedStale(t *testing.T) {
	m := New(nil)
	m.SetSize(90, 12)
	m.SetCached(cachedSeed())
	if !m.Loaded() || !m.Cached() {
		t.Fatal("a seeded pane is loaded and marked cached")
	}
	view := m.View()
	if !strings.Contains(view, "cached issue") {
		t.Fatal("the cached listing must render immediately")
	}
	if !strings.Contains(view, "cached · updating…") {
		t.Fatal("the cached listing must be marked stale")
	}
}

func TestFreshListingClearsTheCachedMarker(t *testing.T) {
	m := New(nil)
	m.SetSize(90, 12)
	m.SetCached(cachedSeed())
	m.SetResult(forge.IssuesMsg{State: forge.IssuesOpen,
		Issues: []forge.Issue{{Number: 2, Title: "fresh issue", State: "OPEN"}}})
	if m.Cached() {
		t.Fatal("a real listing must clear the cached marker")
	}
	view := m.View()
	if !strings.Contains(view, "fresh issue") || strings.Contains(view, "cached · updating…") {
		t.Fatal("the fresh listing replaces the snapshot and the stale note")
	}
}

func TestPollResultClearsTheCachedMarker(t *testing.T) {
	// A restored pane is refreshed by the background poll, not a foreground
	// fetch — the poll's listing must count as "real" too.
	m := New(nil)
	m.SetSize(90, 12)
	m.SetCached(cachedSeed())
	m.SetResult(forge.IssuesMsg{State: forge.IssuesOpen, Poll: true,
		Issues: []forge.Issue{{Number: 3, Title: "polled issue", State: "OPEN"}}})
	if m.Cached() {
		t.Fatal("a poll listing must clear the cached marker")
	}
}

func TestLateSeedNeverOverwritesFetchedData(t *testing.T) {
	m := New(nil)
	m.SetSize(90, 12)
	m.SetResult(forge.IssuesMsg{State: forge.IssuesOpen,
		Issues: []forge.Issue{{Number: 2, Title: "fresh issue", State: "OPEN"}}})
	m.SetCached(cachedSeed())
	if m.Cached() {
		t.Fatal("a seed landing after the fetch must be dropped")
	}
	if view := m.View(); strings.Contains(view, "cached issue") {
		t.Fatal("the fetched listing must survive a late cache seed")
	}
}

func TestOnlyTheOnOpenFetchMayBeIncremental(t *testing.T) {
	m := New(nil)
	m.SetSize(90, 12)
	var fulls []bool
	m.SetRefresh(func(_ forge.IssueState, _ int, full bool) tea.Cmd {
		fulls = append(fulls, full)
		return func() tea.Msg { return nil }
	})
	m.Refresh()        // on-open: may merge incrementally
	m.Update(key("r")) // manual refresh: full resync, always
	if len(fulls) != 2 || fulls[0] || !fulls[1] {
		t.Fatalf("full flags = %v, want [false true] (open incremental, r full)", fulls)
	}
}
