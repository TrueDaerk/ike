package project

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ike/internal/config"
	"ike/internal/fuzzy"
	"ike/internal/palette"
)

// picker.go is the project picker behind project.switch (Roadmap 0090, #12):
// a thin adapter producing palette items from the recent-projects history —
// newest first — plus a direct path-entry affordance. All fuzzy/overlay
// behaviour stays in the palette component (Roadmap 0070); this mode only
// ranks entries and names the msg the selection dispatches.

// PickedMsg is emitted when a picker item is activated: Path is the chosen
// project root — an existing history entry's absolute path, or the raw typed
// path (unvalidated; the switch orchestration (#3) validates before acting).
type PickedMsg struct{ Path string }

// PickerPrefix selects the picker mode inside the palette. The root model
// opens the palette locked to it, so the rune has no user-facing prefix story.
const PickerPrefix = '#'

// PickerMode is the palette Mode listing recent projects. history is
// injectable for tests; by default it reads the process-wide config.
type PickerMode struct {
	history func() []Entry
}

// NewPickerMode builds the picker mode. A nil history reads the
// recent-projects list from the live config on every open.
func NewPickerMode(history func() []Entry) *PickerMode {
	if history == nil {
		history = func() []Entry { return History(config.Get()) }
	}
	return &PickerMode{history: history}
}

// Prefix implements palette.Mode.
func (m *PickerMode) Prefix() rune { return PickerPrefix }

// Placeholder implements palette.Mode.
func (m *PickerMode) Placeholder() string { return "Switch project — recent name or path…" }

// Results implements palette.Mode: history entries fuzzy-matched on display
// name and path (an empty query lists all, newest first), followed by an
// "open this path" item for the raw query so a project outside the history is
// always reachable.
func (m *PickerMode) Results(query string, cx palette.Context) []palette.Item {
	type scored struct {
		entry Entry
		score int
		spans []int
	}
	var out []scored
	for _, e := range m.history() {
		if r, ok := fuzzy.Match(query, e.Name); ok {
			out = append(out, scored{entry: e, score: r.Score, spans: r.Positions})
			continue
		}
		// Fall back to the path so "code/ike" style queries hit too; spans
		// index the name (the rendered title), so a path match highlights nothing.
		if r, ok := fuzzy.Match(query, e.Path); ok {
			out = append(out, scored{entry: e, score: r.Score})
		}
	}
	// Stable on score only: equal scores keep the history's newest-first order.
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })

	items := make([]palette.Item, 0, len(out)+1)
	for _, s := range out {
		items = append(items, palette.Item{
			Title:  s.entry.Name,
			Detail: compactPath(s.entry.Path),
			Spans:  s.spans,
			Score:  s.score,
			Msg:    PickedMsg{Path: s.entry.Path},
		})
	}
	if q := strings.TrimSpace(query); q != "" {
		items = append(items, palette.Item{
			Title: "Open \"" + q + "\"…",
			Msg:   PickedMsg{Path: q},
		})
	}
	return items
}

// maxDetailWidth caps the rendered path chip: the palette row pins Detail to
// the right untruncated, so an over-long path would crowd out the title.
const maxDetailWidth = 40

// compactPath renders path for the picker's detail chip: the home prefix
// collapses to "~", and an over-long remainder keeps its head and tail around
// a "…" so the project's location stays recognisable.
func compactPath(path string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
			path = "~" + string(filepath.Separator) + rel
		} else if path == home {
			path = "~"
		}
	}
	r := []rune(path)
	if len(r) <= maxDetailWidth {
		return path
	}
	keep := (maxDetailWidth - 1) / 2
	return string(r[:keep]) + "…" + string(r[len(r)-keep:])
}
