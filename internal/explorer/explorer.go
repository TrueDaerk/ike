// Package explorer implements the file-tree pane: it shows the project directory
// as an expandable tree rooted at a fixed base (the explorer never ascends above
// it), lets the user expand/collapse folders in place with vim-like keys, and
// opens a file by emitting an OpenFileMsg the root model routes to the editor.
package explorer

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// OpenFileMsg is emitted when the user selects a file to open. The root model
// listens for it and forwards the path to the editor.
type OpenFileMsg struct {
	Path string
}

// node is one entry in the tree. Directory children are loaded lazily the first
// time the node is expanded.
type node struct {
	name     string
	path     string
	isDir    bool
	depth    int
	expanded bool
	loaded   bool
	children []*node
}

// Model is the file-explorer pane: an expandable tree rooted at a fixed base.
type Model struct {
	root    *node   // project base; never replaced, never escaped
	rows    []*node // flattened visible nodes, rebuilt on every expand/collapse
	cursor  int     // index into rows
	offset  int     // first visible row (vertical scroll)
	offsetX int     // first visible column (horizontal scroll)
	hover   int     // row index under the mouse pointer, -1 when none
	active  string  // path of the file currently open in the editor, "" when none
	width   int
	height  int
	focused bool
	err     error
}

// New creates an explorer rooted at dir. The root is expanded immediately so its
// children are visible. A read error is retained and shown in place of the tree.
func New(dir string) Model {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	root := &node{
		name:  filepath.Base(abs),
		path:  abs,
		isDir: true,
		depth: 0,
	}
	m := Model{root: root, hover: -1}
	m.expand(root)
	m.rebuild()
	return m
}

// loadChildren reads a directory node's entries once, sorted directories-first
// then alphabetically.
func (m *Model) loadChildren(n *node) {
	if n.loaded || !n.isDir {
		return
	}
	n.loaded = true
	des, err := os.ReadDir(n.path)
	if err != nil {
		m.err = err
		return
	}
	m.err = nil
	children := make([]*node, 0, len(des))
	for _, de := range des {
		children = append(children, &node{
			name:  de.Name(),
			path:  filepath.Join(n.path, de.Name()),
			isDir: de.IsDir(),
			depth: n.depth + 1,
		})
	}
	sort.SliceStable(children, func(i, j int) bool {
		if children[i].isDir != children[j].isDir {
			return children[i].isDir
		}
		return children[i].name < children[j].name
	})
	n.children = children
}

// expand opens a directory node, loading its children on first use.
func (m *Model) expand(n *node) {
	if !n.isDir {
		return
	}
	m.loadChildren(n)
	n.expanded = true
}

// rebuild flattens the visible tree into m.rows and clamps the cursor.
func (m *Model) rebuild() {
	m.rows = m.rows[:0]
	m.appendVisible(m.root)
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.clampScroll()
}

// appendVisible walks the tree depth-first, emitting each node and recursing into
// expanded directories.
func (m *Model) appendVisible(n *node) {
	m.rows = append(m.rows, n)
	if n.isDir && n.expanded {
		for _, c := range n.children {
			m.appendVisible(c)
		}
	}
}

// Root returns the fixed project base directory.
func (m Model) Root() string { return m.root.path }

// SetSize sets the available width and number of rows.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.clampScroll()
}

// SetFocused toggles whether this pane receives key input.
func (m *Model) SetFocused(f bool) { m.focused = f }

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update handles navigation/expand keys and returns an OpenFileMsg command when a
// file is opened.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "down", "j":
		m.moveCursor(1)
	case "up", "k":
		m.moveCursor(-1)
	case "enter":
		return m.activate()
	case "l", "right":
		return m.expandOrOpen()
	case "h", "left":
		m.collapseOrParent()
	}
	return m, nil
}

func (m *Model) current() *node {
	if len(m.rows) == 0 {
		return nil
	}
	return m.rows[m.cursor]
}

func (m *Model) moveCursor(delta int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	m.clampScroll()
}

// activate toggles a directory (expand/collapse) or opens a file (enter).
func (m Model) activate() (Model, tea.Cmd) {
	n := m.current()
	if n == nil {
		return m, nil
	}
	if n.isDir {
		if n.expanded {
			n.expanded = false
		} else {
			m.expand(n)
		}
		m.rebuild()
		return m, nil
	}
	return m, openCmd(n.path)
}

// expandOrOpen expands a collapsed directory, steps into the first child of an
// expanded one, or opens a file (l / right).
func (m Model) expandOrOpen() (Model, tea.Cmd) {
	n := m.current()
	if n == nil {
		return m, nil
	}
	if !n.isDir {
		return m, openCmd(n.path)
	}
	if !n.expanded {
		m.expand(n)
		m.rebuild()
		return m, nil
	}
	if len(n.children) > 0 {
		m.cursor++ // first child is the next visible row
		m.clampScroll()
	}
	return m, nil
}

