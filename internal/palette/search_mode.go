package palette

import "sort"

// SearchAllPrefix selects the search-everywhere mode inside the palette. The
// root model opens the palette locked to it (palette.searchEverywhere,
// cmd+shift+a / double-shift), so the rune never needs typing; it only has to
// be unique among modes.
const SearchAllPrefix = '*'

// searchAllPerKind caps how many results each composed source contributes, so
// one source (the 10k-file walk) cannot drown the other (a few dozen
// commands).
const searchAllPerKind = 8

// SearchAllMode is the search-everywhere mode (Roadmap 0230): one query ranked
// across commands and files, JetBrains' Search Everywhere palette-style. It
// composes the already-ranked result lists of the command and file modes
// rather than duplicating their ranking: each source's top rows (per-kind cap)
// interleave by match score, ties keeping commands first. Rows show their kind
// as the source mode's prefix glyph; command rows keep their binding chip,
// file rows their project-relative path. Activation dispatches whatever the
// underlying item carries (RunCommandMsg / OpenFileMsg).
type SearchAllMode struct {
	sources []Mode // composed modes, in kind-tiebreak order
}

// NewSearchAllMode builds the search-everywhere mode over the already-built
// command and file modes (in that order — earlier sources win score ties).
func NewSearchAllMode(sources ...Mode) *SearchAllMode {
	return &SearchAllMode{sources: sources}
}

// Prefix implements Mode.
func (s *SearchAllMode) Prefix() rune { return SearchAllPrefix }

// Placeholder implements Mode.
func (s *SearchAllMode) Placeholder() string { return "Search everywhere…" }

// Results implements Mode. Each source is queried and capped, then the union
// is ordered by score; the stable sort keeps earlier sources (commands) ahead
// on equal scores.
func (s *SearchAllMode) Results(query string, cx Context) []Item {
	var merged []Item
	for _, src := range s.sources {
		merged = append(merged, capped(src, query, cx)...)
	}
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].Score > merged[j].Score })
	return merged
}

// capped returns src's top rows (at most searchAllPerKind), each retitled with
// the source's prefix glyph so the row shows its kind; match spans shift with
// the added glyph so highlighting stays aligned.
func capped(src Mode, query string, cx Context) []Item {
	items := src.Results(query, cx)
	if len(items) > searchAllPerKind {
		items = items[:searchAllPerKind]
	}
	kind := string(src.Prefix()) + " "
	shift := len([]rune(kind))
	out := make([]Item, len(items))
	for i, it := range items {
		spans := make([]int, len(it.Spans))
		for j, p := range it.Spans {
			spans[j] = p + shift
		}
		it.Title = kind + it.Title
		it.Spans = spans
		out[i] = it
	}
	return out
}
