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
	"ike/internal/terminal"
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
	colOutput
)

// maxOutputLines caps the retained debuggee output so a chatty program cannot
// grow the panel's memory without bound (#624).
const maxOutputLines = 5000

// outLine is one line of debuggee output, tagged with its stream.
type outLine struct {
	text   string
	stderr bool
}

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

	// finished marks a terminated session (#689): the panel stays open so the
	// Output column remains reviewable; frames show the exit status instead.
	finished bool
	exitCode int
	hasExit  bool

	// Debuggee output (#624): completed lines plus a pending partial (DAP
	// output events can split mid-line); outTop is the scroll offset. outHold
	// is set by a manual scroll away from the bottom (#637): while held,
	// AppendOutput stops re-pinning outTop; scrolling back to the bottom
	// releases it (auto-follow resumes).
	outLines   []outLine
	outPartial outLine
	outTop     int
	outHold    bool

	// term is the embedded debuggee terminal (#676): while set, its PTY view
	// replaces the DAP output rows in the Output column (see terminal.go).
	term *terminal.Model

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

// SetSize records the pane's interior size and refits the embedded terminal.
func (m *Model) SetSize(w, h int) {
	m.w, m.h = w, h
	m.sizeTerminal()
}

// SetFocused records focus.
func (m *Model) SetFocused(f bool) {
	m.focused = f
	m.syncTermFocus()
}

// SetPalette re-threads the theme palette.
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

// AppendOutput appends a debuggee output chunk (#624), splitting it into lines
// and carrying an incomplete trailing line as a pending partial (DAP output
// events can split mid-line). Completed lines are sanitized (ANSI/\r/\t, #637)
// — the partial stays raw so an escape split across chunks is stripped whole
// once its line completes. The view stays pinned to the newest output unless
// the user scrolled away (outHold).
func (m *Model) AppendOutput(stderr bool, text string) {
	if text == "" {
		return
	}
	// A stream switch mid-line flushes the pending partial as its own line.
	if m.outPartial.text != "" && m.outPartial.stderr != stderr {
		m.outLines = append(m.outLines, outLine{text: sanitizeLine(m.outPartial.text), stderr: m.outPartial.stderr})
		m.outPartial = outLine{}
	}
	parts := strings.Split(m.outPartial.text+text, "\n")
	for _, p := range parts[:len(parts)-1] {
		m.outLines = append(m.outLines, outLine{text: sanitizeLine(p), stderr: stderr})
	}
	m.outPartial = outLine{text: parts[len(parts)-1], stderr: stderr}
	if len(m.outLines) > maxOutputLines {
		m.outLines = m.outLines[len(m.outLines)-maxOutputLines:]
	}
	if !m.outHold {
		m.outTop = max(0, m.outputRowCount()-m.bodyHeight()) // follow the newest
	}
}

// outputRowCount is the number of visible output rows (completed lines plus a
// non-empty pending partial).
func (m Model) outputRowCount() int {
	n := len(m.outLines)
	if m.outPartial.text != "" {
		n++
	}
	return n
}

// outputRows returns the output as display lines including the pending
// partial, sanitized for display (its stored text stays raw, see AppendOutput).
func (m Model) outputRows() []outLine {
	if m.outPartial.text == "" {
		return m.outLines
	}
	p := outLine{text: sanitizeLine(m.outPartial.text), stderr: m.outPartial.stderr}
	return append(append([]outLine(nil), m.outLines...), p)
}

// scrollOutput shifts the output scroll offset by delta, clamped, and tracks
// auto-follow (#637): moving away from the bottom holds the view in place,
// returning to the bottom resumes following new output.
func (m *Model) scrollOutput(delta int) {
	maxTop := max(0, m.outputRowCount()-m.bodyHeight())
	m.outTop = clamp(m.outTop+delta, 0, maxTop)
	m.outHold = m.outTop < maxTop
}

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
	m.finished = false
	m.cancelEdit()
}

// SetRunning blanks the paused data while the debuggee runs.
func (m *Model) SetRunning() {
	m.running = true
	m.finished = false
	m.frames = nil
	m.roots = nil
	m.frameSel, m.varSel = 0, 0
	m.frameTop, m.varTop = 0, 0
	m.cancelEdit()
}

// SetFinished marks the session as terminated (#689): paused data is cleared
// but the output (text lines or the embedded terminal's scrollback) stays for
// review until the user closes the panel or a new session resets it.
func (m *Model) SetFinished(exitCode int, hasCode bool) {
	m.finished = true
	m.exitCode = exitCode
	m.hasExit = hasCode
	m.running = false
	m.frames = nil
	m.roots = nil
	m.frameSel, m.varSel = 0, 0
	m.frameTop, m.varTop = 0, 0
	m.cancelEdit()
}

// Finished reports whether the panel shows a terminated session.
func (m Model) Finished() bool { return m.finished }

