package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/layout"
	"ike/internal/overlay"
	"ike/internal/pane"
)

// layouts_select_ui.go is the pane-selection step of window.saveLayout
// (#1568): before the name prompt, a floating mini-map of the current split
// tree lets the user pick which panes the layout pins. Deselected panes are
// not stored — the snapshot keeps a flexible placeholder in their place, and
// applying the layout later flows every unsaved pane into that region. With
// everything selected (the default) the save is a full snapshot, like before.
// Selection reads by color (#1570): pinned cells fill with the theme's
// selection colors, deselected ones render dim, and the highlighted cell
// carries an accent double border. Cells also toggle on left click.

// layoutSelectState is the open selection dialog: per-leaf checkbox state,
// the highlighted leaf, and the validation hint. mapW/mapH/canvasTop record
// the mini-map geometry of the last render so clicks and keyboard moves map
// against exactly what is on screen.
type layoutSelectState struct {
	sel       map[string]bool
	focus     string
	err       string
	mapW      int
	mapH      int
	canvasTop int
}

// layoutSelectContent implements ui.Content (not ModelContent) so the body
// learns the shell's content width budget and the mini-map fills it (#1570).
type layoutSelectContent struct{ m Model }

// Title implements ui.Content.
func (c layoutSelectContent) Title() string { return "Save window layout — choose panes" }

// Render implements ui.Content.
func (c layoutSelectContent) Render(width int) string { return c.m.layoutSelectBody(width) }

// startLayoutSelect opens the mini-map, everything selected, the workspace's
// focused pane highlighted.
func (m *Model) startLayoutSelect() {
	ws := m.activeWS()
	if ws.Tree == nil {
		return
	}
	leaves := layout.Leaves(ws.Tree)
	if len(leaves) == 0 {
		return
	}
	sel := make(map[string]bool, len(leaves))
	for _, key := range leaves {
		sel[key] = true
	}
	focus := ws.Panes.Focused()
	if !sel[focus] {
		focus = leaves[0]
	}
	m.layoutSaveSel = nil
	m.layoutSelect = &layoutSelectState{sel: sel, focus: focus}
	m.shell.SetContent(layoutSelectContent{m: *m})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// layoutSelectOpen reports whether the shell shows the selection mini-map.
func (m Model) layoutSelectOpen() bool { return m.layoutSelect != nil && m.shell.IsOpen() }

// layoutSelectBody renders the mini-map across the full content width: every
// pane a bordered cell colored by its selection state, plus the legend.
func (m Model) layoutSelectBody(width int) string {
	ls := m.layoutSelect
	if ls == nil || width < 10 {
		return ""
	}
	tree := m.activeWS().Tree
	pal := m.pal()
	mapH := m.height - 16
	if mapH > 20 {
		mapH = 20
	}
	if mapH < 8 {
		mapH = 8
	}
	ls.mapW, ls.mapH, ls.canvasTop = width, mapH, 3
	lay := layout.Compute(tree, layout.Rect{W: width, H: mapH})
	blank := strings.Repeat(" ", width)
	rows := make([]string, mapH)
	for i := range rows {
		rows[i] = blank
	}
	canvas := strings.Join(rows, "\n")
	for _, key := range layout.Leaves(tree) {
		r := lay.Panes[key]
		canvas = overlay.Place(canvas, m.layoutSelectCell(key, r, ls), r.X, r.Y, width, mapH)
	}
	dim := lipgloss.NewStyle().Foreground(pal.Hint)
	var b strings.Builder
	b.WriteString(dim.Render("Highlighted panes are pinned by the layout; dim ones are left") + "\n")
	b.WriteString(dim.Render("flexible — applying the layout spreads them over the remaining space.") + "\n\n")
	b.WriteString(canvas)
	b.WriteByte('\n')
	if ls.err != "" {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(pal.Error).Render(ls.err))
	}
	legend := "[space/click] toggle   [a] all   [n] none   [hjkl/arrows] move   [enter] continue   [esc] cancel"
	b.WriteString("\n" + lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Foreground(pal.Hint).Render(legend))
	return b.String()
}

