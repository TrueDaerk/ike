// Package locations is the reusable grouped-locations list (Roadmap 0150):
// items (path, line, column range, line text) grouped by file, with a cursor,
// scroll window, and match-highlighted rendering. Find-in-path (#85) is its
// first consumer; the Problems window (#33) and the TODO index (#61) are the
// planned next ones — the component knows nothing about where its items come
// from.
//
// Items may carry an optional Section (#2413) — one grouping level above the
// file, rendered as a header row over the file headers of every consecutive
// group that shares it. Find in All Projects groups its hits by project that
// way, and keeps the cursor, paging and match-step behaviour of find-in-path
// unchanged.
package locations

import (
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
	"ike/internal/ui"
)

// Range is one highlighted rune range within a line: 0-based, half-open.
type Range struct {
	Start int
	End   int
}

// Item is one location: a line of Text in Path with an optional highlighted
// rune range. Line is 1-based; StartCol/EndCol are 0-based rune offsets
// (half-open); an empty range (StartCol == EndCol) renders without highlight.
//
// A line matched several times is one item, not one per occurrence (#1121):
// the extra ranges live in More and render highlighted in the same row.
type Item struct {
	Path     string
	Line     int
	StartCol int
	EndCol   int
	Text     string
	More     []Range
	// Excluded toggles the item out of the apply set (replace-in-path
	// selective apply, #2154): it renders dim and batch operations skip it.
	Excluded bool
	// Section is the optional grouping level above the file (#2413): items
	// sharing it, arriving consecutively, render under one section header.
	// The empty string (find-in-path, the TODO index) renders no header row
	// at all, so the flat file grouping is unchanged.
	Section string
}

