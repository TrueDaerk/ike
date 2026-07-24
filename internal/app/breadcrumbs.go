package app

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	ilsp "ike/internal/lsp"
	"ike/internal/pane"
	"ike/internal/theme"
)

// breadcrumbs.go renders the editor breadcrumbs bar (#1153, MVP of #31): a
// one-line chrome row under an editor pane's tab/title row showing
// `file ▸ symbol ▸ child` — the documentSymbol chain enclosing the cursor.
// The data is the same hierarchical tree the Structure pane consumes, cached
// app-side per path (docSymbols) by applyDocumentSymbols; the chain is
// derived at render time, so cursor moves need no extra requests. The row
// only exists while symbol data is present for the file (no provider / no
// reply → the editor keeps the row), and it consumes one interior line:
// layout() adds it to the pane's vertical chrome and contentYOff() shifts
// every editor-local mouse translation by it. The settled Update pass
// (syncBreadcrumbLayout) re-runs layout when a pane's row appears or
// disappears outside a layout event (data arrival, tab switch, config
// toggle). Clicking a symbol segment jumps there through openPathAt, so nav
// history records it like a definition jump; the leading file segment is
// informational only.

// crumbSep separates breadcrumb segments; crumbLead marks segments elided at
// the front when the row overflows (the deepest segments win).
const (
	crumbSep   = " ▸ "
	crumbLead  = "… ▸ "
	crumbSepW  = 3
	crumbLeadW = 4
)

// breadcrumbsOn reads editor.breadcrumbs live from the config so the settings
// toggle applies without restart. Default on (the JetBrains default): only an
// explicit "false" disables the bar.
func (m Model) breadcrumbsOn() bool {
	v, ok := m.host.Config().Get("editor.breadcrumbs")
	return !ok || v != "false"
}

// breadcrumbRows reports how many chrome rows the breadcrumbs bar occupies in
// inst: 1 when the bar renders, else 0. The predicate must agree between
// layout() (interior height), renderPane (drawing) and the mouse translation
// (contentYOff), so it is the single source of truth: an editor pane showing
// a file tab whose path has cached symbol data, bar enabled, not in zen.
func (m Model) breadcrumbRows(inst *pane.Instance) int {
	if inst == nil || m.zen || inst.Kind() != pane.KindEditor || inst.ActiveTerminal() != nil {
		return 0
	}
	if !m.breadcrumbsOn() {
		return 0
	}
	ed := inst.Editor()
	if ed == nil || !ed.HasFile() {
		return 0
	}
	if len(m.docSymbols[ed.Path()]) == 0 {
		return 0
	}
	return 1
}

// contentYOff is the pane's content-local Y origin: the shared chrome rows
// (border + title) plus the breadcrumbs row when the pane shows one. Every
// absolute→content-local mouse translation for a keyed pane goes through it.
func (m Model) contentYOff(key string) int {
	return paneContentY + m.breadcrumbRows(m.activeWS().Panes.Get(key))
}

// symbolChain returns the chain of symbols enclosing the 0-based cursor line,
// outermost first. It mirrors the Structure pane's Follow: at each level the
// last node whose [Line, EndLine] span contains the cursor is the most
// specific (document order), then its children narrow further.
func symbolChain(syms []ilsp.SymbolNode, line int) []ilsp.SymbolNode {
	var chain []ilsp.SymbolNode
	nodes := syms
	for {
		best := -1
		for i, n := range nodes {
			if n.Line <= line && line <= n.EndLine {
				best = i
			}
		}
		if best < 0 {
			return chain
		}
		chain = append(chain, nodes[best])
		nodes = nodes[best].Children
	}
}

// crumbLabels builds the display segments for inst's active editor: the file
// basename followed by the enclosing symbol names. ok is false when the bar
// does not render for this pane.
func (m Model) crumbLabels(inst *pane.Instance) ([]string, []ilsp.SymbolNode, bool) {
	if m.breadcrumbRows(inst) == 0 {
		return nil, nil, false
	}
	ed := inst.Editor()
	line, _ := ed.Cursor() // 1-based
	chain := symbolChain(m.docSymbols[ed.Path()], line-1)
	labels := make([]string, 0, len(chain)+1)
	labels = append(labels, baseName(ed.Path()))
	for _, n := range chain {
		labels = append(labels, n.Name)
	}
	return labels, chain, true
}

