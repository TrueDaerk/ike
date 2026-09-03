// Package debugpanel is the debug tool window (0350, #580): a bottom-split
// pane composing a frames (stack) view and a variables tree for the paused
// DAP session, following the vcspanel component pattern. The panel is pure
// view/state — data arrives through setters, and user intents (select a
// frame, expand a variable) leave as messages the app resolves against the
// live session. Debuggee output lives in its own terminal pane beside the
// panel (#1370) — the former Output column is gone.
package debugpanel

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/dap"
	"ike/internal/terminal"
	"ike/internal/theme"
	"ike/internal/ui"
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
	// isWatch marks a watch-expression row (#1914): v.Name is the expression,
	// watchIdx its index in the app's list (-1 for the pending add row).
	isWatch  bool
	watchIdx int
}

// Model is the panel component (value type, pointer receivers — the pane
// registry shape).
type Model struct {
	pal     *theme.Palette
	focused bool
	// w/h is the body interior (under the tab bar); outerH the pane's full
	// interior height as SetSize received it (#2190).
	w, h   int
	outerH int

	// The combined debug area (#2190): term is the debuggee's console
	// terminal, hosted behind the internal tab bar; tab the visible view and
	// tabTouched whether the user picked it (AutoTab then stops overriding).
	term       *terminal.Model
	tab        Tab
	tabTouched bool

	frames   []dap.StackFrame
	frameSel int
	frameTop int // first visible frame row (wheel/keyboard scroll)

	// The variables tree: roots are the selected frame's scopes, led by the
	// watches section when expressions exist (#1914).
	roots     []*varNode
	watchRoot *varNode
	varSel    int
	varTop    int // first visible variable row

	col     column
	running bool // true between steps (no paused data to show)

	// User-dragged frames-column width in cells (#691); zero means the
	// built-in default. Stored exact — not as a re-encoded fraction — so it
	// never drifts (#695). SetSize rescales it proportionally via lastUsable.
	// Session-local, like scroll.
	colFW      int
	lastUsable int

	// finished marks a terminated session (#689): the panel stays open beside
	// the debuggee's terminal pane; frames show the exit status instead.
	finished bool
	exitCode int
	hasExit  bool

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
	edit     ui.Field
	// editWatch marks the open editor as a watch-expression edit (#1914):
	// editWatchIdx targets the app's list (-1 = the pending add row).
	editWatch    bool
	editWatchIdx int

	// noEval mirrors an adapter that refused evaluate (#2174): watch rows
	// stay listed and editable, but the section header says why they carry
	// no values.
	noEval bool
}

// New returns an empty panel.
func New(pal *theme.Palette) Model { return Model{pal: pal, now: time.Now} }

// SetSize records the pane's interior size. A user-dragged column width
// rescales proportionally — the only place it is converted, so drags
// themselves stay drift-free (#695).
func (m *Model) SetSize(w, h int) {
	m.w, m.outerH = w, h
	m.applySize()
	if usable := w - 1; m.colFW > 0 && m.lastUsable > 0 && usable > 0 && usable != m.lastUsable {
		m.colFW = m.colFW * usable / m.lastUsable
		m.lastUsable = usable
	}
}

// SetFocused records focus; the console terminal carries it while it is the
// visible view (#2190).
func (m *Model) SetFocused(f bool) {
	m.focused = f
	if m.term != nil {
		m.term.SetFocused(f && m.tab == TabConsole)
	}
}

// SetPalette re-threads the theme palette, console terminal included.
func (m *Model) SetPalette(p *theme.Palette) {
	m.pal = p
	if m.term != nil {
		m.term.SetPalette(p)
	}
}

// SetEditable records whether the adapter supports setVariable (#627); when
// false, the edit affordance is disabled.
func (m *Model) SetEditable(v bool) { m.canEdit = v }

// Editable reports the recorded setVariable capability (#640).
func (m Model) Editable() bool { return m.canEdit }

// Editing reports whether an inline value editor is open, so the app routes
// every key to the panel instead of the global keymap.
func (m Model) Editing() bool { return m.editing }

// cancelEdit closes the inline editor without committing; a pending add-watch
// placeholder row leaves with it (#1914).
func (m *Model) cancelEdit() {
	wasWatchAdd := m.editing && m.editWatch && m.editWatchIdx < 0
	m.editing = false
	m.edit.Clear()
	m.editName = ""
	m.editRef = 0
	m.editWatch = false
	m.editWatchIdx = 0
	if wasWatchAdd {
		m.dropWatchPlaceholder()
	}
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
	m.finished = false
	m.cancelEdit()
}

