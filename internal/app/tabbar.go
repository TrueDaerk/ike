package app

import (
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/pane"
	"ike/internal/theme"
)

// tabbar.go renders the editor pane's tab bar (Roadmap 0190, #157). The bar
// occupies the pane's top row — the same line the single-document title used —
// so showing tabs costs no extra editor row: with one tab the classic title
// renders (unless editor.tabs.always_show), with two or more the tab list does.
// Overflow elides around the active tab; the bar never wraps.

// tabEllipsis is the truncation marker of a label that does not fit its
// segment (#2151); the hidden tabs beyond either end of the window are marked
// by a counting overflow indicator instead (tabOverflowMark).
const tabEllipsis = "…"

// tabLabelCap bounds one segment's label width (#2151): a very long label
// ellipsizes to this many cells so a single tab can never push every neighbour
// out of the bar. Wide enough for "somelongmodule_test.go ●" while still
// leaving room for two or three siblings on an 80-cell pane.
const tabLabelCap = 24

// tabCloseGlyph is the per-segment close button (#1128); tabCloseW is the
// extra cells a segment spends on it: the glyph plus its trailing pad,
// rendered after the label's own right padding (" label ✕ ").
const (
	tabCloseGlyph = "✕"
	tabCloseW     = 2
)

// tabPinPrefix marks a pinned tab's segment (#1172). It is part of the label
// string itself — a single-width glyph plus a space — so tabWindow and tabHit
// measure it for free and the mirrored geometry stays consistent; renderTabBar
// merely re-colors the glyph in Accent.
const tabPinPrefix = "• "

// tabBar returns the rendered tab bar for an editor pane fitting width cells,
// and whether the bar (rather than the plain title) should be shown.
func (m Model) tabBar(inst *pane.Instance, width int) (string, bool) {
	if m.zen {
		// Zen (#359): no tab bar; the plain single-document title renders.
		return "", false
	}
	if inst.TabCount() < 2 && !m.tabsAlwaysShow() {
		return "", false
	}
	return renderTabBar(tabLabels(inst), inst.ActiveTab(), width, m.pal()), true
}

// tabsAlwaysShow reads editor.tabs.always_show live from the config, so the
// settings toggle applies without restart.
func (m Model) tabsAlwaysShow() bool {
	v, ok := m.host.Config().Get("editor.tabs.always_show")
	return ok && v == "true"
}

// tabLabels builds one display label per tab: the file basename, a directory
// suffix when another tab shares that basename ("main.go — cmd/ike"), a dirty
// marker (●), a stale marker (!, file changed on disk while dirty, 0140) and
// a pin prefix (•, #1172) on pinned tabs.
func tabLabels(inst *pane.Instance) []string {
	n := inst.TabCount()
	names := make([]string, n)
	counts := map[string]int{}
	for i := 0; i < n; i++ {
		name := "untitled"
		if t := inst.Tab(i); t != nil && t.IsTerminal() {
			// Terminal tabs (#573) label themselves: OSC title or shell
			// name; a tool session (#741) keeps its tool glyph (#836).
			if tt := t.Terminal(); tt != nil && tt.Tool() != "" {
				name = "⚙ " + t.Title()
			} else {
				name = "⌨ " + t.Title()
			}
		} else if t := inst.Tab(i); t != nil && t.Content() != nil {
			// Content tabs (#1778) label themselves too: a kind glyph plus
			// the content's short title, so a preview tab of README.md is
			// told apart from an editor tab of the same file.
			name = contentTabGlyph(t.Content().Kind()) + t.Title()
		} else if p := inst.TabPath(i); p != "" {
			// TabPath, not the editor's path: labelling a restored tab must
			// not read its file (#2177) — the strip renders every tab, which
			// would undo the lazy restore on the first frame.
			name = baseName(p)
			// A merged rotation set (#1996) reduces to the live log's name,
			// which is also the name of the plain file it was merged from:
			// label it as the timeline it is.
			if t, ok := mergedLogTitle(p); ok {
				name = t
			}
		}
		names[i] = name
		counts[name]++
	}
	labels := make([]string, n)
	for i := 0; i < n; i++ {
		ed := inst.TabEditor(i)
		label := names[i]
		if inst.TabPinned(i) {
			label = tabPinPrefix + label
		}
		if ed == nil {
			labels[i] = label // terminal tab: no dirty/stale markers
			continue
		}
		if path := inst.TabPath(i); counts[names[i]] > 1 && path != "" {
			if dir := filepath.Dir(displayPath(path)); dir != "" && dir != "." {
				label += " — " + dir
			}
		}
		if _, deferred := inst.TabDeferredView(i); deferred {
			// Nothing is loaded yet (#2177), so none of the document markers
			// below can be true — and asking would read the file.
			labels[i] = label
			continue
		}
		if ed.ReadOnly() {
			label += " [RO]" // an archive-entry preview cannot be saved (#1762)
		}
		if ed.Dirty() {
			label += " ●"
		}
		if ed.Stale() {
			label += "!"
		}
		labels[i] = label
	}
	return labels
}