// ResetSession clears everything a previous session left behind — finished
// marker, output lines, and the embedded terminal — for a fresh launch that
// reuses the still-open panel (#689).
func (m *Model) ResetSession() {
	m.finished = false
	m.hasExit = false
	m.exitCode = 0
	m.running = false
	m.frames = nil
	m.roots = nil
	m.frameSel, m.varSel = 0, 0
	m.frameTop, m.varTop = 0, 0
	m.outLines = nil
	m.outPartial = outLine{}
	m.outTop = 0
	m.outHold = false
	m.CloseTerminal()
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
	// The Output column with an embedded terminal has its own key routing
	// (#676): a running debuggee takes keys raw, an exited one restores the
	// panel's navigation (see terminal.go).
	if m.col == colOutput && m.term != nil {
		return m.outputTermKey(k)
	}
	switch k.String() {
	case "tab", "l", "right":
		if m.col < colOutput {
			m.col++
			m.syncTermFocus()
		}
	case "h", "left":
		if m.col > colFrames {
			m.col--
			m.syncTermFocus()
		}
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
	switch m.col {
	case colFrames:
		m.frameSel = clamp(m.frameSel+delta, 0, len(m.frames)-1)
		m.frameTop = scrollToShow(m.frameTop, m.frameSel, m.bodyHeight(), len(m.frames))
	case colOutput:
		m.scrollOutput(delta)
	default:
		m.varSel = clamp(m.varSel+delta, 0, len(m.flat())-1)
		m.varTop = scrollToShow(m.varTop, m.varSel, m.bodyHeight(), len(m.flat()))
	}
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

// View renders the three columns side by side: frames, variables, output. The
// columns render in every state (#637) — while the debuggee runs or before the
// first stop the frames column shows a placeholder, but the OUTPUT column keeps
// streaming: that is exactly when output arrives.
func (m Model) View() string {
	if m.w < 4 || m.h < 1 {
		return ""
	}
	fw, vw, ow := m.colWidths()
	frames := m.renderFrames(fw)
	vars := m.renderVars(vw)
	output := m.renderOutput(ow)
	sep := lipgloss.NewStyle().Foreground(m.theme().Border).Render("│")
	rows := make([]string, 0, m.h)
	for i := 0; i < m.h; i++ {
		rows = append(rows, pad(rowAt(frames, i), fw)+sep+pad(rowAt(vars, i), vw)+sep+rowAt(output, i))
	}
	return strings.Join(rows, "\n")
}

// colWidths splits the interior into frames | variables | output, reserving one
// cell for each of the two separators.
func (m Model) colWidths() (frames, vars, output int) {
	usable := m.w - 2 // two separators
	if usable < 3 {
		usable = 3
	}
	frames = usable * 2 / 5
	if frames < 12 {
		frames = min(12, usable/3)
	}
	rest := usable - frames
	vars = rest / 2
	output = rest - vars
	return frames, vars, output
}

func rowAt(rows []string, i int) string {
	if i < len(rows) {
		return rows[i]
	}
	return ""
}

// renderFrames renders the stack rows, selection highlighted. With no paused
// data (running, or no stop yet) a placeholder row stands in (#637) — the
// other columns render regardless.
func (m Model) renderFrames(w int) []string {
	title := lipgloss.NewStyle().Foreground(m.theme().Accent).Bold(true).Render(" FRAMES")
	out := []string{title}
	sel := lipgloss.NewStyle().Foreground(m.theme().Accent).Bold(true)
	dim := lipgloss.NewStyle().Foreground(m.theme().Foreground)
	if m.running {
		return append(out, dim.Render(truncate(" running…", w)))
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
		// The assembled line is windowed to the column width around the
		// cursor so a long value cannot overflow into the next column (#640).
		if m.editing && i == m.varSel {
			prefix := " " + strings.Repeat("  ", n.depth) + marker + n.v.Name + " = "
			line := append([]rune(prefix), m.editBuf...)
			ci := len([]rune(prefix)) + m.editCur
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
			if m.focused && m.col == colVars {
				style = style.Reverse(true)
			}
		}
		out = append(out, style.Render(label))
	}
	return out
}

// renderOutput renders the debuggee's captured stdout/stderr (#624); stderr
// lines take the error tone.
func (m Model) renderOutput(w int) []string {
	title := lipgloss.NewStyle().Foreground(m.theme().Accent).Bold(true).Render(" OUTPUT")
	out := []string{title}
	// An embedded debuggee terminal wins over the DAP output rows (#676): its
	// grid is already sized to the column, so its lines splice in verbatim.
	if m.term != nil {
		for _, ln := range strings.Split(m.term.View(), "\n") {
			if len(out) >= m.h {
				break
			}
			out = append(out, ln)
		}
		return out
	}
	dim := lipgloss.NewStyle().Foreground(m.theme().Foreground)
	errStyle := lipgloss.NewStyle().Foreground(m.theme().Error)
	rows := m.outputRows()
	// Clamp against the current size: output may have been appended before the
	// panel was sized (flush-on-open), leaving outTop pinned past the content.
	top := clamp(m.outTop, 0, max(0, len(rows)-m.bodyHeight()))
	for i := top; i < len(rows); i++ {
		if len(out) >= m.h {
			break
		}
		ln := rows[i]
		style := dim
		if ln.stderr {
			style = errStyle
		}
		out = append(out, style.Render(truncate(" "+ln.text, w)))
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
