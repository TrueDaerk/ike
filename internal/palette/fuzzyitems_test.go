package palette

import "testing"

func TestFuzzyItemsFiltersAndFillsMatch(t *testing.T) {
	src := []string{"alpha", "beta", "gamma"}
	items := FuzzyItems("am", src, func(s string) string { return s }, func(s string) Item {
		return Item{Title: s, Detail: "d:" + s}
	})
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1: %+v", len(items), items)
	}
	if items[0].Title != "gamma" || items[0].Detail != "d:gamma" {
		t.Fatalf("build result not kept: %+v", items[0])
	}
	if len(items[0].Spans) == 0 {
		t.Fatalf("match positions not filled: %+v", items[0])
	}
	if items[0].Score == 0 {
		t.Fatalf("match score not filled: %+v", items[0])
	}
}

func TestFuzzyItemsEmptyQueryKeepsOrder(t *testing.T) {
	src := []string{"one", "two", "three"}
	items := FuzzyItems("", src, func(s string) string { return s }, func(s string) Item {
		return Item{Title: s}
	})
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
	for i, want := range src {
		if items[i].Title != want {
			t.Fatalf("item %d = %q, want %q", i, items[i].Title, want)
		}
	}
}

func TestFuzzyItemsMatchesOnTargetNotTitle(t *testing.T) {
	type row struct{ key, label string }
	src := []row{{"zzz", "visible"}, {"abc", "other"}}
	items := FuzzyItems("abc", src, func(r row) string { return r.key }, func(r row) Item {
		return Item{Title: r.label}
	})
	if len(items) != 1 || items[0].Title != "other" {
		t.Fatalf("target function ignored: %+v", items)
	}
}

func TestSortByScoreIsStableAndDescending(t *testing.T) {
	items := []Item{
		{Title: "a", Score: 1},
		{Title: "b", Score: 5},
		{Title: "c", Score: 1},
		{Title: "d", Score: 5},
	}
	SortByScore(items)
	got := ""
	for _, it := range items {
		got += it.Title
	}
	if got != "bdac" {
		t.Fatalf("order = %q, want %q", got, "bdac")
	}
}
