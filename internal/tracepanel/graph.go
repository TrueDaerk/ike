package tracepanel

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/agenttrace"
	"ike/internal/theme"
)

// graph.go is the pane's graph view (#2858): the change path
// (agenttrace.BuildPath) as a snake of boxes — prompt, one box per file
// change, answer, next turn — laid out by snake.go and drawn on a cell
// canvas so the mouse hit test reads the geometry that was rendered. One
// box is selected; one change box may be expanded into a detail block
// beneath its row. The tree view (tracepanel.go) stays a key away.

// ViewMode selects what the pane draws: the tree (#2840) or the graph.
type ViewMode int

const (
	// ViewTree is the turn → decision → tool → file tree on hiertree.
	ViewTree ViewMode = iota
	// ViewGraph is the change path as a snake of boxes.
	ViewGraph
)

// String returns the setting value of the mode ("tree" / "graph").
func (v ViewMode) String() string {
	if v == ViewGraph {
		return "graph"
	}
	return "tree"
}

// ParseViewMode maps a setting value onto a mode; anything but "tree" is
// the graph, the default.
func ParseViewMode(s string) ViewMode {
	if s == "tree" {
		return ViewTree
	}
	return ViewGraph
}

// ShowTextMsg asks the root model to show a prompt or answer in full in the
// floating shell, markdown-rendered (enter on a prompt or answer box).
type ShowTextMsg struct {
	Title string
	Text  string
}

// graphState is the graph view's own state, kept beside the tree's so a
// toggle between the views loses nothing.
type graphState struct {
	stops []agenttrace.Stop
	// sel and expanded are stop keys (stable across rebuilds); "" for none.
	sel      string
	expanded string
	// top is the first content row shown; follow keeps the newest turn in
	// view on a live append until the user scrolls up or moves off the last
	// box.
	top    int
	follow bool
	// lastEnter is the key enter last acted on: a second enter on the same
	// change box expands it instead of opening the file again.
	lastEnter string
}

// SetViewMode switches between the tree and the graph; the other view's
// selection and expansion are kept for the way back.
func (m *Model) SetViewMode(v ViewMode) {
	if m.view == v {
		return
	}
	m.view = v
	if v == ViewGraph {
		m.graph.lastEnter = ""
		m.graphEnsureVisible()
	}
}

// ViewMode reports the shown view.
func (m *Model) ViewMode() ViewMode { return m.view }

// SetPath installs the change path of the session the last Set grouped
// (#2858): selection and the expanded box survive by key; while following,
// the selection moves onto the newest box and the newest turn scrolls into
// view. A session that ended settles a pending answer.
func (m *Model) SetPath(stops []agenttrace.Stop) {
	if m.info.Ended {
		agenttrace.Settle(stops)
	}
	g := &m.graph
	g.stops = stops
	if g.expanded != "" && m.stopIndex(g.expanded) < 0 {
		g.expanded = ""
	}
	last := m.lastSelectable()
	switch {
	case last < 0:
		g.sel = ""
	case g.follow || m.stopIndex(g.sel) < 0:
		g.sel = stops[last].Key
	}
	if g.follow {
		m.graphScrollToEnd()
	}
}

// Path returns the change path the last SetPath handed in.
func (m *Model) Path() []agenttrace.Stop { return m.graph.stops }

// CurrentStop returns the selected box of the graph, nil when none.
func (m *Model) CurrentStop() *agenttrace.Stop {
	i := m.stopIndex(m.graph.sel)
	if i < 0 {
		return nil
	}
	return &m.graph.stops[i]
}

// Expanded returns the key of the expanded change box, "" when none.
func (m *Model) Expanded() string { return m.graph.expanded }

// GraphTop returns the first content row the graph shows (tests).
func (m *Model) GraphTop() int { return m.graph.top }

// stopIndex finds a stop by key, -1 when absent.
func (m *Model) stopIndex(key string) int {
	if key == "" {
		return -1
	}
	for i := range m.graph.stops {
		if m.graph.stops[i].Key == key {
			return i
		}
	}
	return -1
}

