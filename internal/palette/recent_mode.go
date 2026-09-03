package palette

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ike/internal/frecency"
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
// touches it on every file open and tab activation. The currently active file
// is always excluded, so opening the mode and pressing enter jumps away from
// where one is; a query fuzzy-matches the project-relative path.
//
// Ranking is frecency since #2399 — how often *and* how recently an entry was
// opened, from the same store the '@' finder ranks with — with plain MRU order
// as the fallback for entries (and projects) that carry no history, and as the
// whole listing's order under palette.recent.ranking = "recency". The dialog
// also reopens on the row it was last used to activate (PreselectMode) and
// narrows to its projects column on a "p:" query.
type RecentMode struct {
	// list returns the MRU entries, most recent first. Injected by the app.
	list func() []RecentEntry
	// exists filters vanished files out of the listing. Injectable for tests;
	// defaults to an on-disk stat.
	exists func(path string) bool
	// projects supplies the Recent Projects column (#778): items already
	// carrying their activation Msg (the app injects project.PickedMsg
	// values, so the palette stays project-agnostic). Nil hides the column.
	// Since #2399 each item also carries its Key (the project path) and its
	// Rank (the project's frecency score, which only the app can compute).
	projects func() []Item
	// frec ranks the file rows by frecency (#2399), keyed by frecency.Key of
	// the entry path — the same store the '@' finder ranks with (#2155), so
	// the two windows agree on what this project actually works on. Nil (or
	// ranking turned off) falls back to plain MRU order.
	frec *Frecency
	// frecencyRanking gates the blend, so palette.recent.ranking = "recency"
	// restores the pre-#2399 pure-MRU listing without a restart. Nil counts
	// as enabled.
	frecencyRanking func() bool
	// lastPick returns the Key of the row picked the last time this dialog
	// was used in this project, and whether it was a project row (#2399).
	// Nil preselects nothing.
	lastPick func() (string, bool)
	// onPick records an activation for the next open's preselection (#2399).
	onPick func(key string, side bool)
	// now overrides the clock for the last-opened column (#1113); tests only.
	now func() time.Time
}

// ProjectsOnlyPrefix restricts the recent-files dialog to its Recent Projects
// column (#2399): typing "p:" empties the file list, so the automatic focus
// placement hands the keyboard to the projects column and the rest of the
// query filters projects alone. The two columns are otherwise filtered by one
// shared query, which made a project hard to reach whenever a file matched the
// same letters better. `tab` (the column toggle) stays the mouse-free way to
// the column with an empty query; the prefix is the way to *filter* it.
const ProjectsOnlyPrefix = "p:"

// projectsOnly reports whether the query selects the projects column alone,
// and returns the query with the prefix stripped.
func projectsOnly(query string) (rest string, only bool) {
	if !strings.HasPrefix(query, ProjectsOnlyPrefix) {
		return query, false
	}
	return strings.TrimPrefix(query, ProjectsOnlyPrefix), true
}

// recentKey normalizes an MRU path into the frecency store's key space and
// the row's stable identity (#2399), resolving relative spellings against a
// pre-resolved working directory instead of asking the OS per entry. It must
// agree with frecency.Key, which is what the file-open sites record with.
func recentKey(path, cwd string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) && cwd != "" {
		return filepath.Join(cwd, path)
	}
	return filepath.Clean(path)
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

// SetFrecency installs the file-open history behind the frecency ranking
// (#2399) — the same store the '@' finder ranks with (#2155).
func (r *RecentMode) SetFrecency(f *Frecency) { r.frec = f }

// SetRanking installs the palette.recent.ranking gate (#2399): the func is
// consulted per listing, so a settings flip applies to the very next open.
// Nil means frecency ranking is on.
func (r *RecentMode) SetRanking(enabled func() bool) { r.frecencyRanking = enabled }

// SetLastPick installs the preselection source (#2399): the Key of the row
// this dialog was last used to open in this project, plus whether it was a
// project row.
func (r *RecentMode) SetLastPick(f func() (string, bool)) { r.lastPick = f }

// SetPickRecorder installs the sink the activated row is reported to (#2399);
// the root model persists it per project so the next open comes back on it.
func (r *RecentMode) SetPickRecorder(f func(key string, side bool)) { r.onPick = f }

// ranked reports whether palette.recent.ranking leaves frecency on (#2399).
// Nil — no gate wired — counts as on, so a mode built without the setting
// still ranks.
func (r *RecentMode) ranked() bool {
	return r.frecencyRanking == nil || r.frecencyRanking()
}