// collapseOrParent collapses an expanded directory, otherwise jumps to the
// parent node. It never moves above the root.
func (m *Model) collapseOrParent() {
	n := m.current()
	if n == nil {
		return
	}
	if n.isDir && n.expanded {
		n.expanded = false
		m.rebuild()
		return
	}
	m.jumpToParent()
}

// jumpToParent moves the cursor to the nearest preceding row one depth shallower.
func (m *Model) jumpToParent() {
	depth := m.rows[m.cursor].depth
	for i := m.cursor - 1; i >= 0; i-- {
		if m.rows[i].depth < depth {
			m.cursor = i
			m.clampScroll()
			return
		}
	}
}

func openCmd(path string) tea.Cmd {
	return func() tea.Msg { return OpenFileMsg{Path: path} }
}

// clampScroll keeps the cursor within the visible window.
func (m *Model) clampScroll() {
	_, textH, _, _, _ := m.viewport()
	if textH <= 0 {
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+textH {
		m.offset = m.cursor - textH + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// text is the plain (unstyled) content of a row: depth indent, an expand marker,
// and the name (directories gain a trailing slash). It is the single source of
// truth for both width measurement and rendering.
func (n *node) text() string {
	marker := "  "
	if n.isDir {
		if n.expanded {
			marker = "▾ "
		} else {
			marker = "▸ "
		}
	}
	label := n.name
	if n.isDir {
		label += "/"
	}
	return strings.Repeat("  ", n.depth) + marker + label
}

// contentWidth is the display width of the widest visible row.
func (m Model) contentWidth() int {
	w := 0
	for _, n := range m.rows {
		if cw := ansi.StringWidth(n.text()); cw > w {
			w = cw
		}
	}
	return w
}

// viewport resolves the inner text area: its width/height after reserving a
// column for the vertical scrollbar and a row for the horizontal one, whether
// each bar is needed, and the total content width. Two passes settle the mutual
// dependence (reserving for one bar can push the other axis into overflow).
func (m Model) viewport() (textW, textH int, needV, needH bool, contentW int) {
	vw, vh := m.width, m.height
	if vw < 1 {
		vw = 1
	}
	if vh < 1 {
		vh = 1
	}
	contentW = m.contentWidth()
	total := len(m.rows)
	for i := 0; i < 2; i++ {
		textW, textH = vw, vh
		if needV {
			textW--
		}
		if needH {
			textH--
		}
		needV = total > textH
		needH = contentW > textW
	}
	textW, textH = vw, vh
	if needV {
		textW--
	}
	if needH {
		textH--
	}
	if textW < 1 {
		textW = 1
	}
	if textH < 1 {
		textH = 1
	}
	return
}

// scrollThumb sizes and positions a scrollbar thumb on a track of the given
// length for a window of visible cells over a total content size at offset.
func scrollThumb(track, total, visible, offset int) (start, length int) {
	if track <= 0 {
		return 0, 0
	}
	if total <= visible {
		return 0, track
	}
	length = track * visible / total
	if length < 1 {
		length = 1
	}
	if length > track {
		length = track
	}
	maxOff := total - visible
	start = (track - length) * offset / maxOff
	if start < 0 {
		start = 0
	}
	if start > track-length {
		start = track - length
	}
	return
}

// ScrollBy moves the vertical viewport by delta rows (positive scrolls down)
// without moving the cursor — the way a mouse wheel scrolls independently of the
// selection.
func (m *Model) ScrollBy(delta int) {
	_, textH, _, _, _ := m.viewport()
	maxOff := len(m.rows) - textH
	if maxOff < 0 {
		maxOff = 0
	}
	m.offset += delta
	if m.offset > maxOff {
		m.offset = maxOff
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// ScrollXBy moves the horizontal viewport by delta columns (positive scrolls
// right). It is the seam for shift-wheel / horizontal-wheel scrolling.
func (m *Model) ScrollXBy(delta int) {
	textW, _, _, _, contentW := m.viewport()
	maxOff := contentW - textW
	if maxOff < 0 {
		maxOff = 0
	}
	m.offsetX = clamp(m.offsetX+delta, 0, maxOff)
}

// SetActive marks path as the file currently open in the editor so its row is
// highlighted distinctly from the cursor and hover. Pass "" to clear it.
func (m *Model) SetActive(path string) { m.active = path }

// SetHoverAt records the row under the mouse at content-local coordinates, or
// clears the hover when the pointer is off a content row.
func (m *Model) SetHoverAt(x, y int) {
	textW, textH, _, _, _ := m.viewport()
	if x < 0 || y < 0 || x >= textW || y >= textH {
		m.hover = -1
		return
	}
	if i := m.offset + y; i < len(m.rows) {
		m.hover = i
		return
	}
	m.hover = -1
}

// ClearHover drops any hover highlight (pointer left the pane).
func (m *Model) ClearHover() { m.hover = -1 }

// HoverRow returns the visible row index under the pointer, or -1 when none.
func (m Model) HoverRow() int { return m.hover }

// Active returns the path of the file currently marked open, or "" when none.
func (m Model) Active() string { return m.active }

// MouseClick handles a left-press at content-local coordinates (0-based from the
// top-left of the tree area). A press on a scrollbar jumps that axis; a press on
// a row selects it and activates it (toggling a directory, opening a file).
func (m Model) MouseClick(x, y int) (Model, tea.Cmd) {
	if len(m.rows) == 0 || x < 0 || y < 0 {
		return m, nil
	}
	textW, textH, needV, needH, contentW := m.viewport()

	if needV && x == textW && y < textH { // vertical scrollbar track
		if maxOff := len(m.rows) - textH; maxOff > 0 && textH > 1 {
			m.offset = clamp(y*maxOff/(textH-1), 0, maxOff)
		}
		return m, nil
	}
	if needH && y == textH && x < textW { // horizontal scrollbar track
		if maxOff := contentW - textW; maxOff > 0 && textW > 1 {
			m.offsetX = clamp(x*maxOff/(textW-1), 0, maxOff)
		}
		return m, nil
	}
	if x >= textW || y >= textH { // chrome / empty space
		return m, nil
	}
	i := m.offset + y
	if i >= len(m.rows) {
		return m, nil
	}
	m.cursor = i
	return m.activate()
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Scrollbar styling: a dim track with a brighter, heavier thumb, in the spirit
// of table TUIs that surface overflow on the right and bottom edges.
var (
	barTrack    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	barThumb    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	selStyle    = lipgloss.NewStyle().Background(lipgloss.Color("69")).Foreground(lipgloss.Color("231")).Bold(true)
	hoverStyle  = lipgloss.NewStyle().Background(lipgloss.Color("236"))
	activeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("215")).Bold(true)
	dirStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
)

// View renders the tree, clipping each row to the horizontal window and drawing
// vertical/horizontal scrollbars whenever the content overflows the pane.
func (m Model) View() string {
	if m.err != nil {
		return "error: " + m.err.Error()
	}
	if len(m.rows) == 0 {
		return lipgloss.NewStyle().Faint(true).Render("(empty)")
	}
	textW, textH, needV, needH, contentW := m.viewport()
	offY := clamp(m.offset, 0, maxz(len(m.rows)-textH))
	offX := clamp(m.offsetX, 0, maxz(contentW-textW))

	vStart, vLen := scrollThumb(textH, len(m.rows), textH, offY)

	var lines []string
	for k := 0; k < textH; k++ {
		i := offY + k
		var line string
		if i < len(m.rows) {
			n := m.rows[i]
			vis := ansi.Cut(n.text(), offX, offX+textW)
			if pad := textW - ansi.StringWidth(vis); pad > 0 {
				vis += strings.Repeat(" ", pad)
			}
			line = styleFor(m.rowKind(i)).Render(vis)
		} else {
			line = strings.Repeat(" ", textW)
		}
		if needV {
			line += bar("│", "┃", k >= vStart && k < vStart+vLen)
		}
		lines = append(lines, line)
	}

	if needH {
		hStart, hLen := scrollThumb(textW, contentW, textW, offX)
		var b strings.Builder
		for k := 0; k < textW; k++ {
			b.WriteString(bar("─", "━", k >= hStart && k < hStart+hLen))
		}
		row := b.String()
		if needV {
			row += barTrack.Render("╯")
		}
		lines = append(lines, row)
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// rowKind classifies how visible row i is highlighted. Precedence, strongest
// first: the focused cursor, the mouse hover, the open file, a directory, then a
// plain file. View maps each kind to a style; tests exercise the logic here so
// they do not depend on the terminal's colour profile.
type rowKind int

const (
	rowPlain rowKind = iota
	rowDir
	rowActive
	rowHover
	rowSelected
)

func (m Model) rowKind(i int) rowKind {
	n := m.rows[i]
	switch {
	case i == m.cursor && m.focused:
		return rowSelected
	case i == m.hover:
		return rowHover
	case n.path == m.active && m.active != "":
		return rowActive
	case n.isDir:
		return rowDir
	default:
		return rowPlain
	}
}

func styleFor(k rowKind) lipgloss.Style {
	switch k {
	case rowSelected:
		return selStyle
	case rowHover:
		return hoverStyle
	case rowActive:
		return activeStyle
	case rowDir:
		return dirStyle
	default:
		return lipgloss.NewStyle()
	}
}

// bar renders one scrollbar cell, picking the thumb glyph over the track glyph.
func bar(track, thumb string, isThumb bool) string {
	if isThumb {
		return barThumb.Render(thumb)
	}
	return barTrack.Render(track)
}

func maxz(v int) int {
	if v < 0 {
		return 0
	}
	return v
}