// lastSelectable is the index of the newest box, -1 on an empty path.
func (m *Model) lastSelectable() int {
	for i := len(m.graph.stops) - 1; i >= 0; i-- {
		if m.graph.stops[i].Selectable() {
			return i
		}
	}
	return -1
}

// stopNode is the tree-node view of a stop: what the host's ask (#2845)
// and the change-feed links read from Current while the graph is shown.
func stopNode(st *agenttrace.Stop) *agenttrace.Node {
	n := &agenttrace.Node{Key: st.Key, Turn: st.Turn, Event: -1, Agent: st.Agent, Label: st.Label, Detail: st.Detail, At: st.At, Error: st.Error, Pending: st.Pending}
	if len(st.Events) > 0 {
		n.Event = st.Events[0]
	}
	switch st.Kind {
	case agenttrace.StopPrompt:
		n.Kind = agenttrace.NodeTurn
	case agenttrace.StopChange:
		n.Kind = agenttrace.NodeFile
		if st.Ref != nil {
			ref := *st.Ref
			n.Ref = &ref
			n.Path = ref.Path
			n.Label = ref.Op.String()
		}
	case agenttrace.StopAnswer:
		n.Kind = agenttrace.NodeDecision
	case agenttrace.StopSeparator:
		n.Kind = agenttrace.NodeSeparator
	}
	return n
}

// graphSelect moves the selection onto a stop by key and reports success.
func (m *Model) graphSelect(key string) bool {
	i := m.stopIndex(key)
	if i < 0 {
		// A tool node key from the feed's back-link names the call; its
		// first change box answers.
		for j := range m.graph.stops {
			if strings.HasPrefix(m.graph.stops[j].Key, key+"/") && m.graph.stops[j].Selectable() {
				i = j
				break
			}
		}
	}
	if i < 0 || !m.graph.stops[i].Selectable() {
		return false
	}
	m.graph.sel = m.graph.stops[i].Key
	m.noteGraphSelection()
	m.graphEnsureVisible()
	return true
}

// noteGraphSelection re-derives follow after a move: on while the newest
// box is selected.
func (m *Model) noteGraphSelection() {
	last := m.lastSelectable()
	m.graph.follow = last >= 0 && m.graph.stops[last].Key == m.graph.sel
}

// graphLayout lays the path out for the pane width.
func (m *Model) graphLayout() Layout {
	g := &m.graph
	boxW, _ := BoxWidth(m.width)
	widths := make([]int, len(g.stops))
	for i := range g.stops {
		if g.stops[i].Selectable() {
			widths[i] = boxW
		} else {
			widths[i] = sepW
		}
	}
	exp := m.stopIndex(g.expanded)
	detailH := 0
	if exp >= 0 {
		detailH = len(m.detailLines(g.stops[exp], m.width))
	}
	return Snake(widths, m.width, exp, detailH)
}

// graphMaxTop is the top row that still fills the body.
func (m *Model) graphMaxTop(l Layout) int {
	return max(0, l.Height-m.treeHeight())
}

// graphScrollToEnd shows the newest rows.
func (m *Model) graphScrollToEnd() {
	if m.width <= 0 {
		return
	}
	m.graph.top = m.graphMaxTop(m.graphLayout())
}

// graphEnsureVisible scrolls so the selected box (and its detail block, if
// expanded) is on screen.
func (m *Model) graphEnsureVisible() {
	if m.width <= 0 {
		return
	}
	l := m.graphLayout()
	i := m.stopIndex(m.graph.sel)
	if i < 0 || i >= len(l.Slots) {
		m.graph.top = min(m.graph.top, m.graphMaxTop(l))
		return
	}
	s := l.Slots[i]
	y0, y1 := s.Y, s.Y+s.H
	if l.DetailY == y1 {
		y1 += l.DetailH
	}
	h := m.treeHeight()
	switch {
	case y0 < m.graph.top:
		m.graph.top = y0
	case y1 > m.graph.top+h:
		m.graph.top = max(y0, y1-h)
	}
	m.graph.top = max(0, min(m.graph.top, m.graphMaxTop(l)))
}

