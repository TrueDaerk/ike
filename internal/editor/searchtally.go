package editor

import (
	"strconv"

	"ike/internal/editor/search"
	"ike/internal/largefile"
)

// searchtally.go memoizes the search match counter (#2145): the command line
// and the status line both show "current/total" for the highlighted query, and
// both are re-rendered on every frame — a fresh buffer scan per frame (and per
// cursor move, since search highlights stay armed after the search line closes)
// would cost a full pass over the document for nothing.
//
// The scan is bounded twice over (search.ScanMatches): at most
// search.MaxMatches matches and search.MaxScanLines lines, so even a huge
// buffer costs a bounded pass per keystroke and the counter degrades to
// "999+". On top of that the capped match list is cached per
// (document version, query identity), so cursor motion — n/N and ordinary
// j/k alike — only re-derives the index from the cached list.
//
// The store is a pointer for the same reason lineCacheStore is: the many value
// copies of a Model that share one view share one cache, while a genuinely
// separate view gets its own.

type searchTallyKey struct {
	doc   int    // document version
	query string // search.Query.ID()
}

type searchTallyStore struct {
	key    searchTallyKey
	spans  []search.Span
	capped bool
	valid  bool
}

func newSearchTally() *searchTallyStore { return &searchTallyStore{} }

// searchTally returns the tally for the query the view highlights, and whether
// one is live at all (a search line with a pattern, or armed highlights).
func (m Model) searchTally() (search.Tally, bool) {
	q, ok := m.searchHLQuery()
	if !ok {
		return search.Tally{}, false
	}
	if m.FeatureOff(largefile.FeatureSearch) {
		// Large-file degradation (#2159): even the capped scan is a real pass
		// per edit — the counter hides and the status badge says why. Viewport
		// match highlighting is per-line and stays.
		return search.Tally{}, false
	}
	key := searchTallyKey{doc: m.docVersion, query: q.ID()}
	if m.tally != nil && m.tally.valid && m.tally.key == key {
		return search.Tally{
			Index:  search.IndexOf(m.tally.spans, m.cursor),
			Total:  len(m.tally.spans),
			Capped: m.tally.capped,
		}, true
	}
	spans, capped := q.ScanMatches(m.buf, search.MaxMatches, search.MaxScanLines)
	if m.tally != nil {
		m.tally.key, m.tally.spans, m.tally.capped, m.tally.valid = key, spans, capped, true
	}
	return search.Tally{Index: search.IndexOf(spans, m.cursor), Total: len(spans), Capped: capped}, true
}

// SearchCounter renders the match tally for the status line (#2145) —
// "3/17", "12/999+" for a capped count, or "no matches". It is empty whenever
// no search highlight is live, which is what hides the status segment.
func (m Model) SearchCounter() string {
	t, ok := m.searchTally()
	if !ok {
		return ""
	}
	if t.Total == 0 {
		return "no matches"
	}
	total := strconv.Itoa(t.Total)
	if t.Capped {
		total += "+"
	}
	return strconv.Itoa(t.Index) + "/" + total
}