// SetRunning marks the debuggee as running. The last stop's frames and
// variables stay visible as stale context (#693) — rendered dimmed behind a
// `running…` indicator — but every interaction that needs a paused adapter
// (frame activation, variable expansion, inline editing) is gated off, and an
// open inline editor is cancelled (#640).
func (m *Model) SetRunning() {
	m.running = true
	m.finished = false
	m.cancelEdit()
}

// SetFinished marks the session as terminated (#689): paused data is cleared;
// the debuggee's output stays reviewable in its terminal pane (#1370) until
// the user closes it or a new session resets it.
func (m *Model) SetFinished(exitCode int, hasCode bool) {
	m.finished = true
	m.exitCode = exitCode
	m.hasExit = hasCode
	m.running = false
	m.frames = nil
	m.roots = nil
	m.watchRoot = nil // values are stale; the app re-pushes the expressions
	m.frameSel, m.varSel = 0, 0
	m.frameTop, m.varTop = 0, 0
	m.cancelEdit()
}

// Finished reports whether the panel shows a terminated session.
func (m Model) Finished() bool { return m.finished }

// ResetSession clears everything a previous session left behind — the
// finished marker and stale frames/variables — for a fresh launch that reuses
// the still-open panel (#689).
func (m *Model) ResetSession() {
	m.tabTouched = false // the automatic view selection starts fresh (#2190)
	m.finished = false
	m.hasExit = false
	m.exitCode = 0
	m.running = false
	m.frames = nil
	m.roots = nil
	m.watchRoot = nil
	m.frameSel, m.varSel = 0, 0
	m.frameTop, m.varTop = 0, 0
	m.cancelEdit()
}

// SetScopes replaces the variables tree's roots with the selected frame's
// scopes (each expandable via its variablesReference). An open inline editor
// is cancelled — the tree it edited is being replaced (#640).
func (m *Model) SetScopes(scopes []dap.Scope) {
	m.cancelEdit()
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
// and marks it expanded. An open inline editor is cancelled: the refresh may
// replace the very row being edited, and Enter would commit a stale
// ref/name (#640).
func (m *Model) SetChildren(ref int, vars []dap.Variable) {
	m.cancelEdit()
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
	if m.watchRoot != nil {
		// A structured watch result expands like any variable (#1914); the
		// section root itself never matches (it has no reference).
		fill(m.watchRoot.children)
	}
}

// SelectedFrame returns the highlighted frame (zero value when none).
func (m Model) SelectedFrame() (dap.StackFrame, bool) {
	if m.frameSel < 0 || m.frameSel >= len(m.frames) {
		return dap.StackFrame{}, false
	}
	return m.frames[m.frameSel], true
}

// flat renders the tree as visible rows in order: the watches section leads
// (#1914), the selected frame's scopes follow.
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
	if m.watchRoot != nil {
		walk([]*varNode{m.watchRoot})
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
	// The console view owns the keys while visible (#2190): a PTY debuggee
	// gets everything, a pipe console keeps tab/shift+tab as the way back.
	if m.ConsoleActive() {
		return m.consoleKey(k)
	}
	if m.editing {
		return m.editKey(k)
	}
	// Shared list semantics (#1666): steps wrap, page jumps clamp. h/l are
	// the column switch here, so only j/k come from the vim set.
	if m.listNav(k.String()) {
		return nil
	}
	switch k.String() {
	case "tab", "shift+tab":
		// With a console installed, tab cycles the panel's views (#2190) —
		// h/l keep switching the columns; without one it stays the historic
		// column switch.
		if m.term != nil {
			m.SetTab(TabConsole)
			return nil
		}
		if k.String() == "tab" && m.col < colVars {
			m.col++
		}
	case "l", "right":
		if m.col < colVars {
			m.col++
		}
	case "h", "left":
		if m.col > colFrames {
			m.col--
		}
	case "a":
		// Add a watch expression (#1914); allowed while running — it
		// evaluates on the next stop.
		if m.col == colVars && !m.finished {
			m.startWatchAdd()
		}
	case "d", "delete", "backspace":
		// Remove the selected watch row (#1914); other rows ignore the key.
		if m.col == colVars {
			if n, ok := m.selectedVar(); ok && n.isWatch && n.watchIdx >= 0 {
				idx := n.watchIdx
				return func() tea.Msg { return RemoveWatchMsg{Index: idx} }
			}
		}
	case "e":
		m.startEdit()
	case "enter", " ":
		return m.activate()
	}
	return nil
}

// selectedVar returns the variables column's selected row.
func (m Model) selectedVar() (*varNode, bool) {
	rows := m.flat()
	if m.varSel < 0 || m.varSel >= len(rows) {
		return nil, false
	}
	return rows[m.varSel], true
}

// startEdit opens the inline editor on the selected row: a watch row edits
// its expression (#1914, no capability needed), a variable row edits its
// value when the adapter supports setVariable.
func (m *Model) startEdit() {
	if m.col != colVars {
		return
	}
	n, ok := m.selectedVar()
	if !ok {
		return
	}
	if n.isWatch && n.watchIdx >= 0 {
		m.startWatchEdit(n)
		return
	}
	if !m.canEdit || m.running {
		return
	}
	if n.parentRef == 0 { // a scope root has no settable value
		return
	}
	m.editing = true
	m.editRef = n.parentRef
	m.editName = n.v.Name
	m.edit.Set(n.v.Value)
}

// editKey drives the inline line editor: printable runes insert, the usual
// motions edit, enter commits (emitting SetVarMsg), esc cancels.
func (m *Model) editKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.Code {
	case tea.KeyEnter:
		if m.editWatch {
			idx, expr := m.editWatchIdx, strings.TrimSpace(m.edit.Text)
			m.cancelEdit()
			return m.commitWatch(idx, expr)
		}
		ref, name, val := m.editRef, m.editName, m.edit.Text
		m.cancelEdit()
		return func() tea.Msg { return SetVarMsg{Ref: ref, Name: name, Value: val} }
	case tea.KeyEscape:
		m.cancelEdit()
		return nil
	}
	// Everything else is shared line editing (#2002): a movable cursor with
	// word motions, word/line kills, the macOS opt/cmd chords and rune-safe
	// backspace, plus printable insertion at the cursor.
	m.edit.Key(k)
	return nil
}

