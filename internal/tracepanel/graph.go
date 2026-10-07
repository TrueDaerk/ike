package tracepanel

import (
	"image/color"
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
	case agenttrace.StopRewind:
		n.Kind = agenttrace.NodeRewind
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
	var breaks []int
	seenPrompt := false
	for i := range g.stops {
		if g.stops[i].Selectable() {
			widths[i] = boxW
		} else {
			widths[i] = sepW
		}
		// Every question but the first starts a new row (#2866); a
		// compaction marker right before the question starts the row
		// with it instead of taking a row of its own (#2901).
		if g.stops[i].Kind == agenttrace.StopPrompt {
			if seenPrompt {
				b := i
				if i > 0 && !g.stops[i-1].Selectable() {
					b = i - 1
				}
				breaks = append(breaks, b)
			}
			seenPrompt = true
		}
	}
	exp := m.stopIndex(g.expanded)
	detailH := 0
	if exp >= 0 {
		detailH = len(m.detailLines(g.stops[exp], m.width)) + detailFrameH
	}
	return Snake(widths, breaks, m.width, exp, detailH)
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

// graphEnsureVisible scrolls so the selected box (with the turn rule above
// a break, and its detail block, if expanded) is on screen.
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
	if l.RowBreak(i) {
		y0 = s.Y - 2
	}
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
	case "n":
		move(m.stepStop(cur, 1, selectable))
		return nil, true
	case "p":
		move(m.stepStop(cur, -1, selectable))
		return nil, true
	case "l", "right":
		move(l.Right(cur, selectable))
		return nil, true
	case "h", "left":
		move(l.Left(cur, selectable))
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
		if st.Kind == agenttrace.StopRewind {
			m.toggleExpand(st.Key)
			return nil, true
		}
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

// stepStop walks the path from index from in direction dir (+1 / -1) to
// the first stop ok accepts, -1 at the end — n / p (#2878) and the diff
// view's change stepping (#2910).
func (m *Model) stepStop(from, dir int, ok func(int) bool) int {
	for j := from + dir; j >= 0 && j < len(m.graph.stops); j += dir {
		if ok(j) {
			return j
		}
	}
	return -1
}

// toggleExpand expands a change box — or a rewind marker (#2860), whose
// block lists the abandoned branch — in place (collapsing any other) or
// collapses it; prompt and answer boxes have nothing to expand.
func (m *Model) toggleExpand(key string) {
	i := m.stopIndex(key)
	if i < 0 || (m.graph.stops[i].Kind != agenttrace.StopChange && m.graph.stops[i].Kind != agenttrace.StopRewind) {
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

// detailLines is the expanded drawer's content for a change box: the call(s)
// behind it, the assistant text that preceded it and the patch summary; the
// key hint sits in the drawer's frame (drawerHint).
func (m *Model) detailLines(st agenttrace.Stop, width int) []string {
	var lines []string
	if st.Kind == agenttrace.StopRewind {
		return m.rewindLines(st, width)
	}
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
		diff := "line counts unknown"
		if st.HasDiff {
			diff = "+" + itoa(st.Added) + " −" + itoa(st.Removed)
		}
		if m.links.Node(st.Key) != "" {
			diff += " · " + linkMark + " change feed"
		}
		lines = append(lines, where+" · "+diff)
	}
	for i, l := range lines {
		lines[i] = fitCells(l, max(1, width-2*drawerPad))
	}
	return lines
}

// drawerPad is the cells the drawer's frame and padding take on each side
// of a detail line.
const drawerPad = 2

// Canvas cell styles (#2901): a cell carries one foreground style and,
// independently, one selection background, so the selected box keeps its
// kind colour on the frame while the whole box is lifted off the pane.
const (
	stPlain = iota
	// stSecondary is the secondary text: detail rows, the separator marker,
	// the drawer's key hint, a drawer's context lines.
	stSecondary
	stPrompt
	stEdit
	stCreate
	stDelete
	stAnswer
	// stImplicit is an answer the turn never spoke (it ended on a tool
	// call): the answer hue, dimmed.
	stImplicit
	stError
	stPending
	stRewind
	// stSelText is the text of the focused selected box.
	stSelText
	// stConn is the path: arrows, turns, elbows, the rule crossing.
	stConn
	// stRule is the turn rule between two questions; stRuleLabel its
	// "#<turn>".
	stRule
	stRuleLabel
	// stDetail and stFrame are the expanded drawer's text and frame.
	stDetail
	stFrame
	// stAdded, stRemoved and stTimes are a change detail's "+N", "−M" and
	// "×2" markers; stLink the Δ of a linked change.
	stAdded
	stRemoved
	stTimes
	stLink
	stCount
)

// Canvas cell backgrounds.
const (
	bgNone = iota
	bgSelected
	bgSelectedMuted
	bgCount
)

// cell is one canvas position: its glyph ("" for the second half of a wide
// rune), style and selection background.
type cell struct {
	ch string
	st int
	bg int
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
// cells (<= 0 for no limit). Every rune takes exactly the cells its width
// claims, so a row always renders c.w cells wide: a wide rune that would
// straddle the right edge is left out, and a write over half of a wide
// rune blanks the other half (#2866). The background of the cells written
// is kept.
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
		if x >= 0 && x+w <= c.w {
			c.clear(x, y)
			if w == 2 {
				c.clear(x+1, y)
			}
			c.cells[y][x] = cell{ch: string(r), st: st, bg: c.cells[y][x].bg}
			if w == 2 {
				c.cells[y][x+1] = cell{ch: "", st: st, bg: c.cells[y][x+1].bg}
			}
		}
		x += w
		used += w
	}
}

// clear blanks cell (x, y) ahead of a write, and with it the other half of
// the wide rune it belongs to.
func (c *canvas) clear(x, y int) {
	row := c.cells[y]
	switch {
	case row[x].ch == "" && x > 0:
		row[x-1] = cell{ch: " ", st: row[x-1].st, bg: row[x-1].bg}
	case x+1 < c.w && row[x+1].ch == "":
		row[x+1] = cell{ch: " ", st: row[x+1].st, bg: row[x+1].bg}
	}
	row[x] = cell{ch: " ", st: row[x].st, bg: row[x].bg}
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

// highlight sets the background of a run of cells.
func (c *canvas) highlight(x, y, n, bg int) {
	if y < 0 || y >= c.h {
		return
	}
	for i := x; i < x+n && i < c.w; i++ {
		if i >= 0 {
			c.cells[y][i].bg = bg
		}
	}
}

// lines renders rows [from, to) with the given styles and backgrounds.
func (c *canvas) lines(from, to int, styles []lipgloss.Style, bgs []color.Color) []string {
	var out []string
	for y := from; y < to && y < c.h; y++ {
		if y < 0 {
			continue
		}
		var sb strings.Builder
		var run strings.Builder
		cur, curBg := -1, -1
		flush := func() {
			if run.Len() == 0 {
				return
			}
			switch {
			case cur == stPlain && curBg == bgNone:
				sb.WriteString(run.String())
			case curBg == bgNone:
				sb.WriteString(styles[cur].Render(run.String()))
			default:
				sb.WriteString(styles[cur].Background(bgs[curBg]).Render(run.String()))
			}
			run.Reset()
		}
		for _, cl := range c.cells[y] {
			if cl.ch == "" {
				continue
			}
			if cl.st != cur || cl.bg != curBg {
				flush()
				cur, curBg = cl.st, cl.bg
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

// graphStyles builds the canvas styles off the palette: the kinds take the
// dedicated trace roles (#2874), the path and the rule the border colour,
// secondary text the Secondary role — no hard-coded colours, so both
// palettes read.
func (m *Model) graphStyles(pal *theme.Palette) []lipgloss.Style {
	st := make([]lipgloss.Style, stCount)
	st[stPlain] = lipgloss.NewStyle()
	st[stSecondary] = lipgloss.NewStyle().Foreground(pal.Secondary)
	st[stPrompt] = lipgloss.NewStyle().Foreground(pal.TracePrompt)
	st[stEdit] = lipgloss.NewStyle().Foreground(pal.TraceEdit)
	st[stCreate] = lipgloss.NewStyle().Foreground(pal.TraceCreate)
	st[stDelete] = lipgloss.NewStyle().Foreground(pal.TraceDelete)
	st[stAnswer] = lipgloss.NewStyle().Foreground(pal.TraceAnswer)
	st[stImplicit] = lipgloss.NewStyle().Foreground(pal.TraceAnswer).Faint(true)
	st[stError] = lipgloss.NewStyle().Foreground(pal.Error).Bold(true)
	st[stPending] = lipgloss.NewStyle().Faint(true)
	st[stRewind] = lipgloss.NewStyle().Faint(true)
	st[stSelText] = lipgloss.NewStyle().Foreground(pal.SelectionText).Bold(true)
	st[stConn] = lipgloss.NewStyle().Foreground(pal.Border)
	st[stRule] = lipgloss.NewStyle().Foreground(pal.Border)
	st[stRuleLabel] = lipgloss.NewStyle().Foreground(pal.TracePrompt).Bold(true)
	st[stDetail] = lipgloss.NewStyle().Foreground(pal.Foreground)
	st[stFrame] = lipgloss.NewStyle().Foreground(pal.Border)
	st[stAdded] = lipgloss.NewStyle().Foreground(pal.TraceCreate)
	st[stRemoved] = lipgloss.NewStyle().Foreground(pal.TraceDelete)
	st[stTimes] = lipgloss.NewStyle().Foreground(pal.TraceEdit)
	st[stLink] = lipgloss.NewStyle().Foreground(pal.Accent)
	return st
}

// graphBackgrounds builds the selection backgrounds off the palette,
// indexed by the bg* constants (bgNone unused).
func graphBackgrounds(pal *theme.Palette) []color.Color {
	return []color.Color{bgNone: nil, bgSelected: pal.Selection, bgSelectedMuted: pal.SelectionMuted}
}

// frame is a box's border glyph set: kinds tell apart by frame as well as
// by glyph and colour, so a monochrome terminal still reads the path.
type frame struct{ tl, tr, bl, br, h, v string }

var (
	// frameSingle is a change (and a rewind marker).
	frameSingle = frame{"┌", "┐", "└", "┘", "─", "│"}
	// frameDouble is a question: the milestone of a turn.
	frameDouble = frame{"╔", "╗", "╚", "╝", "═", "║"}
	// frameRound is an answer.
	frameRound = frame{"╭", "╮", "╰", "╯", "─", "│"}
	// frameDashed and frameRoundDashed are a pending change / answer: the
	// box is not settled yet.
	frameDashed      = frame{"┌", "┐", "└", "┘", "┄", "┆"}
	frameRoundDashed = frame{"╭", "╮", "╰", "╯", "┄", "┆"}
)

// stopStyle is the style and glyph of a stop: kinds tell apart by glyph
// as well as colour, so a monochrome terminal still reads the path.
func stopStyle(st agenttrace.Stop) (style int, glyph string) {
	switch st.Kind {
	case agenttrace.StopPrompt:
		return stPrompt, "?"
	case agenttrace.StopAnswer:
		switch {
		case st.Pending:
			return stPending, "…"
		case st.Implicit:
			return stImplicit, "◌"
		}
		return stAnswer, "✓"
	case agenttrace.StopRewind:
		return stRewind, "↶"
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
	return stSecondary, "◇"
}

// stopFrame is the border glyph set of a stop: double for a question,
// rounded for an answer, single for a change, dashed while pending.
func stopFrame(st agenttrace.Stop) frame {
	switch st.Kind {
	case agenttrace.StopPrompt:
		return frameDouble
	case agenttrace.StopAnswer:
		if st.Pending {
			return frameRoundDashed
		}
		return frameRound
	case agenttrace.StopChange:
		if st.Pending && !st.Error {
			return frameDashed
		}
	}
	return frameSingle
}

// graphRows draws the path onto a canvas and returns the body's visible
// rows, padded to the body height.
func (m *Model) graphRows(pal *theme.Palette) []string {
	g := &m.graph
	h := m.treeHeight()
	if len(g.stops) == 0 {
		rows := []string{
			lipgloss.NewStyle().Foreground(pal.Secondary).Render(" (no events yet)"),
			lipgloss.NewStyle().Faint(true).Render(" the path fills as the agent asks, changes files and answers"),
		}
		for len(rows) < h {
			rows = append(rows, "")
		}
		return rows[:h]
	}
	l := m.graphLayout()
	g.top = max(0, min(g.top, m.graphMaxTop(l)))
	c := newCanvas(m.width, l.Height)
	for i, st := range g.stops {
		s := l.Slots[i]
		if s.Break {
			turn := st.Turn
			if !st.Selectable() && i+1 < len(g.stops) {
				// A compaction marker starting the row with its question
				// labels the rule with that question's turn.
				turn = g.stops[i+1].Turn
			}
			drawTurnRule(c, s.Y-2, turn)
		}
		if i > 0 {
			drawConnector(c, l.Slots[i-1], s)
		}
		if !st.Selectable() {
			drawSeparator(c, s)
			continue
		}
		m.drawBox(c, st, s, i)
	}
	if exp := m.stopIndex(g.expanded); exp >= 0 && l.DetailH > 0 {
		m.drawDrawer(c, l, g.stops[exp], l.Slots[exp])
	}
	rows := c.lines(g.top, g.top+h, m.graphStyles(pal), graphBackgrounds(pal))
	for len(rows) < h {
		rows = append(rows, "")
	}
	return rows
}

// drawBox draws one 4-row box (#2872): the top border carrying the kind
// glyph (and the Δ mark of a linked change), the label in the kind colour,
// the detail in secondary text with a change's markers coloured, and a
// closed bottom border with no text in it — only the expanded box's "┬"
// marks where its drawer attaches. The selected box is lifted off the
// pane: all four rows take the selection background, the focused pane's
// content rows the selection text colour (#2901).
func (m *Model) drawBox(c *canvas, st agenttrace.Stop, s Slot, i int) {
	style, glyph := stopStyle(st)
	f := stopFrame(st)
	w := s.W
	if w < 4 {
		return
	}
	linked := m.links.Node(st.Key) != ""
	// Top border.
	c.put(s.X, s.Y, f.tl, style, 0)
	c.put(s.X+1, s.Y, glyph, style, 0)
	c.fill(s.X+2, s.Y, w-3, f.h, style)
	if linked && w > 5 {
		c.put(s.X+w-2, s.Y, linkMark, stLink, 0)
	}
	c.put(s.X+w-1, s.Y, f.tr, style, 0)
	// Label and detail rows.
	for y := s.Y + 1; y <= s.Y+2; y++ {
		c.put(s.X, y, f.v, style, 0)
		c.put(s.X+w-1, y, f.v, style, 0)
	}
	// The label carries the kind colour too (#2874), so the kind reads from
	// more than one thin border line; the selection below still wins.
	labelSt := style
	if (st.Pending && st.Kind == agenttrace.StopAnswer) || st.Kind == agenttrace.StopRewind {
		labelSt = stSecondary
	}
	c.put(s.X+1, s.Y+1, fitCells(st.Label, w-2), labelSt, w-2)
	drawDetail(c, st, s.X+1, s.Y+2, w-2)
	// Bottom border.
	c.put(s.X, s.Y+3, f.bl, style, 0)
	c.fill(s.X+1, s.Y+3, w-2, f.h, style)
	if st.Key != "" && st.Key == m.graph.expanded {
		c.put(s.CenterX(), s.Y+3, "┬", style, 0)
	}
	c.put(s.X+w-1, s.Y+3, f.br, style, 0)
	if st.Key != "" && st.Key == m.graph.sel {
		bg := bgSelectedMuted
		if m.focused {
			bg = bgSelected
		}
		for y := s.Y; y < s.Y+boxH; y++ {
			c.highlight(s.X, y, w, bg)
		}
		if m.focused {
			c.restyle(s.X+1, s.Y+1, w-2, stSelText)
			c.restyle(s.X+1, s.Y+2, w-2, stSelText)
		}
	}
}

// drawDetail writes a box's detail row in secondary text; a change's
// markers stand out in their own colours — "+N" added, "−M" removed, "×2"
// edits of one file, "✗" failed, "…" pending.
func drawDetail(c *canvas, st agenttrace.Stop, x, y, w int) {
	d := fitCells(st.Detail, w)
	if st.Kind != agenttrace.StopChange {
		c.put(x, y, d, stSecondary, w)
		return
	}
	pos := 0
	for _, tok := range strings.SplitAfter(d, " ") {
		c.put(x+pos, y, tok, detailTokenStyle(strings.TrimSpace(tok)), 0)
		pos += ansi.StringWidth(tok)
	}
}

// detailTokenStyle is the style of one word of a change detail.
func detailTokenStyle(tok string) int {
	switch {
	case tok == "":
		return stSecondary
	case strings.HasPrefix(tok, "+"):
		return stAdded
	case strings.HasPrefix(tok, "−"):
		return stRemoved
	case strings.HasPrefix(tok, "×"):
		return stTimes
	case tok == "✗":
		return stError
	case tok == "…":
		return stPending
	}
	return stSecondary
}

// drawSeparator draws a compaction marker: a "◇" on the path.
func drawSeparator(c *canvas, s Slot) {
	c.put(s.X, s.Y+1, "─", stConn, 0)
	c.put(s.X+1, s.Y+1, "◇", stSecondary, 0)
	c.put(s.X+2, s.Y+1, "─", stConn, 0)
}

// detailFrameH is the rows the drawer's frame adds around the detail
// lines: its top border (attached to the box) and its bottom border
// (carrying the key hint).
const detailFrameH = 2

// drawDrawer draws the expanded box's detail block (#2901): a full-width
// rounded drawer beneath the box's row whose top border joins the box's
// "┬" with a "┴", the detail lines inside, the key hint in the bottom
// border.
func (m *Model) drawDrawer(c *canvas, l Layout, st agenttrace.Stop, s Slot) {
	lines := m.detailLines(st, m.width)
	w, y := c.w, l.DetailY
	if w < 6 {
		return
	}
	c.put(0, y, "╭", stFrame, 0)
	c.fill(1, y, w-2, "─", stFrame)
	c.put(w-1, y, "╮", stFrame, 0)
	c.put(s.CenterX(), y, "┴", stFrame, 0)
	for j, line := range lines {
		st := stDetail
		if strings.HasPrefix(line, "› ") {
			st = stSecondary
		}
		c.put(0, y+1+j, "│", stFrame, 0)
		c.put(2, y+1+j, line, st, w-4)
		c.put(w-1, y+1+j, "│", stFrame, 0)
	}
	yb := y + 1 + len(lines)
	c.put(0, yb, "╰", stFrame, 0)
	c.fill(1, yb, w-2, "─", stFrame)
	c.put(w-1, yb, "╯", stFrame, 0)
	c.put(2, yb, " "+fitCells(drawerHint(st), w-6)+" ", stSecondary, w-4)
}

// drawerHint is the key line in the drawer's bottom border.
func drawerHint(st agenttrace.Stop) string {
	if st.Kind == agenttrace.StopRewind {
		return "space collapse · the live path continues to the right"
	}
	return "space collapse · enter open · D diff · a ask · V revert"
}

// drawTurnRule draws the dotted "┄┄ #<turn> ┄┄…" rule across the pane
// that sets a new question apart from the turn before (#2866): a boundary
// that reads without dominating the view.
func drawTurnRule(c *canvas, y, turn int) {
	c.fill(0, y, c.w, "┄", stRule)
	c.put(2, y, " #"+itoa(turn)+" ", stRuleLabel, 0)
}

// drawConnector joins two consecutive slots as one path: an arrow along
// the row, or the turn down the rows between two path rows — a "│" over a
// "▼" when the slots' centres align, else an elbow ("╰──╮" / "╭──╯") on
// the top gap row — crossing a break's turn rule with "┼".
func drawConnector(c *canvas, prev, s Slot) {
	switch {
	case prev.Row != s.Row:
		px, x := prev.CenterX(), s.CenterX()
		top := s.Y - 2
		if s.Break {
			top = s.Y - 3
			c.put(x, s.Y-2, "┼", stConn, 0)
		}
		drawElbow(c, top, px, x)
		if s.W == sepW {
			// The path runs straight into the marker.
			c.put(x, s.Y-1, "│", stConn, 0)
			c.put(x, s.Y, "│", stConn, 0)
		} else {
			c.put(x, s.Y-1, "▼", stConn, 0)
		}
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

// drawElbow draws the row of a turn that leaves column px and arrives at
// column x: a "│" when they agree, else a rounded elbow between them.
func drawElbow(c *canvas, y, px, x int) {
	switch {
	case px == x:
		c.put(x, y, "│", stConn, 0)
	case x > px:
		c.put(px, y, "╰", stConn, 0)
		c.fill(px+1, y, x-px-1, "─", stConn)
		c.put(x, y, "╮", stConn, 0)
	default:
		c.put(x, y, "╭", stConn, 0)
		c.fill(x+1, y, px-x-1, "─", stConn)
		c.put(px, y, "╯", stConn, 0)
	}
}

// graphHint is the key line under the graph.
const graphHint = "←/→ ↑/↓ move · n/p along the path · enter open · space expand · t tree · s sessions · D diff · a ask · r rescan · V revert"
