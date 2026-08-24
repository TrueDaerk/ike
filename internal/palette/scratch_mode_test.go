package palette

import "testing"

func TestScratchModeListsNewestFirst(t *testing.T) {
	m := NewScratchMode(func() []ScratchEntry {
		return []ScratchEntry{
			{Path: "/s/scratch-3.py", Title: "def solve():", Lang: "Python"},
			{Path: "/s/scratch-1.txt", Title: "Empty scratch", Lang: "Plain Text"},
			{Path: "/s/scratch-2.go", Title: "package main", Lang: "GO"},
		}
	})
	items := m.Results("", Context{})
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d", len(items))
	}
	// Empty query keeps the store's newest-first order; titles are the
	// injected first-content-line titles, Detail carries the language.
	wantTitles := []string{"def solve():", "Empty scratch", "package main"}
	wantLangs := []string{"Python", "Plain Text", "GO"}
	for i := range wantTitles {
		if items[i].Title != wantTitles[i] {
			t.Fatalf("item %d title = %q, want %q", i, items[i].Title, wantTitles[i])
		}
		if items[i].Detail != wantLangs[i] {
			t.Fatalf("item %d detail = %q, want %q", i, items[i].Detail, wantLangs[i])
		}
	}
	if msg, ok := items[0].Msg.(OpenFileMsg); !ok || msg.Path != "/s/scratch-3.py" {
		t.Fatalf("enter must open the scratch, msg = %#v", items[0].Msg)
	}
}

func TestScratchModeFuzzyFilter(t *testing.T) {
	m := NewScratchMode(func() []ScratchEntry {
		return []ScratchEntry{
			{Path: "/s/scratch-1.py", Title: "import json", Lang: "Python"},
			{Path: "/s/scratch-2.go", Title: "package main", Lang: "GO"},
			{Path: "/s/notes.txt", Title: "grocery list", Lang: "Plain Text"},
		}
	})
	// The query fuzzy-matches the title — the scratch's first content line —
	// not the file name (#2057).
	items := m.Results("json", Context{})
	if len(items) != 1 || items[0].Title != "import json" {
		t.Fatalf("filter 'json' = %v", items)
	}
	// A non-matching query yields no rows (and no inert hint).
	if items := m.Results("zzz", Context{}); len(items) != 0 {
		t.Fatalf("non-matching query must be empty, got %v", items)
	}
}

func TestScratchModeEmptyStoreHint(t *testing.T) {
	m := NewScratchMode(func() []ScratchEntry { return nil })
	items := m.Results("", Context{})
	if len(items) != 1 || items[0].Msg != nil {
		t.Fatalf("empty store must render one inert hint row, got %v", items)
	}
}
