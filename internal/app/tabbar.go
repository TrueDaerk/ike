package app

import (
	"path/filepath"
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

// tabEllipsis marks tabs hidden beyond either end of the bar window.
const tabEllipsis = "…"

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
// marker (●) and a stale marker (!, file changed on disk while dirty, 0140).
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
		} else if ed := inst.TabEditor(i); ed != nil && ed.HasFile() {
			name = baseName(ed.Path())
		}
		names[i] = name
		counts[name]++
	}
	labels := make([]string, n)
	for i := 0; i < n; i++ {
		ed := inst.TabEditor(i)
		label := names[i]
		if ed == nil {
			labels[i] = label // terminal tab: no dirty/stale markers
			continue
		}
		if counts[names[i]] > 1 && ed.HasFile() {
			if dir := filepath.Dir(displayPath(ed.Path())); dir != "" && dir != "." {
				label += " — " + dir
			}
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

// renderTabBar lays the labels out in one row of at most width cells: labels
// joined by │ separators, the active label highlighted via theme slots. When
// the row overflows, a window of tabs around the active one is shown and a …
// on either end marks the tabs elided there.
func renderTabBar(labels []string, active, width int, pal *theme.Palette) string {
	if len(labels) == 0 || width < 1 {
		return ""
	}
	if active < 0 || active >= len(labels) {
		active = 0
	}
	lo, hi := tabWindow(labels, active, width)

	activeStyle := lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
	tabStyle := lipgloss.NewStyle().Foreground(pal.Foreground)
	frameStyle := lipgloss.NewStyle().Foreground(pal.Border)

	var b strings.Builder
	if lo > 0 {
		b.WriteString(frameStyle.Render(tabEllipsis))
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
		if lo == hi {
			// Even the active tab alone may overflow a narrow pane: the
			// label must fit width minus its padding and any end ellipses.
			room := width - 2
			if lo > 0 {
				room--
			}
			if hi < len(labels)-1 {
				room--
			}
			label = ansi.Truncate(label, max(room, 1), tabEllipsis)
		}
		b.WriteString(style.Render(" " + label + " "))
	}
	if hi < len(labels)-1 {
		b.WriteString(frameStyle.Render(tabEllipsis))
	}
	return b.String()
}

// tabAt resolves a bar-local x cell to the tab index rendered there, or -1 for
// the cells between and beyond tabs (ellipses, separators, trailing space). It
// mirrors renderTabBar's geometry exactly, so clicks land on what is drawn.
func tabAt(labels []string, active, width, x int) int {
	if len(labels) == 0 || x < 0 || x >= width {
		return -1
	}
	if active < 0 || active >= len(labels) {
		active = 0
	}
	lo, hi := tabWindow(labels, active, width)
	pos := 0
	if lo > 0 {
		pos++ // left ellipsis cell
	}
	if x < pos {
		return -1
	}
	if lo == hi {
		// A lone (possibly truncated) segment owns the rest of the bar.
		return lo
	}
	for i := lo; i <= hi; i++ {
		if i > lo {
			if x == pos {
				return -1 // separator cell
			}
			pos++
		}
		w := ansi.StringWidth(labels[i]) + 2
		if x < pos+w {
			return i
		}
		pos += w
	}
	return -1
}

// tabBarHit resolves an absolute mouse cell to the editor pane and tab index
// whose visible tab-bar segment it lands on.
func (m Model) tabBarHit(x, y int) (string, int, bool) {
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
		idx := tabAt(tabLabels(inst), inst.ActiveTab(), r.W-paneChromeW, x-(r.X+paneContentX))
		if idx < 0 {
			return "", 0, false
		}
		return key, idx, true
	}
	return "", 0, false
}

// tabWindow picks the run of tabs [lo, hi] to show: starting from the active
// tab it grows rightward then leftward while the row — separators and any
// end ellipses included — still fits width.
func tabWindow(labels []string, active, width int) (int, int) {
	ws := make([]int, len(labels))
	for i, l := range labels {
		ws[i] = ansi.StringWidth(l) + 2 // one padding space each side
	}
	need := func(lo, hi int) int {
		w := 0
		for i := lo; i <= hi; i++ {
			w += ws[i]
		}
		w += hi - lo // one │ between neighbours
		if lo > 0 {
			w++
		}
		if hi < len(labels)-1 {
			w++
		}
		return w
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