// graphKey handles one key in the graph view; ok reports that the key was
// the graph's. The shared pane keys (r, i, a, D, V, t) are handled by the
// caller.
func (m *Model) graphKey(key string) (tea.Cmd, bool) {
	g := &m.graph
	cur := m.stopIndex(g.sel)
	l := m.graphLayout()
	selectable := func(j int) bool { return j >= 0 && j < len(g.stops) && g.stops[j].Selectable() }
	move := func(j int) {
		if selectable(j) {
			g.sel = g.stops[j].Key
			g.lastEnter = ""
			m.noteGraphSelection()
			m.graphEnsureVisible()
		}
	}
	switch key {
	case "l", "right":
		for j := cur + 1; j < len(g.stops); j++ {
			if selectable(j) {
				move(j)
				break
			}
		}
		return nil, true
	case "h", "left":
		for j := cur - 1; j >= 0; j-- {
			if selectable(j) {
				move(j)
				break
			}
		}
		return nil, true
	case "j", "down":
		move(l.Below(cur, selectable))
		return nil, true
	case "k", "up":
		move(l.Above(cur, selectable))
		return nil, true
	case "home", "g":
		for j := 0; j < len(g.stops); j++ {
			if selectable(j) {
				move(j)
				break
			}
		}
		return nil, true
	case "end", "G":
		move(m.lastSelectable())
		return nil, true
	case "pgdown", "ctrl+d":
		m.graphScroll(m.treeHeight())
		return nil, true
	case "pgup", "ctrl+u":
		m.graphScroll(-m.treeHeight())
		return nil, true
	case "enter":
		if !selectable(cur) {
			return nil, true
		}
		st := &g.stops[cur]
		if st.Kind == agenttrace.StopChange && g.lastEnter == st.Key {
			g.lastEnter = ""
			m.toggleExpand(st.Key)
			return nil, true
		}
		g.lastEnter = st.Key
		return m.openStop(st), true
	case "space":
		if selectable(cur) {
			m.toggleExpand(g.stops[cur].Key)
		}
		return nil, true
	case "esc":
		if g.expanded != "" {
			m.toggleExpand(g.expanded)
			return nil, true
		}
	}
	return nil, false
}

// toggleExpand expands a change box in place (collapsing any other) or
// collapses it; prompt and answer boxes have nothing to expand.
func (m *Model) toggleExpand(key string) {
	i := m.stopIndex(key)
	if i < 0 || m.graph.stops[i].Kind != agenttrace.StopChange {
		return
	}
	if m.graph.expanded == key {
		m.graph.expanded = ""
	} else {
		m.graph.expanded = key
	}
	m.graphEnsureVisible()
}

// openStop is enter on a box: a change opens its file at the line, a prompt
// or answer shows its full text in the shell.
func (m *Model) openStop(st *agenttrace.Stop) tea.Cmd {
	switch st.Kind {
	case agenttrace.StopChange:
		if st.Ref == nil || st.Ref.Path == "" {
			return nil
		}
		ref := *st.Ref
		return func() tea.Msg { return OpenLocationMsg{Path: ref.Path, Line: ref.Line - 1, Col: 0} }
	case agenttrace.StopPrompt, agenttrace.StopAnswer:
		title := "Prompt #" + itoa(st.Turn)
		if st.Kind == agenttrace.StopAnswer {
			title = "Answer #" + itoa(st.Turn)
		}
		text := st.Text
		if text == "" {
			text = "_" + st.Label + "_"
		}
		return func() tea.Msg { return ShowTextMsg{Title: title, Text: text} }
	}
	return nil
}

