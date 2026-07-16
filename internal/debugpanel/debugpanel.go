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
	"time"

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

// SetVarMsg asks the app to change a variable's value via the DAP setVariable
// request: Ref is the containing variablesReference, Name the variable, Value
// the new expression. The app refreshes the tree from the adapter's response.
type SetVarMsg struct {
	Ref   int
	Name  string
	Value string
}

// column identifies the focused half of the panel.
type column int

const (
	colFrames column = iota
	colVars
)

// varNode is one row of the variables tree.
type varNode struct {
	v         dap.Variable
	depth     int
	expanded  bool
	loaded    bool
	children  []*varNode
	parentRef int // the variablesReference this node lives under (0 for scope roots)
}

// Model is the panel component (value type, pointer receivers — the pane
// registry shape).
type Model struct {
	pal     *theme.Palette
	focused bool
	w, h    int

	frames   []dap.StackFrame
	frameSel int
	frameTop int // first visible frame row (wheel/keyboard scroll)

	// The variables tree: roots are the selected frame's scopes.
	roots  []*varNode
	varSel int
	varTop int // first visible variable row

	col     column
	running bool // true between steps (no paused data to show)

	// Mouse double-click tracking (#626), mirroring the vcs panel; now is
	// injectable so tests drive the clock.
	now          func() time.Time
	lastClickCol column
	lastClickRow int
	lastClickAt  time.Time

	// Variable-value editing (#627). canEdit mirrors the adapter's
	// supportsSetVariable capability; while editing, editRef/editName identify
	// the target and editBuf/editCur hold the inline line editor.
	canEdit  bool
	editing  bool
	editRef  int
	editName string
	editBuf  []rune
	editCur  int
}

// New returns an empty panel.
func New(pal *theme.Palette) Model { return Model{pal: pal, now: time.Now} }

// SetSize records the pane's interior size.
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

// SetFocused records focus.
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the theme palette.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// SetEditable records whether the adapter supports setVariable (#627); when
// false, the edit affordance is disabled.
func (m *Model) SetEditable(v bool) { m.canEdit = v }

// Editing reports whether an inline value editor is open, so the app routes
// every key to the panel instead of the global keymap.
func (m Model) Editing() bool { return m.editing }

// cancelEdit closes the inline editor without committing.
func (m *Model) cancelEdit() {
	m.editing = false
	m.editBuf = nil
	m.editCur = 0
	m.editName = ""
	m.editRef = 0
}

// SetFrames replaces the stack (a fresh stop) and resets the selection; the
// variables tree empties until scopes arrive.
func (m *Model) SetFrames(frames []dap.StackFrame) {
	m.frames = frames
	m.frameSel = 0
	m.frameTop = 0
	m.roots = nil
	m.varSel = 0
	m.varTop = 0
	m.running = false
	m.cancelEdit()
}

// SetRunning blanks the paused data while the debuggee runs.
func (m *Model) SetRunning() {
	m.running = true
	m.frames = nil
	m.roots = nil
	m.frameSel, m.varSel = 0, 0
	m.frameTop, m.varTop = 0, 0
	m.cancelEdit()
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
	m.varTop = 0
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
					n.children = append(n.children, &varNode{v: v, depth: n.depth + 1, parentRef: ref})
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
	if m.editing {
		return m.editKey(k)
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
	case "e":
		m.startEdit()
	case "enter", " ":
		return m.activate()
	}
	return nil
}

// startEdit opens the inline value editor on the selected variable, when the
// adapter supports setVariable and the row is an editable child (not a scope).
func (m *Model) startEdit() {
	if !m.canEdit || m.col != colVars {
		return
	}
	rows := m.flat()
	if m.varSel < 0 || m.varSel >= len(rows) {
		return
	}
	n := rows[m.varSel]
	if n.parentRef == 0 { // a scope root has no settable value
		return
	}
	m.editing = true
	m.editRef = n.parentRef
	m.editName = n.v.Name
	m.editBuf = []rune(n.v.Value)
	m.editCur = len(m.editBuf)
}

// editKey drives the inline line editor: printable runes insert, the usual
// motions edit, enter commits (emitting SetVarMsg), esc cancels.
func (m *Model) editKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.Code {
	case tea.KeyEnter:
		ref, name, val := m.editRef, m.editName, string(m.editBuf)
		m.cancelEdit()
		return func() tea.Msg { return SetVarMsg{Ref: ref, Name: name, Value: val} }
	case tea.KeyEscape:
		m.cancelEdit()
		return nil
	case tea.KeyBackspace:
		if m.editCur > 0 {
			m.editBuf = append(m.editBuf[:m.editCur-1], m.editBuf[m.editCur:]...)
			m.editCur--
		}
		return nil
	case tea.KeyLeft:
		if m.editCur > 0 {
			m.editCur--
		}
		return nil
	case tea.KeyRight:
		if m.editCur < len(m.editBuf) {
			m.editCur++
		}
		return nil
	case tea.KeyHome:
		m.editCur = 0
		return nil
	case tea.KeyEnd:
		m.editCur = len(m.editBuf)
		return nil
	}
	if k.Text != "" {
		runes := []rune(k.Text)
		m.editBuf = append(m.editBuf[:m.editCur], append(runes, m.editBuf[m.editCur:]...)...)
		m.editCur += len(runes)
	}
	return nil
}

// move shifts the focused column's selection by delta, clamped, and scrolls
// the column so the selection stays visible.
func (m *Model) move(delta int) {
	if m.col == colFrames {
		m.frameSel = clamp(m.frameSel+delta, 0, len(m.frames)-1)
		m.frameTop = scrollToShow(m.frameTop, m.frameSel, m.bodyHeight(), len(m.frames))
		return
	}
	m.varSel = clamp(m.varSel+delta, 0, len(m.flat())-1)
	m.varTop = scrollToShow(m.varTop, m.varSel, m.bodyHeight(), len(m.flat()))
}

// bodyHeight is the number of list rows visible under the column title.
func (m Model) bodyHeight() int {
	if m.h <= 1 {
		return 0
	}
	return m.h - 1
}

// scrollToShow nudges top so sel lands within [top, top+body-1], clamped to the
// row count.
func scrollToShow(top, sel, body, count int) int {
	if body <= 0 {
		return clamp(top, 0, max(0, count-1))
	}
	if sel < top {
		top = sel
	} else if sel > top+body-1 {
		top = sel - body + 1
	}
	return clamp(top, 0, max(0, count-body))
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
	for i := m.frameTop; i < len(m.frames); i++ {
		if len(out) >= m.h {
			break
		}
		f := m.frames[i]
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
	rows := m.flat()
	for i := m.varTop; i < len(rows); i++ {
		if len(out) >= m.h {
			break
		}
		n := rows[i]
		marker := "  "
		if n.v.VariablesReference != 0 {
			marker = "▸ "
			if n.expanded {
				marker = "▾ "
			}
		}
		// The row being edited shows the inline value editor with a cursor.
		if m.editing && i == m.varSel {
			prefix := " " + strings.Repeat("  ", n.depth) + marker + n.v.Name + " = "
			before, after := string(m.editBuf[:m.editCur]), string(m.editBuf[m.editCur:])
			cursor := lipgloss.NewStyle().Reverse(true).Render(" ")
			if len(after) > 0 {
				cursor = lipgloss.NewStyle().Reverse(true).Render(string([]rune(after)[0]))
				after = string([]rune(after)[1:])
			}
			editStyle := lipgloss.NewStyle().Foreground(m.theme().Accent)
			out = append(out, editStyle.Render(truncate(prefix+before, w))+cursor+editStyle.Render(after))
			continue
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
