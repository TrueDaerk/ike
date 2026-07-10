package palette

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ike/internal/fuzzy"
)

// RecentPrefix selects the recent-files mode inside the palette. The root
// model opens the palette locked to it (palette.recentFiles, cmd+e), so the
// rune never needs typing; it only has to be unique among modes.
const RecentPrefix = '%'

// RecentMode is the recent-files mode (Roadmap 0230): the most-recently-used
// file list, JetBrains' Recent Files popup palette-style. The palette owns no
// MRU store of its own — the list func is injected by the root model, which
// touches it on every file open and tab activation. With an empty query the
// items keep MRU order (most recent first) with the currently active file
// excluded, so opening the mode and pressing enter jumps to the *previous*
// file; a query fuzzy-matches the project-relative path, ties keep MRU order.
type RecentMode struct {
	// list returns the MRU paths, most recent first. Injected by the app.
	list func() []string
	// exists filters vanished files out of the listing. Injectable for tests;
	// defaults to an on-disk stat.
	exists func(path string) bool
}

// NewRecentMode builds the recent-files mode over the injected MRU source.
func NewRecentMode(list func() []string) *RecentMode {
	return &RecentMode{list: list, exists: fileExists}
}

// fileExists is the default existence filter: a stat-able non-directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
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
	for _, p := range r.list() {
		if cx.ActivePath != "" && filepath.Clean(p) == active {
			continue
		}
		if r.exists != nil && !r.exists(p) {
			continue
		}
		title := displayRel(p, cx.Root)
		m, ok := fuzzy.Match(query, title)
		if !ok {
			continue
		}
		out = append(out, scored{
			item:  Item{Title: title, Spans: m.Positions, Score: m.Score, Msg: OpenFileMsg{Path: p}},
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
