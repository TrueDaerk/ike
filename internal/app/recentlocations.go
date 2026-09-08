package app

// recentlocations.go implements Last Edit Location and Recent Locations
// (#2545): nav.lastEdit jumps to the most recent edit site (repeats walk back
// through the edit ring, nav/edits.go), and nav.recentLocations opens a
// palette listing the edit locations and the jump history's positions with a
// one-line code preview. A location recorded in another project (the ring is
// session state and rides across switches) switches the project first
// through the same parked-open transaction the all-projects search uses.

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/allfind"
	"ike/internal/host"
	"ike/internal/nav"
	"ike/internal/palette"
	"ike/internal/project"
)

// recentLocationsPrefix selects the recent-locations mode. Only ever opened
// locked, so the rune has no user-facing prefix story.
const recentLocationsPrefix = '.'

// NavLastEditMsg asks the root model to jump to the most recent edit
// location; a repeat walks back through the ring. Dispatched by nav.lastEdit.
type NavLastEditMsg struct{}

// ShowRecentLocationsMsg asks the root model to open the palette locked to
// the recent-locations picker. Dispatched by nav.recentLocations.
type ShowRecentLocationsMsg struct{}

// RecentLocationJumpMsg jumps to a picked location. Root is the project the
// location was recorded in ("" for jump-history rows, which are always the
// current project's); a foreign root switches the project first.
type RecentLocationJumpMsg struct {
	Root string
	Path string
	Line int
	Col  int
}

// recentLocationsMode is a palette Mode over the snapshot taken when the
// picker opened (Set).
type recentLocationsMode struct {
	items []palette.Item
}

// recentLocationRows caps the picker: the ring holds 50 edits and the jump
// history up to 200 positions; the newest of each are what one comes back for.
const recentLocationRows = 60

// Set replaces the mode's rows: the edit ring newest first (rows marked "✎"),
// then the jump history newest first (rows marked "↷"), a location listed
// once — an edit site that was also a jump departure keeps its edit row.
func (r *recentLocationsMode) Set(edits []nav.Location, jumps []nav.Position, lineText func(path string, line int) string) {
	r.items = nil
	seen := map[nav.Position]bool{}
	add := func(mark string, loc nav.Location) {
		key := nav.Position{Path: loc.Path, Line: loc.Line}
		if seen[key] || len(r.items) >= recentLocationRows {
			return
		}
		seen[key] = true
		r.items = append(r.items, palette.Item{
			Title:   mark + "  " + displayPath(loc.Path) + ":" + strconv.Itoa(loc.Line+1),
			Detail:  markPreview(lineText(loc.Path, loc.Line)),
			Msg:     RecentLocationJumpMsg{Root: loc.Root, Path: loc.Path, Line: loc.Line, Col: loc.Col},
			Preview: markTarget(loc.Path, loc.Line),
		})
	}
	for _, e := range edits {
		add("✎", e)
	}
	for _, j := range jumps {
		add("↷", nav.Location{Position: j})
	}
}

// Prefix implements palette.Mode.
func (r *recentLocationsMode) Prefix() rune { return recentLocationsPrefix }

// Placeholder implements palette.Mode.
func (r *recentLocationsMode) Placeholder() string { return "Recent locations…" }

// CodePreview implements palette.PreviewMode (#2053): every row is a file
// position, so the code column shows the location in context.
func (r *recentLocationsMode) CodePreview() bool { return true }

// Results implements palette.Mode: the snapshot fuzzy-matched on the row
// title and preview; an empty query keeps the recency order.
func (r *recentLocationsMode) Results(query string, cx palette.Context) []palette.Item {
	if query == "" {
		return append([]palette.Item(nil), r.items...)
	}
	items := palette.FuzzyItems(query, r.items,
		func(it palette.Item) string { return it.Title + " " + it.Detail },
		func(it palette.Item) palette.Item { return it })
	palette.SortByScore(items)
	return items
}

// locationLineText reads the preview line of a location: from the open buffer
// when the file is loaded in the active workspace, from disk otherwise (a
// foreign project's file is only ever on disk here).
func (m Model) locationLineText(path string, line int) string {
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil {
			continue
		}
		for _, ed := range inst.Editors() {
			if ed.HasFile() && canonicalPath(ed.Path()) == canonicalPath(path) {
				return ed.LineText(line)
			}
		}
	}
	return fileLine(path, line)
}

// showRecentLocations opens the picker over the current ring and history.
func (m Model) showRecentLocations() (tea.Model, tea.Cmd) {
	m.recentLocs.Set(m.editRing.Recent(), m.navHist.Recent(), m.locationLineText)
	if len(m.recentLocs.items) == 0 {
		m.host.Notify(host.Info, "no recent locations yet — edit or jump somewhere first")
		return m, nil
	}
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(m.paletteContext(), recentLocationsPrefix)
	return m, nil
}

// navigateLastEdit runs one nav.lastEdit step: the ring hands out the next
// older edit site relative to the caret, stale files are skipped in place.
func (m Model) navigateLastEdit() (tea.Model, tea.Cmd) {
	cur := m.currentNavPos()
	for {
		target, ok := m.editRing.Step(cur)
		if !ok {
			m.host.Notify(host.Info, "no earlier edit location")
			return m, nil
		}
		if navExists(target.Position) {
			return m.jumpToLocation(target)
		}
		// A deleted or renamed file: pretend the caret sits there so the
		// next Step walks past it.
		cur = target.Position
	}
}

// jumpToLocation opens loc through the open funnel — recording the departure
// like any jump — or, for a location of another project, parks the open and
// runs the switch transaction; the SwitchedMsg handler finishes the job.
func (m Model) jumpToLocation(loc nav.Location) (tea.Model, tea.Cmd) {
	if loc.Root != "" && !sameRoot(loc.Root, m.activeWS().Root) {
		m.allPendingOpen = &allfind.OpenMatchMsg{Root: loc.Root, Path: loc.Path, Line: loc.Line + 1, Col: loc.Col}
		return m, project.SwitchTo(loc.Root)
	}
	return m.openPathAt(loc.Path, loc.Line, loc.Col)
}

// sameRoot compares two project roots, tolerating the cwd-vs-recorded
// spelling difference (symlinked roots) by canonicalising both.
func sameRoot(a, b string) bool {
	return a == b || canonicalPath(a) == canonicalPath(b)
}