// graphScroll moves the window by delta rows; scrolling up ends following,
// reaching the bottom with the newest box selected resumes it.
func (m *Model) graphScroll(delta int) {
	l := m.graphLayout()
	maxTop := m.graphMaxTop(l)
	m.graph.top = max(0, min(m.graph.top+delta, maxTop))
	if m.graph.top < maxTop {
		m.graph.follow = false
	} else {
		m.noteGraphSelection()
	}
}

// graphClick selects the box under pane-content-local (x, y); a second
// click within the double-click window opens it.
func (m *Model) graphClick(x, y int) tea.Cmd {
	l := m.graphLayout()
	cy := y - headerRows + m.graph.top
	if y < headerRows || y >= headerRows+m.treeHeight() {
		m.clicks.Reset()
		return nil
	}
	i := l.At(x, cy)
	if i < 0 || !m.graph.stops[i].Selectable() {
		m.clicks.Reset()
		return nil
	}
	double := m.clicks.Double(i, m.now())
	m.graph.sel = m.graph.stops[i].Key
	m.graph.lastEnter = ""
	m.noteGraphSelection()
	m.graphEnsureVisible()
	if !double {
		return nil
	}
	m.clicks.Reset()
	return m.openStop(&m.graph.stops[i])
}

// detailLines is the expanded block of a change box: the call(s) behind
// it, the assistant text that preceded it, the patch summary and the keys.
func (m *Model) detailLines(st agenttrace.Stop, width int) []string {
	var lines []string
	for _, c := range st.Calls {
		status := "ok"
		switch {
		case c.Error:
			status = "✗ error"
		case c.Pending:
			status = "…"
		}
		lines = append(lines, c.Name+" · "+m.display(c.Title)+" · "+status)
	}
	ctx := st.Context
	if len(ctx) > 4 {
		ctx = ctx[len(ctx)-4:]
	}
	for _, c := range ctx {
		lines = append(lines, "› "+c)
	}
	if st.Ref != nil {
		where := m.display(st.Ref.Path)
		if st.Ref.Line > 0 {
			where += ":" + itoa(st.Ref.Line)
		}
		diff := "no diff recorded"
		if st.HasDiff {
			diff = "+" + itoa(st.Added) + " −" + itoa(st.Removed)
		}
		if m.links.Node(st.Key) != "" {
			diff += " · " + linkMark + " change feed"
		}
		lines = append(lines, where+" · "+diff)
	}
	lines = append(lines, "space collapse · enter open · D diff · a ask · V revert")
	for i, l := range lines {
		lines[i] = fitCells(l, max(1, width-2))
	}
	return lines
}

// Canvas cell styles.
const (
	stPlain = iota
	stFaint
	stPrompt
	stEdit
	stCreate
	stDelete
	stAnswer
	stError
	stPending
	stSelected
	stSelectedMuted
	stConn
	stDetail
	stCount
)

// cell is one canvas position: its glyph ("" for the second half of a wide
// rune) and style.
type cell struct {
	ch string
	st int
}

// canvas is the graph's drawing surface.
type canvas struct {
	w, h  int
	cells [][]cell
}

func newCanvas(w, h int) *canvas {
	c := &canvas{w: w, h: h, cells: make([][]cell, h)}
	for y := range c.cells {
		row := make([]cell, w)
		for x := range row {
			row[x] = cell{ch: " "}
		}
		c.cells[y] = row
	}
	return c
}

// put writes s at (x, y) in style st, clipped to the canvas and to maxW
// cells (<= 0 for no limit).
func (c *canvas) put(x, y int, s string, st, maxW int) {
	if y < 0 || y >= c.h {
		return
	}
	used := 0
	for _, r := range s {
		w := ansi.StringWidth(string(r))
		if w <= 0 {
			continue
		}
		if maxW > 0 && used+w > maxW {
			return
		}
		if x >= 0 && x < c.w {
			c.cells[y][x] = cell{ch: string(r), st: st}
			if w == 2 && x+1 < c.w {
				c.cells[y][x+1] = cell{ch: "", st: st}
			}
		}
		x += w
		used += w
	}
}

