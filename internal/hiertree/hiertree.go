// Package hiertree is the lazily-expanding hierarchy tree the call-hierarchy
// and type-hierarchy overlays share (#2465). Both overlays were forked from
// each other and never diverged: the node shape, the depth-first visible
// walk, the expand/collapse/parent-walk key handling, the request bookkeeping
// that drops stale expansion replies, and the row renderer with its scroll
// window were identical modulo the LSP item type. They live here once.
//
// A host keeps what differs: its LSP message types, its direction flag
// (callers/callees, supertypes/subtypes) and the Apply that turns a reply into
// rows. The Tree is generic over the protocol item a row carries so the
// host's fetch continuation stays typed.
package hiertree

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/theme"
	"ike/internal/ui"
)

// Entry is the presentation and navigation payload of one row: what the
// renderer prints and where enter jumps.
type Entry struct {
	Name   string
	Detail string
	Path   string
	Line   int // 0-based; rendered 1-based
	Col    int
}

// Row is one tree node: its entry, the protocol item the host expands it
// with, and the lazily-fetched children. loaded marks a completed fetch (an
// empty children set is then a leaf); loading marks one in flight.
type Row[T any] struct {
	Entry Entry
	Item  T

	depth    int
	children []*Row[T]
	expanded bool
	loaded   bool
	loading  bool
}

// Fetch is the host-built continuation that expands one row: it issues the
// request under reqID and the host's Apply later feeds the reply back through
// Tree.Apply with the same id.
type Fetch[T any] func(reqID int, item T) tea.Cmd

// Tree is the expandable tree state: the roots it was opened on, the current
// node set, the cursor and scroll window, and the in-flight requests.
type Tree[T any] struct {
	roots []Row[T]
	nodes []*Row[T]
	fetch Fetch[T]

	// pending maps in-flight fetch request ids to the row awaiting children;
	// a rebuild replaces the map, so stale replies (their row no longer in
	// pending) are dropped.
	pending map[int]*Row[T]
	nextReq int

	cursor int // index into the visible-row sequence
	top    int // first visible row of the render window
}

// Open resets the tree onto roots with fetch as the expansion continuation
// and kicks off the first root's expansion, so the first paint is useful.
func (t *Tree[T]) Open(roots []Row[T], fetch Fetch[T]) tea.Cmd {
	t.roots = roots
	t.fetch = fetch
	return t.Rebuild()
}

// Clear drops the tree; a closed overlay holds no nodes and no requests.
func (t *Tree[T]) Clear() {
	t.nodes = nil
	t.pending = nil
}

// Rebuild resets the tree to fresh root nodes — the direction-toggle path —
// and kicks off the first root's expansion. In-flight fetches are orphaned
// (their pending entries are gone), so late replies fall on the floor. The
// request counter keeps climbing, so an orphaned id is never reused.
func (t *Tree[T]) Rebuild() tea.Cmd {
	t.pending = map[int]*Row[T]{}
	t.cursor, t.top = 0, 0
	t.nodes = make([]*Row[T], len(t.roots))
	for i := range t.roots {
		r := t.roots[i]
		t.nodes[i] = &Row[T]{Entry: r.Entry, Item: r.Item}
	}
	if len(t.nodes) == 0 {
		return nil
	}
	return t.Expand(t.nodes[0])
}

// Expand fetches (or re-shows) a row's children: a loaded row just unfolds,
// a row already in flight or a tree without a fetch does nothing.
func (t *Tree[T]) Expand(r *Row[T]) tea.Cmd {
	if r.loaded {
		r.expanded = true
		return nil
	}
	if r.loading || t.fetch == nil {
		return nil
	}
	r.loading = true
	t.nextReq++
	t.pending[t.nextReq] = r
	return t.fetch(t.nextReq, r.Item)
}

// Apply consumes one expansion reply for request reqID. A reply whose row is
// gone (tree rebuilt or cleared) is dropped; so is one the host marks stale
// (its direction no longer matches), though its request is still retired.
// Otherwise the row unfolds onto children, an empty set making it a leaf.
func (t *Tree[T]) Apply(reqID int, stale bool, children []Row[T]) {
	r := t.pending[reqID]
	if r == nil {
		return
	}
	delete(t.pending, reqID)
	if stale {
		return
	}
	r.loading = false
	r.loaded = true
	r.expanded = true
	r.children = make([]*Row[T], len(children))
	for i := range children {
		c := children[i]
		r.children[i] = &Row[T]{Entry: c.Entry, Item: c.Item, depth: r.depth + 1}
	}
}

