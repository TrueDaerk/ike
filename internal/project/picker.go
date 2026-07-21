package project

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ike/internal/config"
	"ike/internal/fuzzy"
	"ike/internal/palette"
	"ike/internal/pathcomplete"
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
	// open reports whether a history entry's workspace is currently loaded
	// in memory (#820); such entries carry the "●" badge and a close aux
	// action. Nil marks nothing.
	open func(path string) bool
}

// SetOpen installs the in-memory check (#820); the app injects the workspace
// manager's Peek so the picker package stays workspace-agnostic.
func (m *PickerMode) SetOpen(open func(path string) bool) { m.open = open }

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
		it := palette.Item{
			Title:  s.entry.Name,
			Detail: CompactPath(s.entry.Path),
			Spans:  s.spans,
			Score:  s.score,
			Msg:    PickedMsg{Path: s.entry.Path},
		}
		if m.open != nil && m.open(s.entry.Path) {
			// Loaded in memory (#820): badge + close-in-place aux action.
			it.Badge = "●"
			it.Aux = CloseWorkspaceMsg{Path: s.entry.Path}
		}
		items = append(items, it)
	}
	if q := strings.TrimSpace(query); q != "" {
		// A path-shaped query browses the filesystem (#542): matching
		// directories become selectable items ahead of the raw fallback.
		if pathish(q) {
			for _, c := range pathcomplete.Dirs(q).Candidates {
				items = append(items, palette.Item{
					Title: "Open " + c,
					Msg:   PickedMsg{Path: c},
				})
			}
		}
		items = append(items, palette.Item{
			Title: "Open \"" + q + "\"…",
			Msg:   PickedMsg{Path: q},
		})
	}
	return items
}

// Complete implements palette.Completer (#542): tab extends a path-shaped
// query to the longest unambiguous directory prefix; anything else is inert.
func (m *PickerMode) Complete(query string) string {
	q := strings.TrimSpace(query)
	if !pathish(q) {
		return query
	}
	return pathcomplete.Dirs(q).Completed
}

// pathish reports a query meant as a filesystem path rather than a fuzzy
// history search: absolute, home-relative or explicitly dot-relative.
func pathish(q string) bool {
	return strings.HasPrefix(q, string(filepath.Separator)) ||
		q == "~" || strings.HasPrefix(q, "~"+string(filepath.Separator)) ||
		strings.HasPrefix(q, "."+string(filepath.Separator)) ||
		strings.HasPrefix(q, ".."+string(filepath.Separator))
}

// maxDetailWidth caps the rendered path chip: the palette row pins Detail to
// the right untruncated, so an over-long path would crowd out the title.
const maxDetailWidth = 40

// CompactPath renders path for constrained chrome — the picker's detail chip
// and the unsaved-changes prompt: the home prefix collapses to "~", and an
// over-long remainder keeps its head and tail around a "…" so the project's
// location stays recognisable. Bounding the width matters beyond looks: the
// floating shell drops a box wider than the terminal outright.
func CompactPath(path string) string {
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
