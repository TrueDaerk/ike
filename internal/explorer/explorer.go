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
	loading  bool // a scan Cmd is in flight for this directory
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

	showHidden bool       // render dot-entries; toggled by explorer.toggleHidden
	indent     int        // spaces per depth level (config explorer.tree_indent)
	sort       string     // ordering within a level (config explorer.sort)
	colors     colorTable // per-filetype colour resolution
}

// New creates an explorer rooted at dir. The root is marked expanded and a scan
// of its children is kicked off by Init; until the scan result arrives the tree
// shows just the root row. A read error is retained and shown in place of the
// tree.
func New(dir string) Model {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	root := &node{
		name:     filepath.Base(abs),
		path:     abs,
		isDir:    true,
		depth:    0,
		expanded: true,
		loading:  true,
	}
	m := Model{
		root:   root,
		hover:  -1,
		indent: 2,
		sort:   "name",
		colors: defaultColors,
	}
	m.rebuild()
	return m
}

// ScanDoneMsg carries the result of a directory scan back into the model. It is
// dispatched by the Cmd expand/refresh return; the root model routes it here.
type ScanDoneMsg struct {
	Path    string
	Entries []scanEntry
	Err     error
}

func (ScanDoneMsg) explorerMsg() {}

// scanEntry is the minimal directory entry a scan reports; node construction
// (depth, sort) happens on the update thread, not in the Cmd.
type scanEntry struct {
	name  string
	isDir bool
}

// scanCmd reads path's entries off the update loop and returns them as a
// ScanDoneMsg, so a slow or huge directory never blocks the UI.
func scanCmd(path string) tea.Cmd {
	return func() tea.Msg {
		des, err := os.ReadDir(path)
		if err != nil {
			return ScanDoneMsg{Path: path, Err: err}
		}
		es := make([]scanEntry, len(des))
		for i, de := range des {
			es[i] = scanEntry{name: de.Name(), isDir: de.IsDir()}
		}
		return ScanDoneMsg{Path: path, Entries: es}
	}
}

// applyScan installs a completed scan's children onto the matching node and
// rebuilds the visible rows. Unknown paths (a node collapsed/refreshed before
// the scan returned) are ignored.
func (m *Model) applyScan(msg ScanDoneMsg) {
	n := nodeByPath(m.root, msg.Path)
	if n == nil {
		return
	}
	n.loading = false
	n.loaded = true
	if msg.Err != nil {
		m.err = msg.Err
		return
	}
	m.err = nil
	m.setChildren(n, msg.Entries)
	m.rebuild()
}