// crumbWindow picks the first visible segment index: segments drop from the
// front (replaced by a leading …) while the row — separators included — still
// overflows width. The deepest segment always stays.
func crumbWindow(labels []string, width int) int {
	total := func(lo int) int {
		w := 0
		for i := lo; i < len(labels); i++ {
			if i > lo {
				w += crumbSepW
			}
			w += ansi.StringWidth(labels[i])
		}
		if lo > 0 {
			w += crumbLeadW
		}
		return w
	}
	lo := 0
	for lo < len(labels)-1 && total(lo) > width {
		lo++
	}
	return lo
}

// renderCrumbRow lays the segments out in one row of at most width cells:
// a leading … for elided front segments, faint separators, the deepest
// segment in the plain foreground and the outer ones muted. A lone segment
// that still overflows is truncated with a trailing ….
func renderCrumbRow(labels []string, width int, pal *theme.Palette) string {
	if len(labels) == 0 || width < 1 {
		return ""
	}
	lo := crumbWindow(labels, width)
	frame := lipgloss.NewStyle().Foreground(pal.Border)
	outer := lipgloss.NewStyle().Foreground(pal.Secondary)
	last := lipgloss.NewStyle().Foreground(pal.Foreground)

	var b strings.Builder
	pos := 0
	if lo > 0 {
		b.WriteString(frame.Render(crumbLead))
		pos += crumbLeadW
	}
	for i := lo; i < len(labels); i++ {
		if i > lo {
			b.WriteString(frame.Render(crumbSep))
			pos += crumbSepW
		}
		label := labels[i]
		if room := width - pos; ansi.StringWidth(label) > room {
			label = ansi.Truncate(label, max(room, 1), "…")
		}
		style := outer
		if i == len(labels)-1 {
			style = last
		}
		b.WriteString(style.Render(label))
		pos += ansi.StringWidth(label)
	}
	return b.String()
}

// crumbHit resolves a row-local x cell to the segment index rendered there,
// or -1 for the leading ellipsis, separators and the cells past the last
// segment. It mirrors renderCrumbRow's geometry exactly.
func crumbHit(labels []string, width, x int) int {
	if len(labels) == 0 || x < 0 || x >= width {
		return -1
	}
	lo := crumbWindow(labels, width)
	pos := 0
	if lo > 0 {
		pos += crumbLeadW
		if x < pos {
			return -1
		}
	}
	for i := lo; i < len(labels); i++ {
		if i > lo {
			if x < pos+crumbSepW {
				return -1
			}
			pos += crumbSepW
		}
		lw := ansi.StringWidth(labels[i])
		if lw > width-pos {
			lw = width - pos // segment truncated at the row edge
		}
		if x < pos+lw {
			return i
		}
		pos += lw
	}
	return -1
}

// breadcrumbRowFor returns the rendered bar for inst fitting width cells, and
// whether the bar renders at all for this pane.
func (m Model) breadcrumbRowFor(inst *pane.Instance, width int) (string, bool) {
	labels, _, ok := m.crumbLabels(inst)
	if !ok {
		return "", false
	}
	return renderCrumbRow(labels, width, m.pal()), true
}

// breadcrumbClick handles a left press on the pane's breadcrumbs row at the
// content-local x cell: a symbol segment jumps to that symbol's position
// through the standard open funnel (nav history records it, #1153); the file
// segment and the cells between segments only keep the focus the press set.
func (m Model) breadcrumbClick(key string, inst *pane.Instance, x int) (tea.Model, tea.Cmd) {
	r, ok := m.lay.Panes[key]
	if !ok {
		return m, nil
	}
	labels, chain, ok := m.crumbLabels(inst)
	if !ok {
		return m, nil
	}
	idx := crumbHit(labels, r.W-paneChromeW, x)
	if idx < 1 || idx > len(chain) {
		return m, nil // miss, or the file segment (informational)
	}
	n := chain[idx-1]
	return m.openPathAt(inst.Editor().Path(), n.Line, n.Col)
}

// syncBreadcrumbLayout runs once per settled Update pass: when any editor
// pane's breadcrumb row appeared or disappeared since the last pass (symbol
// data arrived, the active tab changed, the config toggled, zen), the pane
// interiors are re-sized via layout(). The signature only lists panes showing
// the row, so the steady state — and the no-bar state — recompute nothing.
func (m *Model) syncBreadcrumbLayout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	var b strings.Builder
	for _, key := range m.activeWS().Panes.Keys() {
		if rows := m.breadcrumbRows(m.activeWS().Panes.Get(key)); rows > 0 {
			b.WriteString(key)
			b.WriteString(":")
			b.WriteString(strconv.Itoa(rows))
			b.WriteString(";")
		}
	}
	if sig := b.String(); sig != m.crumbSig {
		m.crumbSig = sig
		m.layout()
	}
}