// PasteText inserts a pasted block into the open inline editor at its cursor
// (#2002); it reports whether anything was consumed, so a closed editor lets
// the paste fall through. While the console view is visible the paste belongs
// to the terminal (#2190) — a PTY debuggee's stdin, like any terminal pane.
func (m *Model) PasteText(text string) bool {
	if m.ConsoleActive() {
		m.term.PasteText(text)
		return true
	}
	if !m.editing {
		return false
	}
	return m.edit.Paste(text)
}

// listNav routes a navigation key at the focused column and reports whether
// it consumed it (#1666): steps wrap, page jumps clamp, and the column
// scrolls so the selection stays visible.
func (m *Model) listNav(key string) bool {
	sel, rows := &m.varSel, len(m.flat())
	if m.col == colFrames {
		sel, rows = &m.frameSel, len(m.frames)
	}
	if !ui.ListNav(key, sel, rows, m.bodyHeight(), ui.NavArrows|ui.NavVim|ui.NavHomeEnd) {
		return false
	}
	if m.col == colFrames {
		m.frameTop = scrollToShow(m.frameTop, m.frameSel, m.bodyHeight(), len(m.frames))
	} else {
		m.varTop = scrollToShow(m.varTop, m.varSel, m.bodyHeight(), len(m.flat()))
	}
	return true
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
	if m.running {
		// Stale rows (#693): frame scopes, variable expansion and navigation
		// all need a paused adapter — a running one refuses or hangs.
		return nil
	}
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
		// The watches section root toggles locally (#1914); a leaf value has
		// nothing to expand.
		if len(n.children) > 0 {
			n.expanded = !n.expanded
			m.clampVarSel()
		}
		return nil
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

// View renders the two columns side by side: frames, variables. The columns
// render in every state (#637) — while the debuggee runs or before the first
// stop the frames column shows a placeholder.
func (m Model) View() string {
	if m.w < 4 || m.h < 1 {
		return ""
	}
	// The combined area (#2190): the tab bar leads, the active view follows —
	// the variables body or the console terminal, both sized to the rows
	// underneath.
	if m.term != nil {
		if m.tab == TabConsole {
			return m.tabBar() + "\n" + m.term.View()
		}
		return m.tabBar() + "\n" + m.bodyView()
	}
	return m.bodyView()
}

// bodyView renders the frames │ variables columns over the body height.
func (m Model) bodyView() string {
	fw, vw := m.colWidths()
	frames := m.renderFrames(fw)
	vars := m.renderVars(vw)
	sep := lipgloss.NewStyle().Foreground(m.theme().Border).Render("│")
	rows := make([]string, 0, m.h)
	for i := 0; i < m.h; i++ {
		rows = append(rows, pad(rowAt(frames, i), fw)+sep+rowAt(vars, i))
	}
	return strings.Join(rows, "\n")
}

// colWidths splits the interior into frames | variables, reserving one cell
// for the separator.
// minColWidth is the smallest width a column may be dragged to (#691),
// mirroring the layout tree's minCell idea so no column collapses.
const minColWidth = 8

func (m Model) colWidths() (frames, vars int) {
	usable := m.w - 1 // one separator
	if usable < 2 {
		usable = 2
	}
	if m.colFW > 0 {
		// A user-adjusted width (#691), exact cells (#695), clamped so the
		// variables column keeps its minimum where space allows.
		frames = clampCol(m.colFW, usable-minColWidth)
		return frames, usable - frames
	}
	frames = usable * 2 / 5
	if frames < 12 {
		frames = min(12, usable/2)
	}
	return frames, usable - frames
}

// clampCol bounds a dragged column width to [minColWidth, max]; a panel too
// narrow to honour the minimum degrades to an even floor instead.
func clampCol(w, max int) int {
	if max < minColWidth {
		max = minColWidth
	}
	return clamp(w, minColWidth, max)
}

// SeparatorHit reports whether content-local x sits on the frames│variables
// separator (0) or not (-1), so the app can start a resize drag (#691)
// instead of a row click.
func (m Model) SeparatorHit(x int) int {
	if m.ConsoleActive() {
		return -1 // the console view has no column separator
	}
	fw, _ := m.colWidths()
	if x == fw {
		return 0
	}
	return -1
}

// ResizeSeparator drags the frames│variables separator to content-local x
// (#691). The width is stored in exact cells so it never drifts (#695).
func (m *Model) ResizeSeparator(sep, x int) {
	if sep != 0 {
		return
	}
	usable := m.w - 1
	if usable < 2*minColWidth {
		return
	}
	m.colFW = clamp(x, minColWidth, usable-minColWidth)
	m.lastUsable = usable
}

func rowAt(rows []string, i int) string {
	if i < len(rows) {
		return rows[i]
	}
	return ""
}

// renderFrames renders the stack rows, selection highlighted. With no paused
// data (running, or no stop yet) a placeholder row stands in (#637) — the
// other column renders regardless.
func (m Model) renderFrames(w int) []string {
	title := lipgloss.NewStyle().Foreground(m.theme().Accent).Bold(true).Render(" FRAMES")
	out := []string{title}
	sel := lipgloss.NewStyle().Foreground(m.theme().Accent).Bold(true)
	dim := lipgloss.NewStyle().Foreground(m.theme().Foreground)
	if m.running {
		// Stale context (#693): the indicator row leads, the last stop's
		// frames follow faint so they read as historical, not live.
		out = append(out, dim.Render(truncate(" running…", w)))
		stale := lipgloss.NewStyle().Foreground(m.theme().Foreground).Faint(true)
		for i := m.frameTop; i < len(m.frames); i++ {
			if len(out) >= m.h {
				break
			}
			f := m.frames[i]
			label := " " + f.Name + " — " + baseOf(f.Source.Path) + ":" + strconv.Itoa(f.Line)
			out = append(out, stale.Render(truncate(label, w)))
		}
		return out
	}
	if m.finished {
		label := " finished"
		if m.hasExit {
			label += " (exit code " + strconv.Itoa(m.exitCode) + ")"
		}
		return append(out, dim.Render(truncate(label, w)))
	}
	if len(m.frames) == 0 {
		return append(out, dim.Render(truncate(" not paused", w)))
	}
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
	if m.running {
		// Stale context while the debuggee runs (#693): everything faint, no
		// selection emphasis — the tree is historical until the next stop.
		sel = lipgloss.NewStyle().Foreground(m.theme().Foreground).Faint(true)
		dim = sel
	}
	rows := m.flat()
	for i := m.varTop; i < len(rows); i++ {
		if len(out) >= m.h {
			break
		}
		n := rows[i]
		marker := "  "
		if n.v.VariablesReference != 0 || len(n.children) > 0 {
			marker = "▸ "
			if n.expanded {
				marker = "▾ "
			}
		}
		// The row being edited shows the inline value editor with a cursor.
		// The assembled line is windowed to the column width around the
		// cursor so a long value cannot overflow into the next column (#640).
		if m.editing && i == m.varSel {
			prefix := " " + strings.Repeat("  ", n.depth) + marker + n.v.Name + " = "
			if m.editWatch {
				// A watch edit types the expression itself (#1914) — no
				// name/value split to prefix.
				prefix = " " + strings.Repeat("  ", n.depth) + marker
			}
			line := append([]rune(prefix), m.edit.Runes()...)
			ci := len([]rune(prefix)) + m.edit.Cur
			if ci == len(line) {
				line = append(line, ' ') // the cursor sits past the buffer end
			}
			if w < 1 {
				continue
			}
			if len(line) > w {
				start := clamp(ci-w+1, 0, len(line)-w)
				line = line[start : start+w]
				ci -= start
			}
			editStyle := lipgloss.NewStyle().Foreground(m.theme().Accent)
			cursor := lipgloss.NewStyle().Reverse(true).Render(string(line[ci]))
			out = append(out, editStyle.Render(string(line[:ci]))+cursor+editStyle.Render(string(line[ci+1:])))
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
			if m.focused && m.col == colVars && !m.running {
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