// setChildren installs sorted child nodes on n from a scan's entries. It is
// shared by the async scan path and the synchronous session-restore path.
func (m *Model) setChildren(n *node, entries []scanEntry) {
	children := make([]*node, 0, len(entries))
	for _, e := range entries {
		children = append(children, &node{
			name:  e.name,
			path:  filepath.Join(n.path, e.name),
			isDir: e.isDir,
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

// nodeByPath finds the node with the given path in the subtree rooted at n.
func nodeByPath(n *node, path string) *node {
	if n.path == path {
		return n
	}
	for _, c := range n.children {
		if found := nodeByPath(c, path); found != nil {
			return found
		}
	}
	return nil
}

// expand opens a directory node, dispatching a scan Cmd on first use. The
// returned Cmd is nil when nothing needs scanning (leaf, or already loaded).
func (m *Model) expand(n *node) tea.Cmd {
	if !n.isDir {
		return nil
	}
	n.expanded = true
	if n.loaded || n.loading {
		return nil
	}
	n.loading = true
	return scanCmd(n.path)
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
// expanded directories. Hidden (dot-prefixed) entries are skipped unless
// showHidden is on; the root is always emitted.
func (m *Model) appendVisible(n *node) {
	m.rows = append(m.rows, n)
	if n.isDir && n.expanded {
		for _, c := range n.children {
			if !m.showHidden && isHidden(c.name) {
				continue
			}
			m.appendVisible(c)
		}
	}
}

// isHidden reports whether name is a hidden (dot-prefixed) entry.
func isHidden(name string) bool { return strings.HasPrefix(name, ".") }

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

// Init implements tea.Model: it kicks off the root directory scan, unless a
// restored session already loaded the root synchronously (an async re-scan would
// replace the children and discard the restored expansion state).
func (m Model) Init() tea.Cmd {
	if m.root.loaded {
		return nil
	}
	return scanCmd(m.root.path)
}

// Update handles navigation/expand keys, scan results, and explorer command
// messages. It returns a Cmd for any work that must run off the update loop
// (directory scans) or be routed onward (OpenFileMsg).
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ScanDoneMsg:
		m.applyScan(msg)
		return m, nil
	case ToggleHiddenMsg:
		m.showHidden = !m.showHidden
		m.rebuild()
		return m, nil
	case CollapseAllMsg:
		m.collapseAll()
		return m, nil
	case RefreshMsg:
		return m, m.refresh()
	case RevealMsg:
		m.reveal()
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
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
		var cmd tea.Cmd
		if n.expanded {
			n.expanded = false
		} else {
			cmd = m.expand(n)
		}
		m.rebuild()
		return m, cmd
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
		cmd := m.expand(n)
		m.rebuild()
		return m, cmd
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

// collapseAll folds the whole tree back to the root and parks the cursor there.
func (m *Model) collapseAll() {
	collapse(m.root)
	m.root.expanded = true // the root stays open; the tree is anchored to it
	m.cursor = 0
	m.offset = 0
	m.rebuild()
}

func collapse(n *node) {
	n.expanded = false
	for _, c := range n.children {
		collapse(c)
	}
}

// refresh invalidates the selected directory's cache (or its parent's, when a
// file is selected) and re-scans it, so external changes show up. Expansion
// state is preserved; the scan repopulates children in place.
func (m *Model) refresh() tea.Cmd {
	n := m.current()
	if n == nil {
		n = m.root
	}
	if !n.isDir {
		n = m.parentOf(n)
	}
	if n == nil {
		n = m.root
	}
	n.loaded = false
	n.loading = true
	n.children = nil
	m.rebuild()
	return scanCmd(n.path)
}

// reveal moves the cursor onto the row of the currently open file, if it is
// visible in the tree. (Auto-expanding collapsed ancestors is left to a later
// pass; today it locates an already-visible active row.)
func (m *Model) reveal() {
	if m.active == "" {
		return
	}
	for i, n := range m.rows {
		if n.path == m.active {
			m.cursor = i
			m.clampScroll()
			return
		}
	}
}

// parentOf returns the parent node of target, or nil for the root / not found.
func (m *Model) parentOf(target *node) *node {
	var find func(n *node) *node
	find = func(n *node) *node {
		for _, c := range n.children {
			if c == target {
				return n
			}
			if p := find(c); p != nil {
				return p
			}
		}
		return nil
	}
	return find(m.root)
}

// clampScroll keeps the cursor within the visible window and the scroll offset
// within the renderable range. Clamping to maxOff is essential: View() clamps a
// stale offset only for display, but MouseClick and hover hit-testing read the
// raw offset, so an offset past the last page would make clicks land on rows far
// below the ones actually shown.
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
	if maxOff := len(m.rows) - textH; m.offset > maxOff {
		m.offset = maxOff
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// rowText is the plain (unstyled) content of a row: indent guides for each
// ancestor level, an expand marker, and the name (directories gain a trailing
// slash). It is the single source of truth for both width measurement and
// rendering, so clipping and the scrollbars agree.
func (m Model) rowText(n *node) string {
	var b strings.Builder
	for d := 0; d < n.depth; d++ {
		b.WriteString("│")
		b.WriteString(strings.Repeat(" ", maxz(m.indent-1)))
	}
	b.WriteString(marker(n))
	b.WriteString(n.name)
	if n.isDir {
		b.WriteString("/")
	}
	return b.String()
}

// marker is the two-cell expand glyph for a row: a caret for directories, blank
// for files.
func marker(n *node) string {
	if !n.isDir {
		return "  "
	}
	if n.expanded {
		return "▾ "
	}
	return "▸ "
}

// contentWidth is the display width of the widest visible row.
func (m Model) contentWidth() int {
	w := 0
	for _, n := range m.rows {
		if cw := ansi.StringWidth(m.rowText(n)); cw > w {
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
// of table TUIs that surface overflow on the right and bottom edges. Highlight
// styles overlay the per-filetype colour: the cursor and open-file rows replace
// it, the hover keeps the foreground colour and adds a background.
var (
	barTrack    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	barThumb    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	selStyle    = lipgloss.NewStyle().Background(lipgloss.Color("69")).Foreground(lipgloss.Color("231")).Bold(true)
	activeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("215")).Bold(true)
	hoverBg     = lipgloss.Color("236")
)

// nodeStyle is a row's base style: its per-filetype colour, plus italics for
// hidden (dot-prefixed) entries.
func (m Model) nodeStyle(n *node) lipgloss.Style {
	s := m.colors.style(n)
	if isHidden(n.name) {
		s = s.Italic(true)
	}
	return s
}

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
			vis := ansi.Cut(m.rowText(n), offX, offX+textW)
			if pad := textW - ansi.StringWidth(vis); pad > 0 {
				vis += strings.Repeat(" ", pad)
			}
			switch m.rowKind(i) {
			case rowSelected:
				line = selStyle.Render(vis)
			case rowActive:
				line = activeStyle.Render(vis)
			case rowHover:
				line = m.nodeStyle(n).Background(hoverBg).Render(vis)
			default:
				line = m.nodeStyle(n).Render(vis)
			}
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
