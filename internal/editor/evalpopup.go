package editor

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// evalpopup.go is the debugger's evaluate popup (#2174): a cursor-anchored
// box holding the result of one DAP `evaluate` — the selection, or an
// expression the user typed — with its structured children expandable in
// place through `variables` requests. A sibling of the peek popup (#1154):
// same frame (#316), same app-side compositing, and it owns a few keys while
// open (move, expand/collapse, esc) while any other key dismisses it and is
// handled normally.
//
// The editor stays protocol-free: the app hands rows over as EvalVar values
// (like DebugLocal for inline values) and answers EvalExpandMsg with
// SetEvalChildren.

// evalVisibleRows caps how many result rows the popup shows at once; a deeper
// tree scrolls with the selection. evalKeyHint is the footer line.
const (
	evalVisibleRows = 10
	evalKeyHint     = "enter: expand · esc: close"
)

// EvalVar is one row of the evaluate popup's result tree, in the app's terms:
// a rendered name/value pair with the adapter's structured reference (0 for a
// leaf).
type EvalVar struct {
	Name  string
	Value string
	Type  string
	Ref   int
}

// EvalExpandMsg asks the app to fetch Ref's children for the open evaluate
// popup; the answer comes back through SetEvalChildren.
type EvalExpandMsg struct{ Ref int }

// evalNode is one tree row: the value plus its expansion state. loaded marks
// children already fetched, so a re-expand never re-requests.
type evalNode struct {
	v        EvalVar
	depth    int
	expanded bool
	loaded   bool
	children []*evalNode
}

// evalState is the open popup: the evaluated expression as a header, the
// result tree, the selected row and the scroll offset into the flattened
// rows.
type evalState struct {
	expr   string
	roots  []*evalNode
	sel    int
	scroll int
}

// OpenEvalResult opens the evaluate popup over this editor showing res as the
// answer to expr. A structured result (non-zero Ref) starts collapsed —
// expanding it costs a request, so it is the user's call.
func (m *Model) OpenEvalResult(expr string, res EvalVar) {
	if res.Name == "" {
		res.Name = expr
	}
	m.eval = &evalState{expr: expr, roots: []*evalNode{{v: res}}}
}

// EvalOpen reports whether the evaluate popup is showing.
func (m Model) EvalOpen() bool { return m.eval != nil && len(m.eval.roots) > 0 }

// EvalAnchor returns the buffer-relative cell the popup anchors to: the
// cursor, like hover and peek.
func (m Model) EvalAnchor() (col, line int) { return m.cursor.Col, m.cursor.Line }

// DismissEval closes the evaluate popup (the session ended, the debuggee
// resumed, the pane lost the buffer).
func (m *Model) DismissEval() { m.eval = nil }

// SetEvalChildren fills every popup node holding ref with the fetched rows
// and expands it — the answer to EvalExpandMsg. A ref the popup no longer
// shows (it was closed and reopened meanwhile) is simply dropped.
func (m *Model) SetEvalChildren(ref int, vars []EvalVar) {
	if m.eval == nil || ref == 0 {
		return
	}
	var fill func(nodes []*evalNode)
	fill = func(nodes []*evalNode) {
		for _, n := range nodes {
			if n.v.Ref == ref {
				n.children = n.children[:0]
				for _, v := range vars {
					n.children = append(n.children, &evalNode{v: v, depth: n.depth + 1})
				}
				n.loaded = true
				n.expanded = true
			}
			fill(n.children)
		}
	}
	fill(m.eval.roots)
	m.clampEvalSel()
}

// evalRows flattens the visible tree in render order.
func (e *evalState) rows() []*evalNode {
	var out []*evalNode
	var walk func(nodes []*evalNode)
	walk = func(nodes []*evalNode) {
		for _, n := range nodes {
			out = append(out, n)
			if n.expanded {
				walk(n.children)
			}
		}
	}
	walk(e.roots)
	return out
}

// clampEvalSel keeps the selection and the scroll window inside the row count
// after the tree changed shape.
func (m *Model) clampEvalSel() {
	e := m.eval
	rows := len(e.rows())
	if rows == 0 {
		e.sel, e.scroll = 0, 0
		return
	}
	if e.sel >= rows {
		e.sel = rows - 1
	}
	if e.sel < 0 {
		e.sel = 0
	}
	if e.sel < e.scroll {
		e.scroll = e.sel
	}
	if e.sel > e.scroll+evalVisibleRows-1 {
		e.scroll = e.sel - evalVisibleRows + 1
	}
	if maxTop := rows - evalVisibleRows; e.scroll > maxTop {
		e.scroll = maxTop
	}
	if e.scroll < 0 {
		e.scroll = 0
	}
}

