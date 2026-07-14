// Package debugpanel is the debug tool window (0350, #580): a bottom-split
// pane composing a frames (stack) view and a variables tree for the paused
// DAP session, following the vcspanel component pattern. The panel is pure
// view/state — data arrives through setters, and user intents (select a
// frame, expand a variable) leave as messages the app resolves against the
// live session.
package debugpanel

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/dap"
	"ike/internal/theme"
)

// SelectFrameMsg reports the user activating a stack frame: the app fetches
// its scopes and navigates the editor to the frame's location.
type SelectFrameMsg struct{ Frame dap.StackFrame }

// ExpandVarMsg asks the app to fetch a variablesReference's children.
type ExpandVarMsg struct{ Ref int }

// column identifies the focused half of the panel.
type column int

const (
	colFrames column = iota
	colVars
)

// varNode is one row of the variables tree.
type varNode struct {
	v        dap.Variable
	depth    int
	expanded bool
	loaded   bool
	children []*varNode
}

// Model is the panel component (value type, pointer receivers — the pane
// registry shape).
type Model struct {
	pal     *theme.Palette
	focused bool
	w, h    int

	frames   []dap.StackFrame
	frameSel int

	// The variables tree: roots are the selected frame's scopes.
	roots  []*varNode
	varSel int

	col     column
	running bool // true between steps (no paused data to show)
}

// New returns an empty panel.
func New(pal *theme.Palette) Model { return Model{pal: pal} }

// SetSize records the pane's interior size.
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

// SetFocused records focus.
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the theme palette.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// SetFrames replaces the stack (a fresh stop) and resets the selection; the
// variables tree empties until scopes arrive.
func (m *Model) SetFrames(frames []dap.StackFrame) {
	m.frames = frames
	m.frameSel = 0
	m.roots = nil
	m.varSel = 0
	m.running = false
}

// SetRunning blanks the paused data while the debuggee runs.
func (m *Model) SetRunning() {
	m.running = true
	m.frames = nil
	m.roots = nil
	m.frameSel, m.varSel = 0, 0
}

// SetScopes replaces the variables tree's roots with the selected frame's
// scopes (each expandable via its variablesReference).
func (m *Model) SetScopes(scopes []dap.Scope) {
	m.roots = m.roots[:0]
	for _, s := range scopes {
		m.roots = append(m.roots, &varNode{
			v:     dap.Variable{Name: s.Name, VariablesReference: s.VariablesReference},
			depth: 0,
		})
	}
	m.varSel = 0
	// The first scope (Locals by convention) expands eagerly; its children
	// arrive via SetChildren once the app fetched them.
	if len(m.roots) > 0 {
		m.roots[0].expanded = true
	}
}

// SetChildren fills every tree node holding ref with the fetched variables
// and marks it expanded.
func (m *Model) SetChildren(ref int, vars []dap.Variable) {
	var fill func(nodes []*varNode)
	fill = func(nodes []*varNode) {
		for _, n := range nodes {
			if n.v.VariablesReference == ref {
				n.children = n.children[:0]
				for _, v := range vars {
					n.children = append(n.children, &varNode{v: v, depth: n.depth + 1})
				}
				n.loaded = true
				n.expanded = true
			}
			fill(n.children)
		}
	}
	fill(m.roots)
}

// SelectedFrame returns the highlighted frame (zero value when none).
func (m Model) SelectedFrame() (dap.StackFrame, bool) {
	if m.frameSel < 0 || m.frameSel >= len(m.frames) {
		return dap.StackFrame{}, false
	}
	return m.frames[m.frameSel], true
}

// flat renders the tree as visible rows in order.
func (m Model) flat() []*varNode {
	var out []*varNode
	var walk func(nodes []*varNode)
	walk = func(nodes []*varNode) {
		for _, n := range nodes {
			out = append(out, n)
			if n.expanded {
				walk(n.children)
			}
		}
	}
	walk(m.roots)
	return out
}

// Update handles panel keys: j/k (and arrows) move within the focused
// column, tab/h/l switch columns, enter activates (frame select / variable
// expand-collapse).
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch k.String() {
	case "tab", "l", "right":
		m.col = colVars
	case "h", "left":
		m.col = colFrames
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "enter", " ":
		return m.activate()
	}
	return nil
}

