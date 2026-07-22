// Package locations is the reusable grouped-locations list (Roadmap 0150):
// items (path, line, column range, line text) grouped by file, with a cursor,
// scroll window, and match-highlighted rendering. Find-in-path (#85) is its
// first consumer; the Problems window (#33) and the TODO index (#61) are the
// planned next ones — the component knows nothing about where its items come
// from.
package locations

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
)

// Item is one location: a line of Text in Path with an optional highlighted
// rune range. Line is 1-based; StartCol/EndCol are 0-based rune offsets
// (half-open); an empty range (StartCol == EndCol) renders without highlight.
type Item struct {
	Path     string
	Line     int
	StartCol int
	EndCol   int
	Text     string
}

// group is one file's items, in arrival order.
type group struct {
	path  string
	items []Item
}

// List is the stateful component. Append streams items in; the cursor walks
// items (file header rows are labels, not stops).
type List struct {
	groups []group
	total  int
	cursor int // index into the item sequence across groups
	top    int // first visible *row* (headers + items) of the render window
}

// Reset clears all items and state.
func (l *List) Reset() { *l = List{} }

// Append adds a batch of items, grouping consecutive items of the same path
// (both scanner backends emit file-contiguous matches; a path seen again
// later starts a new group rather than re-sorting arrival order).
func (l *List) Append(items []Item) {
	for _, it := range items {
		if n := len(l.groups); n > 0 && l.groups[n-1].path == it.Path {
			l.groups[n-1].items = append(l.groups[n-1].items, it)
		} else {
			l.groups = append(l.groups, group{path: it.Path, items: []Item{it}})
		}
		l.total++
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
	for _, g := range l.groups {
		if target == row {
			return 0, false // header row
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
	l.cursor = ((l.cursor+delta)%l.total + l.total) % l.total
	return l.Current()
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
	for _, g := range l.groups {
		row++ // header
		if i < len(g.items) {
			return row + i
		}
		row += len(g.items)
		i -= len(g.items)
	}
	return 0
}

// rowCount is the total number of render rows (headers + items).
func (l *List) rowCount() int { return len(l.groups) + l.total }

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
	// Scroll the window to keep the cursor row visible.
	cur := l.rowOfCursor()
	if cur < l.top {
		l.top = cur
	}
	if cur >= l.top+height {
		l.top = cur - height + 1
	}
	if max := l.rowCount() - height; l.top > max {
		l.top = max
	}
	if l.top < 0 {
		l.top = 0
	}

	header := lipgloss.NewStyle().Bold(true).Foreground(pal.BorderFocus)
	count := lipgloss.NewStyle().Faint(true)
	sel := lipgloss.NewStyle().Background(pal.SelectionMuted)
	match := lipgloss.NewStyle().Foreground(pal.BorderFocus).Bold(true).Underline(true)
	matchSel := match.Background(pal.SelectionMuted)
	lineNo := lipgloss.NewStyle().Faint(true)

	var out []string
	row, item := 0, 0
	for _, g := range l.groups {
		if row >= l.top+height {
			break
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
				out = append(out, l.renderItem(it, item == l.cursor, width, sel, match, matchSel, lineNo))
			}
			row++
			item++
		}
	}
	return strings.Join(out, "\n")
}

// renderItem renders one "  12: text" row with the match range highlighted,
// sliding the text window right when the match sits past the width budget.
func (l *List) renderItem(it Item, selected bool, width int, sel, match, matchSel, lineNo lipgloss.Style) string {
	no := strconv.Itoa(it.Line)
	prefix := "  " + strings.Repeat(" ", max(0, 5-len(no))) + no + ": "
	budget := width - lipgloss.Width(prefix)
	if budget < 8 {
		budget = 8
	}

	// Tabs flatten to spaces; embedded newlines (a multi-line match text,
	// #971) would render as a literal second row, so they flatten too.
	flat := strings.NewReplacer("\t", " ", "\n", " ", "\r", "").Replace(it.Text)
	runes := []rune(flat)
	start, end := clampRange(it.StartCol, it.EndCol, len(runes))
	// Slide the window so the match is visible; prepend an ellipsis when cut.
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
	pre := string(runes[off:min(start, winEnd)])
	mid := string(runes[min(start, winEnd):min(end, winEnd)])
	post := string(runes[min(end, winEnd):winEnd])
	if off > 0 && len(pre) > 0 {
		pre = "…" + string([]rune(pre)[1:])
	}

	if selected {
		return ansiClip(sel.Render(prefix+pre)+matchSel.Render(mid)+sel.Render(post), width)
	}
	return ansiClip(lineNo.Render(prefix)+pre+match.Render(mid)+post, width)
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
