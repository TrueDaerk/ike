package terminal

import "testing"

// TestSearchMatchesMemoized (#2163): searchLine runs searchMatches per frame,
// and the scan walks the entire scrollback under gridMu. With the memo, an
// unchanged grid + query serves the cached set; a grid mutation or a query
// edit recomputes.
func TestSearchMatchesMemoized(t *testing.T) {
	m := searchModel(t)
	press(m, '/', "/")
	typeQuery(m, "5")
	first := m.searchMatches()
	if len(first) == 0 {
		t.Fatal("setup: query '5' should match seq output")
	}
	second := m.searchMatches()
	if len(second) != len(first) || &second[0] != &first[0] {
		t.Fatal("unchanged grid+query must serve the memoized match set")
	}
	// A grid mutation invalidates the memo.
	m.sess.version.Add(1)
	third := m.searchMatches()
	if &third[0] == &first[0] {
		t.Fatal("a grid version bump must recompute the match set")
	}
	// A query edit invalidates it too.
	typeQuery(m, "1") // "51"
	fourth := m.searchMatches()
	if len(fourth) >= len(third) {
		t.Fatalf("narrowed query should shrink matches: %d -> %d", len(third), len(fourth))
	}
}