// contentTabGlyph is the per-kind marker of a content tab's label (#1778),
// the ⚙/⌨ convention terminal tabs already use.
func contentTabGlyph(k pane.Kind) string {
	switch k {
	case pane.KindMarkdown:
		return "◫ "
	case pane.KindImage:
		return "▣ "
	case pane.KindArchive:
		return "❒ "
	case pane.KindData:
		return "▤ "
	case pane.KindDiff:
		return "⇄ "
	case pane.KindHTTP:
		return "⇅ "
	}
	return ""
}

// fitTabLabels ellipsizes over-long labels to tabLabelCap cells (#2151), the
// widths every bar computation works from. It is idempotent — an already
// capped label passes through unchanged — so callers may apply it freely, and
// truncating from the right keeps a pinned tab's "• " prefix intact.
func fitTabLabels(labels []string) []string {
	out := make([]string, len(labels))
	for i, l := range labels {
		if ansi.StringWidth(l) > tabLabelCap {
			l = ansi.Truncate(l, tabLabelCap, tabEllipsis)
		}
		out[i] = l
	}
	return out
}

// tabOverflowMark is the indicator standing in for the tabs hidden beyond one
// end of the window (#2151): "+7" for seven elided tabs, empty for none. It
// replaces the bare … the bar used to show, so the count of what is out of
// sight is readable without opening the tab picker.
func tabOverflowMark(hidden int) string {
	if hidden < 1 {
		return ""
	}
	return "+" + strconv.Itoa(hidden)
}

// renderTabBar lays the labels out in one row of at most width cells: labels
// joined by │ separators, the active label highlighted via theme slots. When
// the row overflows, a window of tabs around the active one is shown and a
// "+N" on either end counts the tabs elided there.
func renderTabBar(labels []string, active, width int, pal *theme.Palette) string {
	if len(labels) == 0 || width < 1 {
		return ""
	}
	if active < 0 || active >= len(labels) {
		active = 0
	}
	labels = fitTabLabels(labels)
	lo, hi := tabWindow(labels, active, width)

	activeStyle := lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
	tabStyle := lipgloss.NewStyle().Foreground(pal.Foreground)
	frameStyle := lipgloss.NewStyle().Foreground(pal.Border)
	pinStyle := lipgloss.NewStyle().Foreground(pal.Accent)

	left, right := tabEnds(labels, lo, hi, width)
	var b strings.Builder
	if left != "" {
		b.WriteString(frameStyle.Render(left))
	}
	for i := lo; i <= hi; i++ {
		if i > lo {
			b.WriteString(frameStyle.Render("│"))
		}
		style := tabStyle
		if i == active {
			style = activeStyle
		}
		label := labels[i]
		withClose := true
		if lo == hi {
			// Even the active tab alone may overflow a narrow pane: the
			// label must fit width minus its padding and any end ellipses;
			// the ✕ renders only when the segment still has room for it.
			room := loneTabRoom(labels, lo, width)
			if ansi.StringWidth(label)+tabCloseW > room {
				withClose = false
				label = ansi.Truncate(label, max(room, 1), tabEllipsis)
			}
		}
		if rest, isPinned := strings.CutPrefix(label, tabPinPrefix); isPinned {
			// The pin glyph (#1172) renders in Accent; it occupies the same
			// cells the plain-label rendering would, so hit-testing and the
			// window math need no special case.
			b.WriteString(style.Render(" ") + pinStyle.Render(strings.TrimSuffix(tabPinPrefix, " ")) + style.Render(" "+rest+" "))
		} else {
			b.WriteString(style.Render(" " + label + " "))
		}
		if withClose {
			// The close button (#1128) is muted like the frame so labels
			// stay the visually dominant text.
			b.WriteString(frameStyle.Render(tabCloseGlyph) + " ")
		}
	}
	if right != "" {
		b.WriteString(frameStyle.Render(right))
	}
	return b.String()
}

// loneTabRoom is the label room of a bar showing a single (lo == hi) segment:
// the width minus the segment's own padding and any end overflow indicators.
func loneTabRoom(labels []string, lo, width int) int {
	left, right := tabEnds(labels, lo, lo, width)
	return width - 2 - len(left) - len(right)
}