// layoutSelectCell renders one pane cell: selection fills it with the theme's
// selection colors plus a ✓, deselection dims it, and the highlighted cell
// carries an accent double border.
func (m Model) layoutSelectCell(key string, r layout.Rect, ls *layoutSelectState) string {
	pal := m.pal()
	if r.W < 2 || r.H < 2 {
		st := lipgloss.NewStyle().Foreground(pal.Hint)
		if ls.sel[key] {
			st = lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText)
		}
		return st.Render("▪")
	}
	border, borderCol := lipgloss.NormalBorder(), pal.Border
	if key == ls.focus {
		border, borderCol = lipgloss.DoubleBorder(), pal.BorderFocus
	}
	// lipgloss v2 sizes are border-box: Width/Height include the border cells.
	st := lipgloss.NewStyle().Border(border).BorderForeground(borderCol).Width(r.W).Height(r.H)
	label := m.layoutSelectLabel(key)
	if ls.sel[key] {
		st = st.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
		label = "✓ " + label
	} else {
		st = st.Foreground(pal.Hint)
	}
	return st.Render(ansi.Truncate(" "+label, r.W-2, ""))
}

// layoutSelectLabel is the cell label: a configured tool pane (#741) shows
// its tool name instead of TERMINAL; everything else follows paneLabel.
func (m Model) layoutSelectLabel(key string) string {
	if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindTerminal {
		if tool := inst.Terminal().Tool(); tool != "" {
			return strings.ToUpper(tool)
		}
	}
	return m.paneLabel(key)
}

// layoutSelectClick maps a content-local click into the mini-map: hitting a
// cell focuses and toggles it. Coordinates come from the last-rendered
// geometry, so the hit test matches what is on screen.
func (m *Model) layoutSelectClick(cx, cy int) {
	ls := m.layoutSelect
	if ls == nil || ls.mapW <= 0 || ls.mapH <= 0 {
		return
	}
	cy -= ls.canvasTop
	if cx < 0 || cy < 0 || cx >= ls.mapW || cy >= ls.mapH {
		return
	}
	lay := layout.Compute(m.activeWS().Tree, layout.Rect{W: ls.mapW, H: ls.mapH})
	key, ok := lay.PaneAt(cx, cy)
	if !ok {
		return
	}
	ls.focus = key
	ls.sel[key] = !ls.sel[key]
	ls.err = ""
}

// updateLayoutSelect consumes every key while the mini-map is open: hjkl and
// the arrows move spatially between panes, space toggles, a/n select
// all/none, enter carries the selection into the name prompt, esc cancels.
func (m Model) updateLayoutSelect(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ls := m.layoutSelect
	move := func(dir Direction) {
		w, h := ls.mapW, ls.mapH
		if w <= 0 || h <= 0 {
			w, h = 60, 15 // before the first render: any proportional viewport
		}
		lay := layout.Compute(m.activeWS().Tree, layout.Rect{W: w, H: h})
		if best := focusTarget(lay.Panes, ls.focus, dir); best != "" {
			ls.focus = best
		}
	}
	switch msg.String() {
	case "h", "left":
		move(DirLeft)
	case "l", "right":
		move(DirRight)
	case "k", "up":
		move(DirUp)
	case "j", "down":
		move(DirDown)
	case "space", " ":
		ls.sel[ls.focus] = !ls.sel[ls.focus]
		ls.err = ""
	case "a":
		for key := range ls.sel {
			ls.sel[key] = true
		}
		ls.err = ""
	case "n":
		for key := range ls.sel {
			ls.sel[key] = false
		}
	case "enter":
		selected := 0
		for _, on := range ls.sel {
			if on {
				selected++
			}
		}
		if selected == 0 {
			ls.err = "select at least one pane"
			return m, nil
		}
		m.layoutSaveSel = ls.sel
		m.layoutSelect = nil
		m.startLayoutSavePrompt()
		return m, nil
	case "esc":
		m.layoutSelect = nil
		m.shell.Close()
		return m, nil
	}
	return m, nil
}
