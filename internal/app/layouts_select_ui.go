package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/layout"
	"ike/internal/ui"
)

// layouts_select_ui.go is the pane-selection step of window.saveLayout
// (#1568): before the name prompt, a floating mini-map of the current split
// tree lets the user pick which panes the layout pins. Deselected panes are
// not stored — the snapshot keeps a flexible placeholder in their place, and
// applying the layout later flows every unsaved pane into that region. With
// everything selected (the default) the save is a full snapshot, like before.

// layoutSelectState is the open selection dialog: per-leaf checkbox state,
// the highlighted leaf, and the validation hint.
type layoutSelectState struct {
	sel   map[string]bool
	focus string
	err   string
}

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
	m.shell.SetContent(ui.ModelContent{Heading: "Save window layout — choose panes", Body: m.layoutSelectBody})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// layoutSelectOpen reports whether the shell shows the selection mini-map.
func (m Model) layoutSelectOpen() bool { return m.layoutSelect != nil && m.shell.IsOpen() }

// layoutSelectMapSize picks the mini-map viewport from the window size: wide
// enough for labels, small enough to read as a map.
func layoutSelectMapSize(w, h int) (int, int) {
	mw := w - 20
	if mw > 64 {
		mw = 64
	}
	if mw < 24 {
		mw = 24
	}
	mh := h - 14
	if mh > 18 {
		mh = 18
	}
	if mh < 9 {
		mh = 9
	}
	return mw, mh
}

// layoutSelectBody renders the mini-map: every pane a bordered cell (double
// border on the highlighted one) with its checkbox and label, plus the legend.
func (m Model) layoutSelectBody() string {
	ls := m.layoutSelect
	tree := m.activeWS().Tree
	w, h := layoutSelectMapSize(m.width, m.height)
	lay := layout.Compute(tree, layout.Rect{W: w, H: h})
	canvas := make([][]rune, h)
	for y := range canvas {
		canvas[y] = make([]rune, w)
		for x := range canvas[y] {
			canvas[y][x] = ' '
		}
	}
	for _, key := range layout.Leaves(tree) {
		r := lay.Panes[key]
		drawMiniBox(canvas, r, key == ls.focus)
		if r.W < 6 || r.H < 3 {
			continue // too small for a label; the border alone marks the pane
		}
		box := "[ ] "
		if ls.sel[key] {
			box = "[x] "
		}
		label := box + m.paneLabel(key)
		putMiniText(canvas, r.X+2, r.Y+1, r.W-4, label)
	}
	var b strings.Builder
	b.WriteString("Checked panes are pinned by the layout; unchecked ones are left\n")
	b.WriteString("flexible — applying the layout spreads them over the remaining space.\n\n")
	for _, row := range canvas {
		b.WriteString(strings.TrimRight(string(row), " "))
		b.WriteByte('\n')
	}
	if ls.err != "" {
		b.WriteString("\n" + ls.err)
	}
	b.WriteString("\n  [space] toggle   [a] all   [n] none   [hjkl/arrows] move   [enter] continue   [esc] cancel")
	return b.String()
}

// drawMiniBox draws a cell border clipped to the canvas; the focused pane gets
// a double border.
func drawMiniBox(canvas [][]rune, r layout.Rect, focused bool) {
	hr, vr, tl, tr, bl, br := '─', '│', '┌', '┐', '└', '┘'
	if focused {
		hr, vr, tl, tr, bl, br = '═', '║', '╔', '╗', '╚', '╝'
	}
	x2, y2 := r.X+r.W-1, r.Y+r.H-1
	set := func(x, y int, c rune) {
		if y >= 0 && y < len(canvas) && x >= 0 && x < len(canvas[y]) {
			canvas[y][x] = c
		}
	}
	if r.W < 2 || r.H < 2 {
		set(r.X, r.Y, vr)
		return
	}
	for x := r.X + 1; x < x2; x++ {
		set(x, r.Y, hr)
		set(x, y2, hr)
	}
	for y := r.Y + 1; y < y2; y++ {
		set(r.X, y, vr)
		set(x2, y, vr)
	}
	set(r.X, r.Y, tl)
	set(x2, r.Y, tr)
	set(r.X, y2, bl)
	set(x2, y2, br)
}

// putMiniText writes s at (x, y) truncated to max cells.
func putMiniText(canvas [][]rune, x, y, max int, s string) {
	if y < 0 || y >= len(canvas) {
		return
	}
	for i, r := range []rune(s) {
		if i >= max || x+i >= len(canvas[y]) {
			return
		}
		canvas[y][x+i] = r
	}
}

// updateLayoutSelect consumes every key while the mini-map is open: hjkl and
// the arrows move spatially between panes, space toggles, a/n select
// all/none, enter carries the selection into the name prompt, esc cancels.
func (m Model) updateLayoutSelect(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ls := m.layoutSelect
	move := func(dir Direction) {
		w, h := layoutSelectMapSize(m.width, m.height)
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
