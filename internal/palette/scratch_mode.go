package palette

import (
	"sort"

	"ike/internal/fuzzy"
)

// ScratchPrefix selects the scratch-files mode (Roadmap 0280, #352). The root
// model opens the palette locked to it (scratch.list), so the rune never
// needs typing; it only has to be unique among modes.
const ScratchPrefix = '~'

// ScratchEntry is one row source for ScratchMode (#2057): a scratch's path,
// the title to render for it — the first non-empty content line, or a
// placeholder for an empty file, JetBrains' scratch-view convention — and its
// language for the Detail chip. The app builds these lazily over
// internal/scratch + internal/lang so this package stays free of both.
type ScratchEntry struct {
	Path  string
	Title string
	Lang  string
}

// ScratchMode lists the scratch store: entries newest-first (the store's
// order), fuzzy-filtered by title on the query, enter opens. The palette owns
// no store — the list func is injected by the root model over
// internal/scratch.Entries.
type ScratchMode struct {
	// list returns the scratch rows newest-first. Injected by the app.
	list func() []ScratchEntry
}

// NewScratchMode builds the scratch-files mode over the injected store.
func NewScratchMode(list func() []ScratchEntry) *ScratchMode {
	return &ScratchMode{list: list}
}

// Prefix implements Mode.
func (s *ScratchMode) Prefix() rune { return ScratchPrefix }

// Placeholder implements Mode.
func (s *ScratchMode) Placeholder() string { return "Scratch files…" }

// Results implements Mode. The query fuzzy-matches the title (the scratch's
// first content line, or its empty placeholder); equal scores keep the
// store's newest-first order. An empty store renders one inert hint row (nil
// Msg: enter just closes the palette).
func (s *ScratchMode) Results(query string, cx Context) []Item {
	if s.list == nil {
		return nil
	}
	type scored struct {
		item  Item
		score int
	}
	var out []scored
	for _, e := range s.list() {
		m, ok := fuzzy.Match(query, e.Title)
		if !ok {
			continue
		}
		out = append(out, scored{
			item:  Item{Title: e.Title, Detail: e.Lang, Spans: m.Positions, Score: m.Score, Msg: OpenFileMsg{Path: e.Path}},
			score: m.Score,
		})
	}
	if len(out) == 0 && query == "" {
		return []Item{{Title: "No scratch files yet — run \"New Scratch File\" first"}}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	items := make([]Item, len(out))
	for i, sc := range out {
		items[i] = sc.item
	}
	return items
}
