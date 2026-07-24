package palette

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ike/internal/fuzzy"
	"ike/internal/ui"
)

// RecentPrefix selects the recent-files mode inside the palette. The root
// model opens the palette locked to it (palette.recentFiles, cmd+e), so the
// rune never needs typing; it only has to be unique among modes.
const RecentPrefix = '%'

// SideMode is an optional Mode extension (#778): a locked mode exposing a
// secondary left column next to the main result list. The palette renders
// both side by side; tab (or left/right on an empty query) moves the column
// focus, up/down navigates the focused column, enter activates its
// selection.
type SideMode interface {
	Mode
	// SideTitle is the left column's dim heading.
	SideTitle() string
	// SideResults lists the left column's items for the query.
	SideResults(query string, cx Context) []Item
}

// RecentMode is the recent-files mode (Roadmap 0230): the most-recently-used
// file list, JetBrains' Recent Files popup palette-style. The palette owns no
// MRU store of its own — the list func is injected by the root model, which
// touches it on every file open and tab activation. With an empty query the
// items keep MRU order (most recent first) with the currently active file
// excluded, so opening the mode and pressing enter jumps to the *previous*
// file; a query fuzzy-matches the project-relative path, ties keep MRU order.
type RecentMode struct {
	// list returns the MRU entries, most recent first. Injected by the app.
	list func() []RecentEntry
	// exists filters vanished files out of the listing. Injectable for tests;
	// defaults to an on-disk stat.
	exists func(path string) bool
	// projects supplies the Recent Projects column (#778): items already
	// carrying their activation Msg (the app injects project.PickedMsg
	// values, so the palette stays project-agnostic). Nil hides the column.
	projects func() []Item
	// now overrides the clock for the last-opened column (#1113); tests only.
	now func() time.Time
}

// RecentEntry is one injected MRU record (#1113): the file path and when it
// was last opened. A zero LastOpened (legacy entry) renders no time.
type RecentEntry struct {
	Path       string
	LastOpened time.Time
}

// RemoveRecentFileMsg is the aux action of a recent-files row (#1113,
// mirroring the project picker's #842 prune): shift+delete or a click on the
// "✕" zone asks the app to drop the entry from the MRU history.
type RemoveRecentFileMsg struct{ Path string }

// NewRecentMode builds the recent-files mode over the injected MRU source.
func NewRecentMode(list func() []RecentEntry) *RecentMode {
	return &RecentMode{list: list, exists: fileExists}
}

// clock returns the injectable now source, defaulting to the wall clock.
func (r *RecentMode) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// fileExists is the default existence filter: a stat-able non-directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// SetProjects installs the recent-projects source for the left column
// (#778); nil keeps the dialog single-column.
func (r *RecentMode) SetProjects(list func() []Item) { r.projects = list }

// SideTitle implements SideMode.
func (r *RecentMode) SideTitle() string { return "Recent Projects" }

// SideResults implements SideMode: the injected recent projects, filtered by
// the query (fuzzy on the title, ties keeping recency order).
func (r *RecentMode) SideResults(query string, _ Context) []Item {
	if r.projects == nil {
		return nil
	}
	type scored struct {
		item  Item
		score int
	}
	var out []scored
	for _, it := range r.projects() {
		m, ok := fuzzy.Match(query, it.Title)
		if !ok {
			continue
		}
		it.Spans = m.Positions
		it.Score = m.Score
		out = append(out, scored{item: it, score: m.Score})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	items := make([]Item, len(out))
	for i, s := range out {
		items[i] = s.item
	}
	return items
}

// Prefix implements Mode.
func (r *RecentMode) Prefix() rune { return RecentPrefix }

// Placeholder implements Mode.
func (r *RecentMode) Placeholder() string { return "Recent files…" }

// Results implements Mode. Vanished files and the active file are dropped;
// the query fuzzy-matches the display path; equal scores keep MRU order.
func (r *RecentMode) Results(query string, cx Context) []Item {
	if r.list == nil {
		return nil
	}
	active := filepath.Clean(cx.ActivePath)
	type scored struct {
		item  Item
		score int
	}
	var out []scored
	now := r.clock()
	for _, e := range r.list() {
		if cx.ActivePath != "" && filepath.Clean(e.Path) == active {
			continue
		}
		if r.exists != nil && !r.exists(e.Path) {
			continue
		}
		title := displayRel(e.Path, cx.Root)
		m, ok := fuzzy.Match(query, title)
		if !ok {
			continue
		}
		out = append(out, scored{
			item: Item{
				Title: title,
				Spans: m.Positions,
				Score: m.Score,
				Msg:   OpenFileMsg{Path: e.Path},
				// Last-opened column + prune action (#1113), like the
				// project picker's rows after #842.
				Time: ui.RelTime(e.LastOpened, now),
				Aux:  RemoveRecentFileMsg{Path: e.Path},
			},
			score: m.Score,
		})
	}
	// Stable sort: score decides, ties keep the MRU (input) order.
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	items := make([]Item, len(out))
	for i, s := range out {
		items[i] = s.item
	}
	return items
}

// displayRel renders path relative to root (forward slashes) when it lies
// inside it, else as-is. MRU entries keep whatever form they were opened with
// — absolute from the explorer, sometimes relative — so both sides are made
// absolute before relativizing (Abs resolves against the process cwd, which
// is the project root the app runs in).
func displayRel(path, root string) string {
	if root != "" {
		absRoot, errRoot := filepath.Abs(root)
		absPath, errPath := filepath.Abs(path)
		if errRoot == nil && errPath == nil {
			if rel, err := filepath.Rel(absRoot, absPath); err == nil && !strings.HasPrefix(rel, "..") {
				return filepath.ToSlash(rel)
			}
		}
	}
	return filepath.ToSlash(path)
}