// Visible returns the rows a depth-first walk of the expanded tree shows.
func (t *Tree[T]) Visible() []*Row[T] {
	var out []*Row[T]
	var walk func(rs []*Row[T])
	walk = func(rs []*Row[T]) {
		for _, r := range rs {
			out = append(out, r)
			if r.expanded {
				walk(r.children)
			}
		}
	}
	walk(t.nodes)
	return out
}

// Current returns the row under the cursor, nil on an empty tree.
func (t *Tree[T]) Current() *Row[T] { return t.current(t.Visible()) }

func (t *Tree[T]) current(rows []*Row[T]) *Row[T] {
	if t.cursor < 0 || t.cursor >= len(rows) {
		return nil
	}
	return rows[t.cursor]
}

// Cursor is the selected index into the visible-row sequence.
func (t *Tree[T]) Cursor() int { return t.cursor }

// Key routes one key against the tree and reports whether it consumed it.
// page is the render window's row count, the pgup/pgdn jump size (#1666):
// single steps wrap, page jumps clamp. enter hands the selected row to
// onEnter, tab calls onToggle — the host flips its direction and rebuilds.
// right/l/space expand the selected row; left/h collapse it, or on a
// collapsed row land on its parent, JetBrains-tree style. Keys the tree does
// not own (esc, q) come back unhandled for the host.
func (t *Tree[T]) Key(key string, page int, onEnter func(Row[T]) tea.Cmd, onToggle func() tea.Cmd) (tea.Cmd, bool) {
	rows := t.Visible()
	if ui.ListNav(key, &t.cursor, len(rows), page, ui.NavFull) {
		return nil, true
	}
	switch key {
	case "enter":
		if r := t.current(rows); r != nil {
			return onEnter(*r), true
		}
		return nil, true
	case "right", "l", "space":
		if r := t.current(rows); r != nil {
			if r.expanded {
				return nil, true
			}
			return t.Expand(r), true
		}
		return nil, true
	case "left", "h":
		r := t.current(rows)
		if r == nil {
			return nil, true
		}
		if r.expanded {
			r.expanded = false
			return nil, true
		}
		// Collapsed already: land on the parent.
		for i := t.cursor - 1; i >= 0; i-- {
			if rows[i].depth < r.depth {
				t.cursor = i
				break
			}
		}
		return nil, true
	case "tab":
		return onToggle(), true
	}
	return nil, false
}

// RenderRows lays the visible tree out to width×height, scrolled so the
// cursor row stays in the window (the cursor is clamped first, so a tree that
// shrank under it still has a selection). An empty tree renders the dim
// empty notice as its single row. displayPath formats the row's path; nil
// prints it verbatim.
func (t *Tree[T]) RenderRows(width, height int, pal *theme.Palette, displayPath func(string) string, empty string) []string {
	rows := t.Visible()
	if len(rows) == 0 {
		return []string{lipgloss.NewStyle().Faint(true).Render(empty)}
	}
	t.cursor = ui.ClampIndex(t.cursor, len(rows))
	t.top = ui.ScrollToShow(t.top, t.cursor, height, len(rows))

	sel := lipgloss.NewStyle().Background(pal.SelectionMuted)
	name := lipgloss.NewStyle().Bold(true)
	dim := lipgloss.NewStyle().Faint(true)
	clip := lipgloss.NewStyle().MaxWidth(width)

	if displayPath == nil {
		displayPath = func(p string) string { return p }
	}

	var out []string
	for i := t.top; i < len(rows) && i < t.top+height; i++ {
		r := rows[i]
		marker := "▸"
		switch {
		case r.loading:
			marker = "…"
		case r.expanded:
			marker = "▾"
		case r.loaded && len(r.children) == 0:
			marker = "·"
		}
		indent := strings.Repeat("  ", r.depth)
		loc := displayPath(r.Entry.Path) + ":" + strconv.Itoa(r.Entry.Line+1)
		detail := ""
		if r.Entry.Detail != "" {
			detail = " " + r.Entry.Detail
		}
		if i == t.cursor {
			row := indent + marker + " " + r.Entry.Name + detail + "  " + loc
			out = append(out, clip.Render(sel.Render(row)))
			continue
		}
		row := dim.Render(indent+marker+" ") + name.Render(r.Entry.Name) +
			dim.Render(detail+"  "+loc)
		out = append(out, clip.Render(row))
	}
	return out
}
