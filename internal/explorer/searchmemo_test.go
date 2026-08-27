package explorer

// searchmemo_test.go guards the speed search's match memo (#2187): searchLine
// renders its counter from searchMatches, so the scan over every flattened
// row ran per frame while the field was open — tens of thousands of rows on a
// monorepo. The memo is keyed by query and the row-set epoch.

import "testing"

func TestSearchMatchesMemoized(t *testing.T) {
	m := searchModel(t, "alpha.go", "album.go", "beta.go")
	m = typeText(m, "/al")
	first := m.searchMatches()
	if len(first) != 2 {
		t.Fatalf("setup: 'al' should match two rows, got %v", first)
	}
	second := m.searchMatches()
	if &second[0] != &first[0] {
		t.Fatal("an unchanged query and row set must serve the memoized matches")
	}
	// A query edit recomputes.
	m = typeText(m, "b")
	third := m.searchMatches()
	if len(third) != 1 || rowName(m, third[0]) != "album.go" {
		t.Fatalf("'alb' should match album.go alone, got %v", third)
	}
	// So does a row-set change: rebuild bumps the epoch.
	m.rebuild()
	fourth := m.searchMatches()
	if len(fourth) != 1 {
		t.Fatalf("matches after a rebuild = %v, want one", fourth)
	}
	if &fourth[0] == &third[0] {
		t.Fatal("a rebuilt row set must invalidate the memo")
	}
}

// TestSearchMatchesFollowRowChanges (#2187): the memo must never outlive the
// rows it indexes — a collapse that drops matching rows has to be reflected.
func TestSearchMatchesFollowRowChanges(t *testing.T) {
	m := searchModel(t, "alpha.go", "beta.go")
	m = typeText(m, "/a")
	if len(m.searchMatches()) == 0 {
		t.Fatal("setup: 'a' should match at least one row")
	}
	// Drop every row but the root and rebuild: the stale memo would still
	// point at indices that no longer exist.
	m.root.expanded = false
	m.rebuild()
	for _, idx := range m.searchMatches() {
		if idx >= len(m.rows) {
			t.Fatalf("stale match index %d past %d rows", idx, len(m.rows))
		}
	}
}
