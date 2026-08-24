package palette

import (
	"sort"

	tea "charm.land/bubbletea/v2"
)

// SearchAllPrefix selects the search-everywhere mode inside the palette. The
// root model opens the palette locked to it (palette.searchEverywhere,
// cmd+shift+a / double-shift), so the rune never needs typing; it only has to
// be unique among modes.
const SearchAllPrefix = '*'

// searchAllPerKind caps how many results each composed source contributes, so
// one source (the 10k-file walk) cannot drown the other (a few dozen
// commands).
const searchAllPerKind = 8

// searchAllScoreBand quantises fuzzy scores before the source-priority tier
// applies (#1421): two results whose scores differ by less than one
// word-boundary bonus (internal/fuzzy's strongest per-rune reward) count as
// comparable matches, and within a band the earlier source wins — command
// before file before symbol. A clearly stronger match (a full band apart)
// still outranks a higher-priority source.
const searchAllScoreBand = 16

// SearchAllMode is the search-everywhere mode (Roadmap 0230): one query ranked
// across commands and files, JetBrains' Search Everywhere palette-style. It
// composes the already-ranked result lists of the command and file modes
// rather than duplicating their ranking: each source's top rows (per-kind cap)
// interleave by banded match score with an explicit source-priority tier
// (#1421) — within a score band the earlier source wins, so a command beats a
// comparably-matched file, which beats a comparably-matched symbol. Rows show
// their kind as the source mode's prefix glyph; command rows keep their
// binding chip, file rows their project-relative path. Activation dispatches
// whatever the underlying item carries (RunCommandMsg / OpenFileMsg). A query
// led by a composed source's own prefix rune scopes the list to that source
// (#1417) — see scoped.
type SearchAllMode struct {
	sources []Mode // composed modes, in kind-tiebreak order
	recents Mode   // optional; listed first on an empty query (#263)
}

// NewSearchAllMode builds the search-everywhere mode over the already-built
// command and file modes (in that order — the source order is the priority
// tier: earlier sources win within a score band).
func NewSearchAllMode(sources ...Mode) *SearchAllMode {
	return &SearchAllMode{sources: sources}
}

// SetRecents installs the recent-files mode shown while the query is empty:
// JetBrains' Search Everywhere opens on recents, not on a directory walk
// (0082 sheet 17, #263).
func (s *SearchAllMode) SetRecents(m Mode) { s.recents = m }

// Prefix implements Mode.
func (s *SearchAllMode) Prefix() rune { return SearchAllPrefix }

// Placeholder implements Mode.
func (s *SearchAllMode) Placeholder() string { return "Search everywhere…" }

// PanelTitle implements PanelMode (#2055): the search-everywhere hits tip into
// the Find panel under the query that produced them.
func (s *SearchAllMode) PanelTitle(query string) string {
	if query == "" {
		return "Find"
	}
	return "Find: " + query
}

// Results implements Mode. Each source is queried and capped, then the union
// is ordered by (score band, source tier, score): scores quantised to
// searchAllScoreBand rank first, the source's position in s.sources breaks
// band ties (command > file > symbol), and the raw score orders within a
// source's band. The stable sort keeps each source's own ranking — usage
// boost (#773), MRU order — for fully equal keys. An empty query lists the
// recent files first (most recent first, active file excluded) followed by
// the first source's listing; with no MRU history it falls through to the
// plain source listing. A prefix-scoped query returns that source's own ranked
// list, uncapped and unmerged.
func (s *SearchAllMode) Results(query string, cx Context) []Item {
	if src, body, ok := s.scoped(query); ok {
		return glyphed(src, src.Results(body, cx))
	}
	if query == "" && s.recents != nil {
		if rec := capped(s.recents, "", cx); len(rec) > 0 {
			if len(s.sources) > 0 {
				rec = append(rec, capped(s.sources[0], "", cx)...)
			}
			return rec
		}
	}
	type tiered struct {
		item Item
		tier int
	}
	var merged []tiered
	for tier, src := range s.sources {
		for _, it := range capped(src, query, cx) {
			merged = append(merged, tiered{item: it, tier: tier})
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		bi, bj := merged[i].item.Score/searchAllScoreBand, merged[j].item.Score/searchAllScoreBand
		if bi != bj {
			return bi > bj
		}
		if merged[i].tier != merged[j].tier {
			return merged[i].tier < merged[j].tier
		}
		return merged[i].item.Score > merged[j].item.Score
	})
	out := make([]Item, len(merged))
	for i, t := range merged {
		out[i] = t.item
	}
	return out
}

// scoped resolves a prefix-scoped query (#1417): a query whose leading rune is
// one composed source's prefix (":" command, "@" file, "$" symbol) restricts
// the result list to that source alone and matches on the remaining body.
// Search Everywhere is a locked mode, so the palette core never strips the
// prefix itself — the users typing it know it from the unlocked palette and
// expect the list to narrow, not to fuzzy-match the raw prefixed string. A
// leading rune that names no source is ordinary query text.
func (s *SearchAllMode) scoped(query string) (Mode, string, bool) {
	r := []rune(query)
	if len(r) == 0 {
		return nil, query, false
	}
	for _, src := range s.sources {
		if src.Prefix() == r[0] {
			return src, string(r[1:]), true
		}
	}
	return nil, query, false
}

// capped returns src's top rows (at most searchAllPerKind), glyphed with the
// source's prefix. The cap only exists to keep one source from drowning the
// others in the composed list, so prefix-scoped results skip it and call
// glyphed directly.
func capped(src Mode, query string, cx Context) []Item {
	items := src.Results(query, cx)
	if len(items) > searchAllPerKind {
		items = items[:searchAllPerKind]
	}
	return glyphed(src, items)
}

// glyphed retitles items with their source's prefix glyph so each row shows its
// kind; match spans shift with the added glyph so highlighting stays aligned.
func glyphed(src Mode, items []Item) []Item {
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

// QueryChanged implements LiveMode by forwarding to every composed live
// source (#295), so an asynchronous source — the workspace-symbol mode —
// re-queries inside search everywhere exactly as it does standalone. A
// prefix-scoped query (#1417) forwards the stripped body to that one source
// only; the sources it filters out need no re-query.
func (s *SearchAllMode) QueryChanged(query string, cx Context) tea.Cmd {
	if src, body, ok := s.scoped(query); ok {
		if live, isLive := src.(LiveMode); isLive {
			return live.QueryChanged(body, cx)
		}
		return nil
	}
	var cmds []tea.Cmd
	for _, src := range s.sources {
		if live, ok := src.(LiveMode); ok {
			cmds = append(cmds, live.QueryChanged(query, cx))
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}