// rankedFiles is ranked for the file list, which additionally needs the
// file-open store to have been wired at all. The projects column carries its
// scores on the items, so it only consults the setting.
func (r *RecentMode) rankedFiles() bool { return r.frec != nil && r.ranked() }

// Preselect implements PreselectMode (#2399): with an empty query body the
// dialog opens on the row it was last used to open, so cmd+e + enter bounces
// between the two files one is switching between instead of walking the MRU
// list down. Any typed query is a fresh intent and ranks normally.
func (r *RecentMode) Preselect(query string) (string, bool) {
	if query != "" || r.lastPick == nil {
		return "", false
	}
	return r.lastPick()
}

// RecordPick implements PickRecorder (#2399).
func (r *RecentMode) RecordPick(it Item, side bool) {
	if r.onPick == nil || it.Key == "" {
		return
	}
	r.onPick(it.Key, side)
}

// Hint implements HintMode (#2399): the dialog's two non-obvious affordances
// — the column toggle and the projects-only filter — spelled out under the
// list, since neither is visible in a row.
func (r *RecentMode) Hint(query string) string {
	if r.projects == nil {
		return ""
	}
	if _, only := projectsOnly(query); only {
		return "projects only — backspace the \"" + ProjectsOnlyPrefix + "\" for files"
	}
	return "tab: switch column · " + ProjectsOnlyPrefix + " projects only · shift+del: forget"
}

// SideTitle implements SideMode.
func (r *RecentMode) SideTitle() string { return "Recent Projects" }

// SideResults implements SideMode: the injected recent projects, filtered by
// the query (fuzzy on the title, frecency blended in like the file list —
// #2399 — and ties keeping recency order). A "p:" prefix (ProjectsOnlyPrefix)
// filters projects alone and is stripped before matching.
func (r *RecentMode) SideResults(query string, _ Context) []Item {
	if r.projects == nil {
		return nil
	}
	body, _ := projectsOnly(query)
	type scored struct {
		item Item
		rank float64
	}
	var out []scored
	// A project's frecency is computed by the app (only it knows the project
	// paths) and arrives as Item.Rank; the blend policy is the file list's.
	qlen := len([]rune(strings.TrimSpace(body)))
	var boost func(float64) float64
	if r.ranked() {
		boost = func(rank float64) float64 { return frecencyBoost(rank, qlen) }
	}
	for _, it := range r.projects() {
		m, ok := fuzzy.Match(body, it.Title)
		if !ok {
			continue
		}
		it.Spans = m.Positions
		it.Score = m.Score
		rank := float64(m.Score)
		if boost != nil {
			rank += boost(it.Rank)
		}
		out = append(out, scored{item: it, rank: rank})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].rank > out[j].rank })
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
// the query fuzzy-matches the display path. Ranking is frecency (#2399) — the
// file-open history the '@' finder ranks with, blended into the fuzzy score by
// the shared frecencyBoost policy, so an empty query lists what this project
// actually keeps coming back to and a typed query hands the lead to match
// quality. Files with no recorded history score 0 and therefore keep plain MRU
// order among themselves, which is also the whole listing's shape on a fresh
// project (the pure-recency fallback) and with the setting on "recency".
// A "p:" query (ProjectsOnlyPrefix) lists no files at all: it belongs to the
// Recent Projects column, and the empty file list hands it the focus.
func (r *RecentMode) Results(query string, cx Context) []Item {
	if r.list == nil {
		return nil
	}
	if _, only := projectsOnly(query); only {
		return nil
	}
	active := filepath.Clean(cx.ActivePath)
	type scored struct {
		item Item
		rank float64
	}
	var out []scored
	now := r.clock()
	// The frecency boost damps with the query length, resolved once per call.
	qlen := len([]rune(strings.TrimSpace(query)))
	ranked := r.rankedFiles()
	// The working directory the relative MRU spellings resolve against is
	// resolved once, not per entry: frecency.Key consults it and the list runs
	// to fifty entries, re-keyed on every keystroke (the same reason
	// file_mode.go hoists its frecRoot).
	cwd := frecency.Key(".")
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
		key := recentKey(e.Path, cwd)
		rank := float64(m.Score)
		if ranked {
			rank += frecencyBoost(r.frec.Score(key), qlen)
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
				// Stable identity for the preselection (#2399); the title is
				// root-relative and would change under a project switch.
				Key: key,
			},
			rank: rank,
		})
	}
	// Stable sort: the blended rank decides, ties keep the MRU (input) order.
	sort.SliceStable(out, func(i, j int) bool { return out[i].rank > out[j].rank })
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