// fill writes n copies of glyph from (x, y).
func (c *canvas) fill(x, y, n int, glyph string, st int) {
	for i := 0; i < n; i++ {
		c.put(x+i, y, glyph, st, 0)
	}
}

// restyle changes the style of a run of cells.
func (c *canvas) restyle(x, y, n, st int) {
	if y < 0 || y >= c.h {
		return
	}
	for i := x; i < x+n && i < c.w; i++ {
		if i >= 0 {
			c.cells[y][i].st = st
		}
	}
}

// lines renders rows [from, to) with the given styles.
func (c *canvas) lines(from, to int, styles []lipgloss.Style) []string {
	var out []string
	for y := from; y < to && y < c.h; y++ {
		if y < 0 {
			continue
		}
		var sb strings.Builder
		var run strings.Builder
		cur := -1
		flush := func() {
			if run.Len() == 0 {
				return
			}
			if cur == stPlain {
				sb.WriteString(run.String())
			} else {
				sb.WriteString(styles[cur].Render(run.String()))
			}
			run.Reset()
		}
		for _, cl := range c.cells[y] {
			if cl.ch == "" {
				continue
			}
			if cl.st != cur {
				flush()
				cur = cl.st
			}
			run.WriteString(cl.ch)
		}
		flush()
		out = append(out, sb.String())
	}
	return out
}

// fitCells cuts s to w cells, ending a cut string with an ellipsis.
func fitCells(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return ansi.Truncate(s, w-1, "") + "…"
}

// graphStyles builds the canvas styles off the palette.
func (m *Model) graphStyles(pal *theme.Palette) []lipgloss.Style {
	st := make([]lipgloss.Style, stCount)
	st[stPlain] = lipgloss.NewStyle()
	st[stFaint] = lipgloss.NewStyle().Faint(true)
	st[stPrompt] = lipgloss.NewStyle().Foreground(pal.Accent)
	st[stEdit] = lipgloss.NewStyle().Foreground(pal.Warning)
	st[stCreate] = lipgloss.NewStyle().Foreground(pal.Success)
	st[stDelete] = lipgloss.NewStyle().Foreground(pal.Error)
	st[stAnswer] = lipgloss.NewStyle().Foreground(pal.Info)
	st[stError] = lipgloss.NewStyle().Foreground(pal.Error).Bold(true)
	st[stPending] = lipgloss.NewStyle().Faint(true)
	st[stSelected] = lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	st[stSelectedMuted] = lipgloss.NewStyle().Background(pal.SelectionMuted).Bold(true)
	st[stConn] = lipgloss.NewStyle().Foreground(pal.Border)
	st[stDetail] = lipgloss.NewStyle().Foreground(pal.Foreground)
	return st
}

// stopStyle is the border style and glyph of a stop: kinds tell apart by
// glyph as well as colour, so a monochrome terminal still reads the path.
func stopStyle(st agenttrace.Stop) (style int, glyph string) {
	switch st.Kind {
	case agenttrace.StopPrompt:
		return stPrompt, "?"
	case agenttrace.StopAnswer:
		if st.Pending {
			return stPending, "…"
		}
		return stAnswer, "✓"
	case agenttrace.StopChange:
		if st.Error {
			return stError, "✗"
		}
		if st.Ref != nil {
			switch st.Ref.Op {
			case agenttrace.OpCreate:
				return stCreate, "+"
			case agenttrace.OpDelete:
				return stDelete, "✕"
			}
		}
		if st.Pending {
			return stPending, "…"
		}
		return stEdit, "✎"
	}
	return stFaint, "◇"
}