// tabEnds are the window's end overflow indicators as drawn (#2151): "+N"
// counting the tabs hidden left of lo and right of hi, empty where nothing is
// hidden. On a bar too narrow to carry both an indicator and a one-cell label
// segment, the indicators are dropped — showing the active tab beats counting
// what is not shown — so every geometry consumer must ask here rather than
// derive the widths itself.
func tabEnds(labels []string, lo, hi, width int) (string, string) {
	left, right := "", ""
	if lo > 0 {
		left = tabOverflowMark(lo)
	}
	if hi < len(labels)-1 {
		right = tabOverflowMark(len(labels) - 1 - hi)
	}
	if width-len(left)-len(right) < 3 {
		return "", ""
	}
	return left, right
}

// tabEndsWidth is the cell cost the window math budgets for the end
// indicators — "+N" markers whose width grows with the hidden count, unlike
// the single-cell … they replaced.
func tabEndsWidth(labels []string, lo, hi, width int) int {
	left, right := tabEnds(labels, lo, hi, width)
	return len(left) + len(right)
}

// tabAt resolves a bar-local x cell to the tab index rendered there, or -1 for
// the cells between and beyond tabs (ellipses, separators, trailing space).
func tabAt(labels []string, active, width, x int) int {
	idx, _ := tabHit(labels, active, width, x)
	return idx
}

// tabHit resolves a bar-local x cell to the tab index rendered there and
// whether the cell is that segment's ✕ close zone (#1128); idx -1 for the
// cells between and beyond tabs (ellipses, separators, trailing space). It
// mirrors renderTabBar's geometry exactly, so clicks land on what is drawn.
func tabHit(labels []string, active, width, x int) (int, bool) {
	if len(labels) == 0 || x < 0 || x >= width {
		return -1, false
	}
	if active < 0 || active >= len(labels) {
		active = 0
	}
	labels = fitTabLabels(labels)
	lo, hi := tabWindow(labels, active, width)
	left, _ := tabEnds(labels, lo, hi, width)
	pos := len(left) // the left "+N" indicator's cells
	if x < pos {
		return -1, false
	}
	if lo == hi {
		// A lone (possibly truncated) segment owns the rest of the bar; its
		// ✕ exists only when the full label left room for it (renderTabBar).
		lw := ansi.StringWidth(labels[lo])
		if lw+tabCloseW <= loneTabRoom(labels, lo, width) {
			return lo, x == pos+1+lw+1
		}
		return lo, false
	}
	for i := lo; i <= hi; i++ {
		if i > lo {
			if x == pos {
				return -1, false // separator cell
			}
			pos++
		}
		lw := ansi.StringWidth(labels[i])
		w := lw + 2 + tabCloseW
		if x < pos+w {
			return i, x == pos+1+lw+1 // the ✕ cell after the label's pad
		}
		pos += w
	}
	return -1, false
}

// tabBarHit resolves an absolute mouse cell to the editor pane and tab index
// whose visible tab-bar segment it lands on, plus whether the cell is that
// segment's ✕ close zone (#1128).
func (m Model) tabBarHit(x, y int) (string, int, bool, bool) {
	for key, r := range m.lay.Panes {
		if y != r.Y+1 || x < r.X+paneContentX || x >= r.X+r.W-paneContentX {
			continue
		}
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		if inst.TabCount() < 2 && !m.tabsAlwaysShow() {
			continue // the row shows the plain title, not a bar
		}
		idx, onClose := tabHit(tabLabels(inst), inst.ActiveTab(), r.W-paneChromeW, x-(r.X+paneContentX))
		if idx < 0 {
			return "", 0, false, false
		}
		return key, idx, onClose, true
	}
	return "", 0, false, false
}

// tabWindow picks the run of tabs [lo, hi] to show: starting from the active
// tab it grows rightward then leftward while the row — separators and any
// end overflow indicators included — still fits width. The active tab is
// therefore always inside the window, however many tabs the pane holds.
func tabWindow(labels []string, active, width int) (int, int) {
	labels = fitTabLabels(labels)
	ws := make([]int, len(labels))
	for i, l := range labels {
		// One padding space each side plus the ✕ close zone (#1128).
		ws[i] = ansi.StringWidth(l) + 2 + tabCloseW
	}
	need := func(lo, hi int) int {
		w := 0
		for i := lo; i <= hi; i++ {
			w += ws[i]
		}
		w += hi - lo // one │ between neighbours
		return w + tabEndsWidth(labels, lo, hi, width)
	}
	lo, hi := active, active
	for {
		switch {
		case hi+1 < len(labels) && need(lo, hi+1) <= width:
			hi++
		case lo > 0 && need(lo-1, hi) <= width:
			lo--
		default:
			return lo, hi
		}
	}
}
