// Package explorer implements the file-tree pane: it shows the project directory
// as an expandable tree rooted at a fixed base (the explorer never ascends above
// it), lets the user expand/collapse folders in place with vim-like keys, and
// opens a file by emitting an OpenFileMsg the root model routes to the editor.
package explorer

import (
	"os"
	"path/filepath"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	m := Model{root: root}
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
	if m.height <= 0 {
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+m.height {
		m.offset = m.cursor - m.height + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// View renders the tree.
func (m Model) View() string {
	if m.err != nil {
		return "error: " + m.err.Error()
	}
	if len(m.rows) == 0 {
		return lipgloss.NewStyle().Faint(true).Render("(empty)")
	}
	end := m.offset + m.height
	if m.height <= 0 || end > len(m.rows) {
		end = len(m.rows)
	}
	sel := lipgloss.NewStyle().Foreground(lipgloss.Color("69")).Bold(true)
	dir := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

	var lines []string
	for i := m.offset; i < end; i++ {
		n := m.rows[i]
		indent := ""
		for d := 0; d < n.depth; d++ {
			indent += "  "
		}

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

		switch {
		case i == m.cursor && m.focused:
			lines = append(lines, indent+sel.Render(marker+label))
		case n.isDir:
			lines = append(lines, indent+marker+dir.Render(label))
		default:
			lines = append(lines, indent+marker+label)
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}