// move shifts the focused column's selection by delta, clamped.
func (m *Model) move(delta int) {
	if m.col == colFrames {
		m.frameSel = clamp(m.frameSel+delta, 0, len(m.frames)-1)
		return
	}
	m.varSel = clamp(m.varSel+delta, 0, len(m.flat())-1)
}

// activate runs enter on the focused column.
func (m *Model) activate() tea.Cmd {
	if m.col == colFrames {
		frame, ok := m.SelectedFrame()
		if !ok {
			return nil
		}
		return func() tea.Msg { return SelectFrameMsg{Frame: frame} }
	}
	rows := m.flat()
	if m.varSel < 0 || m.varSel >= len(rows) {
		return nil
	}
	n := rows[m.varSel]
	if n.v.VariablesReference == 0 {
		return nil // a leaf value has nothing to expand
	}
	if n.expanded {
		n.expanded = false
		return nil
	}
	if n.loaded {
		n.expanded = true
		return nil
	}
	ref := n.v.VariablesReference
	return func() tea.Msg { return ExpandVarMsg{Ref: ref} }
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// View renders the two columns side by side: frames left, variables right.
func (m Model) View() string {
	if m.w < 4 || m.h < 1 {
		return ""
	}
	if m.running {
		return " running…"
	}
	if len(m.frames) == 0 {
		return " no paused debug session"
	}
	leftW := m.w * 2 / 5
	if leftW < 16 {
		leftW = min(16, m.w/2)
	}
	rightW := m.w - leftW - 1
	left := m.renderFrames(leftW)
	right := m.renderVars(rightW)
	sep := lipgloss.NewStyle().Foreground(m.theme().Border).Render("│")
	rows := make([]string, 0, m.h)
	for i := 0; i < m.h; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		rows = append(rows, pad(l, leftW)+sep+r)
	}
	return strings.Join(rows, "\n")
}

// renderFrames renders the stack rows, selection highlighted.
func (m Model) renderFrames(w int) []string {
	title := lipgloss.NewStyle().Foreground(m.theme().Accent).Bold(true).Render(" FRAMES")
	out := []string{title}
	sel := lipgloss.NewStyle().Foreground(m.theme().Accent).Bold(true)
	dim := lipgloss.NewStyle().Foreground(m.theme().Foreground)
	for i, f := range m.frames {
		if len(out) >= m.h {
			break
		}
		label := " " + f.Name + " — " + baseOf(f.Source.Path) + ":" + strconv.Itoa(f.Line)
		label = truncate(label, w)
		style := dim
		if i == m.frameSel {
			style = sel
			if m.focused && m.col == colFrames {
				style = style.Reverse(true)
			}
		}
		out = append(out, style.Render(label))
	}
	return out
}

// renderVars renders the visible tree rows.
func (m Model) renderVars(w int) []string {
	title := lipgloss.NewStyle().Foreground(m.theme().Accent).Bold(true).Render(" VARIABLES")
	out := []string{title}
	sel := lipgloss.NewStyle().Foreground(m.theme().Accent).Bold(true)
	dim := lipgloss.NewStyle().Foreground(m.theme().Foreground)
	for i, n := range m.flat() {
		if len(out) >= m.h {
			break
		}
		marker := "  "
		if n.v.VariablesReference != 0 {
			marker = "▸ "
			if n.expanded {
				marker = "▾ "
			}
		}
		label := " " + strings.Repeat("  ", n.depth) + marker + n.v.Name
		if n.v.Value != "" {
			label += " = " + n.v.Value
		}
		label = truncate(label, w)
		style := dim
		if i == m.varSel {
			style = sel
			if m.focused && m.col == colVars {
				style = style.Reverse(true)
			}
		}
		out = append(out, style.Render(label))
	}
	return out
}

func (m Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

func pad(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func truncate(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	if w < 1 {
		return ""
	}
	if len(r) > w {
		r = r[:w-1]
	}
	return string(r) + "…"
}

func baseOf(path string) string {
	if i := strings.LastIndexAny(path, "/\\"); i >= 0 {
		return path[i+1:]
	}
	return path
}