// Ranges returns every match range of the item, ordered by start column.
func (it Item) Ranges() []Range {
	out := make([]Range, 0, 1+len(it.More))
	out = append(out, Range{Start: it.StartCol, End: it.EndCol})
	out = append(out, it.More...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// group is one file's items, in arrival order, within its section (#2413).
type group struct {
	path    string
	section string
	items   []Item
}

// List is the stateful component. Append streams items in; the cursor walks
// items (file header rows are labels, not stops).
type List struct {
	groups []group
	total  int
	cursor int // index into the item sequence across groups
	top    int // first visible *row* (headers + items) of the render window
	viewH  int // rows of the last Render window — the page-jump size (#1666)

	// SectionLabel renders a section header row (#2413) from the section key
	// and the number of items under it; nil renders the key itself in the
	// file-header style. The host owns the styling — Find in All Projects
	// spells the project name, its root and its scan error out there.
	SectionLabel func(section string, items int) string

	// Rewrite, when set, turns rows into replace previews (#2154): each match
	// range renders struck through, followed by the text Rewrite returns for
	// it. ok = false renders the match plainly (e.g. a template that no longer
	// applies). The host sets it before Render; Reset clears it with the rest.
	Rewrite func(it Item, r Range) (string, bool)
}

// Reset clears all items and state, keeping the last render height so a page
// jump before the first re-render still moves by a screenful. The render
// hooks (Rewrite, SectionLabel) are host state re-set per frame, so they go
// with it.
func (l *List) Reset() { *l = List{viewH: l.viewH} }

// Append adds a batch of items, grouping consecutive items of the same path
// (both scanner backends emit file-contiguous matches; a path seen again
// later starts a new group rather than re-sorting arrival order).
//
// Consecutive items on the same line of the same file collapse into one item
// (#1121): the row count is per line, and the extra ranges ride along in More
// so every occurrence still renders highlighted. Both backends emit a line's
// matches contiguously, so comparing against the group's last item suffices.
func (l *List) Append(items []Item) {
	for _, it := range items {
		if n := len(l.groups); n > 0 && l.groups[n-1].path == it.Path && l.groups[n-1].section == it.Section {
			g := &l.groups[n-1]
			if last := &g.items[len(g.items)-1]; last.Line == it.Line {
				mergeRanges(last, it)
				continue
			}
			g.items = append(g.items, it)
		} else {
			l.groups = append(l.groups, group{path: it.Path, section: it.Section, items: []Item{it}})
		}
		l.total++
	}
}

// mergeRanges folds src's match ranges into dst, dropping ones already there.
func mergeRanges(dst *Item, src Item) {
	for _, r := range src.Ranges() {
		dup := r == Range{Start: dst.StartCol, End: dst.EndCol}
		for _, have := range dst.More {
			dup = dup || have == r
		}
		if !dup {
			dst.More = append(dst.More, r)
		}
	}
}

// Total returns the item count; Files the group count.
func (l *List) Total() int { return l.total }
func (l *List) Files() int { return len(l.groups) }

// Current returns the item under the cursor.
func (l *List) Current() (Item, bool) {
	if l.total == 0 {
		return Item{}, false
	}
	i := l.cursor
	for _, g := range l.groups {
		if i < len(g.items) {
			return g.items[i], true
		}
		i -= len(g.items)
	}
	return Item{}, false
}

// Cursor returns the item index under the cursor.
func (l *List) Cursor() int { return l.cursor }

// SetCursor moves the cursor to item index i, clamped to the item range.
func (l *List) SetCursor(i int) {
	l.cursor = i
	if l.cursor >= l.total {
		l.cursor = l.total - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
}

// ItemAt maps a visible row of the last Render window (0-based from the
// window's top) to the index of the item rendered on it; header rows and
// rows past the end report ok = false.
func (l *List) ItemAt(visibleRow int) (int, bool) {
	if visibleRow < 0 {
		return 0, false
	}
	target := l.top + visibleRow
	row, item := 0, 0
	for gi, g := range l.groups {
		if l.sectionHead(gi) {
			if target == row {
				return 0, false // section header row (#2413)
			}
			row++
		}
		if target == row {
			return 0, false // file header row
		}
		row++
		if target < row+len(g.items) {
			return item + (target - row), true
		}
		row += len(g.items)
		item += len(g.items)
	}
	return 0, false
}

// sectionHead reports whether group gi opens a new section — the row rendered
// above its file header (#2413). A group with an empty section never does, so
// a sectionless list keeps its flat file layout.
func (l *List) sectionHead(gi int) bool {
	s := l.groups[gi].section
	return s != "" && (gi == 0 || l.groups[gi-1].section != s)
}

// sectionItems counts the items of the section group gi opens: the run of
// consecutive groups sharing its section key.
func (l *List) sectionItems(gi int) int {
	n := 0
	for i := gi; i < len(l.groups) && l.groups[i].section == l.groups[gi].section; i++ {
		n += len(l.groups[i].items)
	}
	return n
}

// sectionRows is the number of section header rows in the whole list.
func (l *List) sectionRows() int {
	n := 0
	for gi := range l.groups {
		if l.sectionHead(gi) {
			n++
		}
	}
	return n
}

// Move shifts the cursor by delta, clamped to the item range.
func (l *List) Move(delta int) {
	l.cursor += delta
	if l.cursor >= l.total {
		l.cursor = l.total - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
}

// Advance shifts the cursor by delta with wrap-around and returns the new
// current item — the next/prev-match primitive that works without the list
// rendered.
func (l *List) Advance(delta int) (Item, bool) {
	if l.total == 0 {
		return Item{}, false
	}
	l.cursor = ui.StepIndex(l.cursor, delta, l.total)
	return l.Current()
}

// Step shifts the cursor by delta single steps with wrap-around (#1666) —
// the up/down key primitive, as opposed to Move's clamped wheel semantics.
func (l *List) Step(delta int) { l.cursor = ui.StepIndex(l.cursor, delta, l.total) }

// StepMatch is Step in the shape the shared cmd+g chord reports (#2410): it
// wraps the same way and says where the cursor landed, so the pane's filter
// row can show "3/17" — or "1/12 (wrapped)" for the step that came back
// around.
func (l *List) StepMatch(delta int) ui.MatchStep {
	if l.total == 0 {
		return ui.NoMatches()
	}
	next, wrapped := ui.StepWrap(l.cursor, l.total, delta)
	l.cursor = next
	return ui.Stepped(next, l.total, wrapped)
}

// Page shifts the cursor by delta windows of the last Render height, clamped
// at both ends (#1666). Header rows count towards the jump — the cursor lands
// on the item nearest the target render row — so one pgdn scrolls exactly one
// screenful whatever the group layout. An unrendered list falls back to ten
// items.
func (l *List) Page(delta int) {
	if l.total == 0 {
		return
	}
	h := l.viewH
	if h < 1 {
		h = 10
	}
	l.cursor = l.itemNearRow(l.rowOfCursor() + delta*h)
}

// Home and End jump to the first/last item.
func (l *List) Home() { l.cursor = 0 }
func (l *List) End()  { l.cursor = max(0, l.total-1) }

// itemNearRow returns the item whose render row is closest to target, so a
// page jump that lands on a file header resolves to a real stop.
func (l *List) itemNearRow(target int) int {
	row, item, best, bestDist := 0, 0, 0, -1
	for gi, g := range l.groups {
		if l.sectionHead(gi) {
			row++ // section header (#2413)
		}
		row++ // file header
		for range g.items {
			d := row - target
			if d < 0 {
				d = -d
			}
			if bestDist < 0 || d < bestDist {
				best, bestDist = item, d
			}
			row++
			item++
		}
	}
	return best
}

// ToggleExcluded flips the cursor item in or out of the apply set (#2154).
func (l *List) ToggleExcluded() bool {
	it := l.currentRef()
	if it == nil {
		return false
	}
	it.Excluded = !it.Excluded
	return true
}

// ToggleExcludedGroup flips the cursor's whole file (#2154): any included
// item excludes them all; a fully excluded file re-includes them all.
func (l *List) ToggleExcludedGroup() {
	g := l.currentGroupRef()
	if g == nil {
		return
	}
	exclude := false
	for _, it := range g.items {
		if !it.Excluded {
			exclude = true
			break
		}
	}
	for i := range g.items {
		g.items[i].Excluded = exclude
	}
}

// ExcludedCount reports how many items are toggled out of the apply set.
func (l *List) ExcludedCount() int {
	n := 0
	for _, g := range l.groups {
		for _, it := range g.items {
			if it.Excluded {
				n++
			}
		}
	}
	return n
}

// IncludedCount reports how many items remain in the apply set.
func (l *List) IncludedCount() int { return l.total - l.ExcludedCount() }

// RemoveIncluded removes and returns every non-excluded item in display
// order — the replace-all batch (#2154). Excluded rows stay listed so the
// user sees what was deliberately left alone.
func (l *List) RemoveIncluded() []Item {
	return l.removeIncluded(nil)
}

// RemoveIncludedGroup removes and returns the cursor file's non-excluded
// items — the replace-file batch (#2154).
func (l *List) RemoveIncludedGroup() []Item {
	g := l.currentGroupRef()
	if g == nil {
		return nil
	}
	return l.removeIncluded(g)
}

// removeIncluded extracts the non-excluded items of one group (or, with a
// nil group, of every group), keeping the cursor on the nearest survivor.
func (l *List) removeIncluded(only *group) []Item {
	var out []Item
	var groups []group
	newCursor := l.cursor
	idx := 0
	for gi := range l.groups {
		g := &l.groups[gi]
		if only != nil && g != only {
			groups = append(groups, *g)
			idx += len(g.items)
			continue
		}
		var keep []Item
		for _, it := range g.items {
			if it.Excluded {
				keep = append(keep, it)
			} else {
				out = append(out, it)
				if idx < l.cursor {
					newCursor--
				}
			}
			idx++
		}
		if len(keep) > 0 {
			groups = append(groups, group{path: g.path, section: g.section, items: keep})
		}
	}
	l.groups = groups
	l.total -= len(out)
	l.cursor = newCursor
	l.clampCursor()
	return out
}

// currentRef returns a pointer to the cursor's item.
func (l *List) currentRef() *Item {
	i := l.cursor
	for gi := range l.groups {
		g := &l.groups[gi]
		if i < len(g.items) {
			return &g.items[i]
		}
		i -= len(g.items)
	}
	return nil
}

// currentGroupRef returns a pointer to the cursor's group.
func (l *List) currentGroupRef() *group {
	i := l.cursor
	for gi := range l.groups {
		if i < len(l.groups[gi].items) {
			return &l.groups[gi]
		}
		i -= len(l.groups[gi].items)
	}
	return nil
}

// clampCursor keeps the cursor inside the item range after removals.
func (l *List) clampCursor() {
	if l.cursor >= l.total {
		l.cursor = l.total - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
}

// All returns every item in display order (replace-all consumes this).
func (l *List) All() []Item {
	out := make([]Item, 0, l.total)
	for _, g := range l.groups {
		out = append(out, g.items...)
	}
	return out
}

// CurrentGroup returns the cursor's file and all of its items.
func (l *List) CurrentGroup() (string, []Item) {
	i := l.cursor
	for _, g := range l.groups {
		if i < len(g.items) {
			return g.path, g.items
		}
		i -= len(g.items)
	}
	return "", nil
}

// RemoveCurrent drops the item under the cursor (its group too when it
// empties), keeping the cursor on the next item.
func (l *List) RemoveCurrent() (Item, bool) {
	it, ok := l.Current()
	if !ok {
		return Item{}, false
	}
	i := l.cursor
	for gi := range l.groups {
		g := &l.groups[gi]
		if i < len(g.items) {
			g.items = append(g.items[:i], g.items[i+1:]...)
			if len(g.items) == 0 {
				l.groups = append(l.groups[:gi], l.groups[gi+1:]...)
			}
			break
		}
		i -= len(g.items)
	}
	l.total--
	if l.cursor >= l.total {
		l.cursor = l.total - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	return it, true
}

// RemoveGroup drops every item of path, adjusting the cursor.
func (l *List) RemoveGroup(path string) {
	itemsBefore := 0
	for gi, g := range l.groups {
		if g.path == path {
			l.total -= len(g.items)
			if l.cursor >= itemsBefore {
				if l.cursor < itemsBefore+len(g.items) {
					l.cursor = itemsBefore // was inside: land on the successor
				} else {
					l.cursor -= len(g.items)
				}
			}
			l.groups = append(l.groups[:gi], l.groups[gi+1:]...)
			break
		}
		itemsBefore += len(g.items)
	}
	if l.cursor >= l.total {
		l.cursor = l.total - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
}

// rowOfCursor maps the cursor's item index to its render row (accounting for
// the header row above each group).
func (l *List) rowOfCursor() int {
	i, row := l.cursor, 0
	for gi, g := range l.groups {
		if l.sectionHead(gi) {
			row++ // section header (#2413)
		}
		row++ // file header
		if i < len(g.items) {
			return row + i
		}
		row += len(g.items)
		i -= len(g.items)
	}
	return 0
}

// rowCount is the total number of render rows (section headers, file headers
// and items).
func (l *List) rowCount() int { return len(l.groups) + l.total + l.sectionRows() }

// Render lays the list out to width×height, scrolled so the cursor is
// visible. displayPath shortens paths for the header rows (nil renders them
// verbatim).
func (l *List) Render(width, height int, pal *theme.Palette, displayPath func(string) string) string {
	if l.total == 0 || width < 8 || height < 1 {
		return ""
	}
	if displayPath == nil {
		displayPath = func(p string) string { return p }
	}
	// Scroll the window to keep the cursor row visible, and remember the
	// window height as the page-jump size (#1666).
	l.viewH = height
	l.top = ui.ScrollToShow(l.top, l.rowOfCursor(), height, l.rowCount())

	header := lipgloss.NewStyle().Bold(true).Foreground(pal.BorderFocus)
	count := lipgloss.NewStyle().Faint(true)
	match := lipgloss.NewStyle().Foreground(pal.BorderFocus).Bold(true).Underline(true)
	add := lipgloss.NewStyle().Foreground(pal.Success).Bold(true)
	st := rowStyles{
		sel:      lipgloss.NewStyle().Background(pal.SelectionMuted),
		match:    match,
		matchSel: match.Background(pal.SelectionMuted),
		lineNo:   lipgloss.NewStyle().Faint(true),
		// The replace preview (#2154) strikes the match and appends the
		// replacement; an excluded row renders entirely faint.
		strike:    match.Underline(false).Strikethrough(true),
		strikeSel: match.Underline(false).Strikethrough(true).Background(pal.SelectionMuted),
		add:       add,
		addSel:    add.Background(pal.SelectionMuted),
		exc:       lipgloss.NewStyle().Faint(true),
		excSel:    lipgloss.NewStyle().Faint(true).Background(pal.SelectionMuted),
	}

	var out []string
	row, item := 0, 0
	for gi, g := range l.groups {
		if row >= l.top+height {
			break
		}
		if l.sectionHead(gi) {
			if row >= l.top {
				out = append(out, ansiClip(l.sectionRow(gi, width, pal), width))
			}
			row++
			if row >= l.top+height {
				break
			}
		}
		if row >= l.top {
			h := header.Render(truncateRunes(displayPath(g.path), width-8)) +
				count.Render(" ("+strconv.Itoa(len(g.items))+")")
			out = append(out, ansiClip(h, width))
		}
		row++
		for _, it := range g.items {
			if row >= l.top+height {
				break
			}
			if row >= l.top {
				out = append(out, l.renderItem(it, item == l.cursor, width, st))
			}
			row++
			item++
		}
	}
	return strings.Join(out, "\n")
}

// sectionRow renders the section header of group gi (#2413) through the
// host's SectionLabel hook, falling back to the key in the file-header style.
func (l *List) sectionRow(gi, width int, pal *theme.Palette) string {
	if l.SectionLabel != nil {
		return l.SectionLabel(l.groups[gi].section, l.sectionItems(gi))
	}
	return lipgloss.NewStyle().Bold(true).Foreground(pal.BorderFocus).
		Render(truncateRunes(l.groups[gi].section, width))
}

// rowStyles bundles the item-row styles Render builds once per frame.
type rowStyles struct {
	sel, match, matchSel, lineNo   lipgloss.Style
	strike, strikeSel, add, addSel lipgloss.Style // replace preview (#2154)
	exc, excSel                    lipgloss.Style // excluded rows (#2154)
}

// renderItem renders one "  12: text" row with every match range on the line
// highlighted (#1121), sliding the text window right when the first match sits
// past the width budget. With a Rewrite hook set (#2154) each match renders
// struck through with its replacement appended; an excluded item renders
// entirely faint with a ✗ marker and no preview.
func (l *List) renderItem(it Item, selected bool, width int, st rowStyles) string {
	sel, match, matchSel, lineNo := st.sel, st.match, st.matchSel, st.lineNo
	no := strconv.Itoa(it.Line)
	lead := "  "
	if it.Excluded {
		lead = "✗ "
		match, matchSel, lineNo = st.exc, st.excSel, st.exc
		sel = st.excSel
	}
	prefix := lead + strings.Repeat(" ", max(0, 5-len(no))) + no + ": "
	budget := width - lipgloss.Width(prefix)
	if budget < 8 {
		budget = 8
	}

	// Tabs flatten to spaces; embedded newlines (a multi-line match text,
	// #971) would render as a literal second row, so they flatten too.
	flat := strings.NewReplacer("\t", " ", "\n", " ", "\r", "").Replace(it.Text)
	runes := []rune(flat)
	ranges := clampRanges(it.Ranges(), len(runes))
	start := 0
	if len(ranges) > 0 {
		start = ranges[0].Start
	}
	// Slide the window so the first match is visible; prepend an ellipsis when
	// cut.
	off := 0
	if start > budget-8 {
		off = start - budget/2
		if off > len(runes)-budget {
			off = len(runes) - budget
		}
		if off < 0 {
			off = 0
		}
	}
	winEnd := min(len(runes), off+budget)

	plain := func(s string) string {
		if selected {
			return sel.Render(s)
		}
		if it.Excluded {
			return st.exc.Render(s)
		}
		return s
	}
	hl := func(s string) string {
		if selected {
			return matchSel.Render(s)
		}
		return match.Render(s)
	}
	// preview returns the replacement text to append after a match range, or
	// ok = false for a plain highlight (no hook, excluded row, stale template).
	preview := func(r Range) (string, bool) {
		if l.Rewrite == nil || it.Excluded {
			return "", false
		}
		return l.Rewrite(it, r)
	}
	addStyle := st.add
	strike, strikeSel := st.strike, st.strikeSel
	if selected {
		addStyle = st.addSel
	}

	var b strings.Builder
	if selected {
		b.WriteString(sel.Render(prefix))
	} else {
		b.WriteString(lineNo.Render(prefix))
	}
	pos, leading := off, true
	writePlain := func(seg string) {
		if leading && off > 0 {
			seg = "…" + string([]rune(seg)[1:])
		}
		leading = false
		b.WriteString(plain(seg))
	}
	for _, r := range ranges {
		s, e := max(r.Start, pos), min(r.End, winEnd)
		if s >= winEnd {
			break
		}
		if e <= s {
			continue // empty or already-covered range
		}
		if s > pos {
			writePlain(string(runes[pos:s]))
		}
		leading = false
		if repl, ok := preview(r); ok {
			style := strike
			if selected {
				style = strikeSel
			}
			b.WriteString(style.Render(string(runes[s:e])))
			b.WriteString(addStyle.Render(repl))
		} else {
			b.WriteString(hl(string(runes[s:e])))
		}
		pos = e
	}
	if pos < winEnd {
		writePlain(string(runes[pos:winEnd]))
	}
	return ansiClip(b.String(), width)
}

// clampRanges sanitizes highlight ranges against the rune length and sorts
// them by start, so the renderer can walk the line left to right.
func clampRanges(rs []Range, n int) []Range {
	out := make([]Range, 0, len(rs))
	for _, r := range rs {
		s, e := clampRange(r.Start, r.End, n)
		out = append(out, Range{Start: s, End: e})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// clampRange sanitizes a highlight range against the rune length.
func clampRange(start, end, n int) (int, int) {
	if start < 0 {
		start = 0
	}
	if end > n {
		end = n
	}
	if end < start {
		end = start
	}
	if start > n {
		start = n
	}
	return start, end
}

// truncateRunes cuts s to at most n runes with an ellipsis.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if n <= 1 || len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n+1:])
}

// ansiClip hard-caps a styled row to width cells. ansi.Truncate, not
// lipgloss MaxWidth — MaxWidth WRAPS overlong content onto a second line
// (#971), which corrupts single-row lists.
func ansiClip(s string, width int) string {
	return ansi.Truncate(s, width, "…")
}
