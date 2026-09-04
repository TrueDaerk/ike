package palette

import (
	"sort"

	"ike/internal/fuzzy"
)

// fuzzyitems.go holds the two building blocks every list-shaped Mode.Results
// repeats (#2463): fuzzy-filter a snapshot into rows, then rank the rows. The
// modes that need more than a plain score — a frecency boost, a per-source
// band, a secondary key — keep their own loop; these helpers exist for the
// plain case, so a picker over a slice is a two-liner instead of a
// hand-rolled scored struct.

// FuzzyItems fuzzy-matches query against target(v) for every element of src
// and returns one Item per surviving element, built by build. The match's
// rune positions and score are filled into the item afterwards, so build only
// describes the row (title, detail, activation message) and never repeats the
// highlight wiring. An empty query matches everything (fuzzy.Match's
// contract), so the result is src's own order until SortByScore ranks it.
func FuzzyItems[T any](query string, src []T, target func(T) string, build func(T) Item) []Item {
	var items []Item
	for _, v := range src {
		res, ok := fuzzy.Match(query, target(v))
		if !ok {
			continue
		}
		it := build(v)
		it.Spans = res.Positions
		it.Score = res.Score
		items = append(items, it)
	}
	return items
}

// SortByScore ranks items best-first by Score in place, keeping the input
// order among equal scores — so an empty query (every score equal) leaves the
// caller's snapshot order untouched.
func SortByScore(items []Item) {
	sort.SliceStable(items, func(i, j int) bool { return items[i].Score > items[j].Score })
}