// graphRows draws the path onto a canvas and returns the body's visible
// rows, padded to the body height.
func (m *Model) graphRows(pal *theme.Palette) []string {
	g := &m.graph
	h := m.treeHeight()
	if len(g.stops) == 0 {
		rows := []string{lipgloss.NewStyle().Faint(true).Render("(no events yet)")}
		for len(rows) < h {
			rows = append(rows, "")
		}
		return rows
	}
	l := m.graphLayout()
	g.top = max(0, min(g.top, m.graphMaxTop(l)))
	c := newCanvas(m.width, l.Height)
	for i, st := range g.stops {
		s := l.Slots[i]
		if i > 0 {
			m.drawConnector(c, l.Slots[i-1], s)
		}
		if !st.Selectable() {
			c.put(s.X, s.Y+1, "─◇─", stFaint, s.W)
			continue
		}
		m.drawBox(c, st, s, i)
	}
	if exp := m.stopIndex(g.expanded); exp >= 0 && l.DetailH > 0 {
		for j, line := range m.detailLines(g.stops[exp], m.width) {
			st := stDetail
			if j == 0 || j == l.DetailH-1 {
				st = stFaint
			}
			c.put(1, l.DetailY+j, " "+line, st, m.width-1)
		}
	}
	rows := c.lines(g.top, g.top+h, m.graphStyles(pal))
	for len(rows) < h {
		rows = append(rows, "")
	}
	return rows
}

// drawBox draws one 3-row box: the top border carrying the kind glyph (and
// the Δ mark of a linked change), the label, the detail set into the bottom
// border.
func (m *Model) drawBox(c *canvas, st agenttrace.Stop, s Slot, i int) {
	style, glyph := stopStyle(st)
	w := s.W
	if w < 4 {
		return
	}
	linked := m.links.Node(st.Key) != ""
	// Top border.
	c.put(s.X, s.Y, "┌", style, 0)
	c.put(s.X+1, s.Y, glyph, style, 0)
	c.fill(s.X+2, s.Y, w-3, "─", style)
	if linked && w > 5 {
		c.put(s.X+w-2, s.Y, linkMark, style, 0)
	}
	c.put(s.X+w-1, s.Y, "┐", style, 0)
	// Label row.
	c.put(s.X, s.Y+1, "│", style, 0)
	c.put(s.X+w-1, s.Y+1, "│", style, 0)
	label := fitCells(st.Label, w-2)
	labelSt := stPlain
	if st.Pending && st.Kind == agenttrace.StopAnswer {
		labelSt = stFaint
	}
	c.put(s.X+1, s.Y+1, label, labelSt, w-2)
	if st.Key == m.graph.sel {
		sel := stSelected
		if !m.focused {
			sel = stSelectedMuted
		}
		c.restyle(s.X+1, s.Y+1, w-2, sel)
	}
	// Bottom border with the detail.
	c.put(s.X, s.Y+2, "└", style, 0)
	c.fill(s.X+1, s.Y+2, w-2, "─", style)
	if st.Key == m.graph.expanded {
		c.put(s.X+1, s.Y+2, "┴", style, 0)
	}
	if d := st.Detail; d != "" && w > 6 {
		d = " " + fitCells(d, w-5) + " "
		c.put(s.X+2, s.Y+2, d, stFaint, w-4)
	}
	c.put(s.X+w-1, s.Y+2, "┘", style, 0)
}

// drawConnector joins two consecutive slots: an arrow along the row, or
// the "▼" of a turn in the row between two path rows.
func (m *Model) drawConnector(c *canvas, prev, s Slot) {
	switch {
	case prev.Row != s.Row:
		c.put(s.CenterX(), s.Y-1, "▼", stConn, 0)
	case s.Dir > 0:
		x0 := prev.X + prev.W
		n := s.X - x0
		if n > 0 {
			c.fill(x0, s.Y+1, n-1, "─", stConn)
			c.put(s.X-1, s.Y+1, "▶", stConn, 0)
		}
	default:
		x0 := s.X + s.W
		n := prev.X - x0
		if n > 0 {
			c.put(x0, s.Y+1, "◀", stConn, 0)
			c.fill(x0+1, s.Y+1, n-1, "─", stConn)
		}
	}
}

// graphHint is the key line under the graph.
const graphHint = "h/l along the path · j/k rows · enter open · space expand · t tree · a ask · r rescan · Δ: D diff · V revert"