// evalKey handles a key while the evaluate popup is open. It returns true
// when the key was consumed (close, move, expand/collapse); any other key
// closes the popup and returns false so normal dispatch handles it — the
// peek/hover dismiss precedent.
func (m *Model) evalKey(key tea.KeyPressMsg) (bool, tea.Cmd) {
	switch {
	case key.Code == tea.KeyEscape:
		m.DismissEval()
		return true, nil
	case key.Code == tea.KeyDown, key.Code == 'j' && key.Mod == 0:
		m.evalMove(1)
		return true, nil
	case key.Code == tea.KeyUp, key.Code == 'k' && key.Mod == 0:
		m.evalMove(-1)
		return true, nil
	case key.Code == tea.KeyLeft, key.Code == 'h' && key.Mod == 0:
		m.evalCollapse()
		return true, nil
	case key.Code == tea.KeyEnter, key.Code == tea.KeySpace,
		key.Code == tea.KeyRight, key.Code == 'l' && key.Mod == 0:
		return true, m.evalToggle()
	}
	m.DismissEval()
	return false, nil
}

// evalMove steps the selection by delta, clamped (a bounded tree reads better
// without wrap-around here — the popup is a peek, not a list pane).
func (m *Model) evalMove(delta int) {
	m.eval.sel += delta
	m.clampEvalSel()
}

// evalCollapse folds the selected row, or steps to its parent when it is
// already a leaf — the usual tree-left semantics.
func (m *Model) evalCollapse() {
	e := m.eval
	rows := e.rows()
	if e.sel < 0 || e.sel >= len(rows) {
		return
	}
	n := rows[e.sel]
	if n.expanded {
		n.expanded = false
		m.clampEvalSel()
		return
	}
	for i := e.sel - 1; i >= 0; i-- {
		if rows[i].depth < n.depth {
			e.sel = i
			break
		}
	}
	m.clampEvalSel()
}

// evalToggle expands or folds the selected row. An unfetched structured value
// asks the app for its children (EvalExpandMsg); a leaf does nothing.
func (m *Model) evalToggle() tea.Cmd {
	e := m.eval
	rows := e.rows()
	if e.sel < 0 || e.sel >= len(rows) {
		return nil
	}
	n := rows[e.sel]
	if n.expanded {
		n.expanded = false
		m.clampEvalSel()
		return nil
	}
	if n.loaded {
		n.expanded = true
		m.clampEvalSel()
		return nil
	}
	if n.v.Ref == 0 {
		return nil
	}
	ref := n.v.Ref
	return func() tea.Msg { return EvalExpandMsg{Ref: ref} }
}

// EvalView renders the popup: a bold header naming the evaluated expression,
// a rule, and the visible window of result rows with the selection
// highlighted. Rows are truncated (not wrapped) at the popup width cap; dim
// ellipsis rows mark content scrolled out of view.
func (m Model) EvalView() string {
	e := m.eval
	if e == nil || len(e.roots) == 0 {
		return ""
	}
	rows := e.rows()
	end := e.scroll + evalVisibleRows
	if end > len(rows) {
		end = len(rows)
	}
	th := m.theme()
	dim := lipgloss.NewStyle().Foreground(th.Border)
	sel := lipgloss.NewStyle().Foreground(th.Accent).Bold(true)
	plain := lipgloss.NewStyle().Foreground(th.Foreground)

	header := "eval: " + e.expr
	lines := make([]string, 0, end-e.scroll)
	for i := e.scroll; i < end; i++ {
		lines = append(lines, evalRowText(rows[i]))
	}
	// The key hint counts towards the width: a short result would otherwise
	// leave the box too narrow to read its own footer.
	maxW := m.popupMaxWidth()
	width := max(lipgloss.Width(header), lipgloss.Width(evalKeyHint))
	for _, l := range lines {
		if w := lipgloss.Width(l); w > width {
			width = w
		}
	}
	if width > maxW {
		width = maxW
	}

	out := []string{
		sel.Render(ansi.Truncate(header, width, "…")),
		dim.Render(strings.Repeat("─", width)),
	}
	if e.scroll > 0 {
		out = append(out, dim.Render("…"))
	}
	for i, l := range lines {
		style := plain
		if e.scroll+i == e.sel {
			style = sel.Reverse(true)
		}
		out = append(out, style.Render(ansi.Truncate(l, width, "…")))
	}
	if end < len(rows) {
		out = append(out, dim.Render("…"))
	}
	out = append(out, dim.Render(ansi.Truncate(evalKeyHint, width, "…")))
	return m.popupFrame().Padding(0, 1).Render(strings.Join(out, "\n"))
}

// evalRowText renders one row: indent, an expansion marker for a structured
// value, the name and the value.
func evalRowText(n *evalNode) string {
	marker := "  "
	if n.v.Ref != 0 || len(n.children) > 0 {
		marker = "▸ "
		if n.expanded {
			marker = "▾ "
		}
	}
	line := strings.Repeat("  ", n.depth) + marker + n.v.Name
	if n.v.Value != "" {
		line += " = " + n.v.Value
	}
	if n.v.Type != "" {
		line += "  (" + n.v.Type + ")"
	}
	return line
}
