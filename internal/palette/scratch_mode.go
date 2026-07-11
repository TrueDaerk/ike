package palette

import (
	"path/filepath"
	"sort"

	"ike/internal/fuzzy"
)

// ScratchPrefix selects the scratch-files mode (Roadmap 0280, #352). The root
// model opens the palette locked to it (scratch.list), so the rune never
// needs typing; it only has to be unique among modes.
const ScratchPrefix = '~'

// ScratchMode lists the scratch store: file names newest-first (the store's
// order), fuzzy-filtered by the query, enter opens. The palette owns no store
// — the list func is injected by the root model over internal/scratch.List.
type ScratchMode struct {
	// list returns the scratch paths newest-first. Injected by the app.
	list func() []string
}

// NewScratchMode builds the scratch-files mode over the injected store.
func NewScratchMode(list func() []string) *ScratchMode {
	return &ScratchMode{list: list}
}

// Prefix implements Mode.
func (s *ScratchMode) Prefix() rune { return ScratchPrefix }

// Placeholder implements Mode.
func (s *ScratchMode) Placeholder() string { return "Scratch files…" }

// Results implements Mode. The query fuzzy-matches the file name; equal
// scores keep the store's newest-first order. An empty store renders one
// inert hint row (nil Msg: enter just closes the palette).
func (s *ScratchMode) Results(query string, cx Context) []Item {
	if s.list == nil {
		return nil
	}
	type scored struct {
		item  Item
		score int
	}
	var out []scored
	for _, p := range s.list() {
		title := filepath.Base(p)
		m, ok := fuzzy.Match(query, title)
		if !ok {
			continue
		}
		out = append(out, scored{
			item:  Item{Title: title, Spans: m.Positions, Score: m.Score, Msg: OpenFileMsg{Path: p}},
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
