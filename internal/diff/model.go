package diff

// model.go is the pane half of the diff viewer (#60): a value-type component
// mirroring the other pane models (preview, terminal), embedded in a
// pane.Instance or — via ui.ModelContent — in the floating shell. It renders
// the computed rows side by side (default) or unified, wraps long lines by
// display-cell budget like the editor viewport, and navigates hunks with n/N;
// enter asks the root model to jump the real editor to the hunk. The view is
// read-only; hunk-level staging is a later increment for #28.

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/editor/viewport"
	"ike/internal/theme"
)

// JumpMsg asks the root model to open the diff's right-hand (current) file
// with the cursor on Line (1-based) — dispatched when enter is pressed on a
// hunk. Path is empty when the right side is not backed by a file.
type JumpMsg struct {
	Path string
	Line int
}

// tabWidth is the display width a tab expands to inside the diff view. The
// diff compares raw text; tabs only widen at render time.
const tabWidth = 4

// Model is one live diff view comparing a left (old) and right (new) version.
// It is a value type with pointer-receiver mutators, like the other pane
// components.
type Model struct {
	key        string
	leftTitle  string
	rightTitle string
	leftPath   string // file backing the left column, "" when none; for persistence
	rightPath  string // jump target for enter; empty disables jumping
	pal        *theme.Palette

	w, h    int
	focused bool
	unified bool

	res       Result
	cur       int // current hunk index, -1 before the first n/N
	top       int // first visible visual row
	lines     []string
	rowStarts []int // visual row each Row starts on, for hunk navigation
}

// New returns a diff view keyed to its owning pane, comparing the two texts.
// leftTitle/rightTitle label the columns (file names, "HEAD", "snapshot", …);
// rightPath, when non-empty, is the file enter jumps the editor to.
func New(key, leftTitle, rightTitle, rightPath string, pal *theme.Palette) Model {
	return Model{key: key, leftTitle: leftTitle, rightTitle: rightTitle, rightPath: rightPath, pal: pal, cur: -1}
}

// NewFiles returns a diff view over two file paths, labelled by their base
// names; enter jumps to the right file.
func NewFiles(key, leftPath, rightPath string, pal *theme.Palette) Model {
	m := New(key, filepath.Base(leftPath), filepath.Base(rightPath), rightPath, pal)
	m.leftPath = leftPath
	return m
}

// Key returns the owning pane key.
func (m Model) Key() string { return m.key }

// Titles returns the column labels, for pane chrome and the status line.
func (m Model) Titles() (left, right string) { return m.leftTitle, m.rightTitle }

// LeftPath returns the file the left column is backed by ("" when none),
// for persistence.
func (m Model) LeftPath() string { return m.leftPath }

// RightPath returns the file the right column is backed by ("" when none),
// for persistence.
func (m Model) RightPath() string { return m.rightPath }

// Unified reports whether the view is in unified (single-column) layout.
func (m Model) Unified() bool { return m.unified }

// HunkCount returns how many hunks the diff holds.
func (m Model) HunkCount() int { return len(m.res.Hunks) }

// CurrentHunk returns the hunk index n/N last landed on, -1 before the first.
func (m Model) CurrentHunk() int { return m.cur }

// SetFocused marks the view focused; the focused view consumes its keys.
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-themes and re-renders the view.
func (m *Model) SetPalette(p *theme.Palette) {
	m.pal = p
	m.render()
}

// SetSize records the interior size and re-renders: lines wrap to the column
// budget, so a resize invalidates every rendered row.
func (m *Model) SetSize(w, h int) {
	if w == m.w && h == m.h {
		return
	}
	m.w, m.h = w, h
	m.render()
}

// SetContents diffs the two texts and renders the result. The scroll position
// resets; the current hunk clears.
func (m *Model) SetContents(left, right string) {
	m.res = Compute(left, right)
	m.cur = -1
	m.top = 0
	m.render()
}

// SetUnified switches between unified and side-by-side layout.
func (m *Model) SetUnified(u bool) {
	if m.unified == u {
		return
	}
	m.unified = u
	m.render()
	m.scrollToHunk(m.cur)
}

// Update handles the view's keys when focused.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		return m.handleKey(k)
	}
	return nil
}

// handleKey drives scrolling, layout toggle, and hunk navigation. The view is
// read-only, so vim motions map straight to view movement.
func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "up", "k":
		m.scrollTo(m.top - 1)
	case "down", "j":
		m.scrollTo(m.top + 1)
	case "pgup", "ctrl+u":
		m.scrollTo(m.top - m.pageStep())
	case "pgdown", "ctrl+d":
		m.scrollTo(m.top + m.pageStep())
	case "home", "g":
		m.scrollTo(0)
	case "end", "G":
		m.scrollTo(len(m.lines))
	case "n":
		m.stepHunk(1)
	case "N":
		m.stepHunk(-1)
	case "u":
		m.SetUnified(!m.unified)
	case "enter":
		return m.jump()
	}
	return nil
}

// stepHunk moves the current hunk by delta, clamped, and scrolls to it.
func (m *Model) stepHunk(delta int) {
	if len(m.res.Hunks) == 0 {
		return
	}
	next := m.cur + delta
	if m.cur < 0 && delta < 0 {
		next = len(m.res.Hunks) - 1 // N before any n: start from the last hunk
	}
	m.cur = clamp(next, 0, len(m.res.Hunks)-1)
	m.scrollToHunk(m.cur)
}

// scrollToHunk scrolls hunk i's first visual row a third down the viewport.
func (m *Model) scrollToHunk(i int) {
	if i < 0 || i >= len(m.res.Hunks) || len(m.rowStarts) == 0 {
		return
	}
	m.scrollTo(m.rowStarts[m.res.Hunks[i].Start] - m.h/3)
}

// jump returns the command dispatching a JumpMsg for the current hunk (the
// first hunk when none was navigated to yet): the editor opens the right-hand
// file on the hunk's first line.
func (m *Model) jump() tea.Cmd {
	if m.rightPath == "" || len(m.res.Hunks) == 0 {
		return nil
	}
	i := m.cur
	if i < 0 {
		i = 0
	}
	h := m.res.Hunks[i]
	line := 0
	for _, row := range m.res.Rows[h.Start:h.End] {
		if row.RightNo > 0 {
			line = row.RightNo
			break
		}
	}
	if line == 0 {
		// A pure-removal hunk has no right-side line; land on the neighbour
		// before the removal.
		if h.Start > 0 {
			line = m.res.Rows[h.Start-1].RightNo
		}
		if line == 0 {
			line = 1
		}
	}
	path := m.rightPath
	return func() tea.Msg { return JumpMsg{Path: path, Line: line} }
}

// pageStep is one page-scroll increment: just under a viewport of lines.
func (m Model) pageStep() int { return max(1, m.h-1) }

// ScrollBy scrolls the view by delta visual rows (mouse wheel).
func (m *Model) ScrollBy(delta int) { m.scrollTo(m.top + delta) }

// scrollTo clamps and applies a new top row.
func (m *Model) scrollTo(top int) {
	m.top = clamp(top, 0, max(0, len(m.lines)-m.h))
}

// View renders the visible window, hard-clamped to the pane interior.
func (m Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	var b strings.Builder
	for row := 0; row < m.h; row++ {
		if row > 0 {
			b.WriteByte('\n')
		}
		if i := m.top + row; i >= 0 && i < len(m.lines) {
			b.WriteString(ansi.Truncate(m.lines[i], m.w, "…"))
		}
	}
	return b.String()
}

// render rebuilds the styled visual lines at the current width, layout, and
// theme, and records each row's first visual line for hunk navigation.
func (m *Model) render() {
	m.lines = nil
	m.rowStarts = make([]int, len(m.res.Rows))
	if m.w <= 0 {
		return
	}
	if m.unified {
		m.renderUnified()
	} else {
		m.renderSideBySide()
	}
	m.scrollTo(m.top)
}

// styles bundles the resolved lipgloss styles one render pass reuses.
type styles struct {
	gutter  lipgloss.Style
	same    lipgloss.Style
	added   lipgloss.Style
	removed lipgloss.Style
	span    lipgloss.Style
}

func (m Model) styles() styles {
	pal := m.pal
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	return styles{
		gutter:  lipgloss.NewStyle().Faint(true),
		same:    lipgloss.NewStyle(),
		added:   lipgloss.NewStyle().Background(pal.DiffAdded),
		removed: lipgloss.NewStyle().Background(pal.DiffRemoved),
		span:    lipgloss.NewStyle().Background(pal.DiffChanged),
	}
}

// base returns the whole-line background style for one side of a row.
func (st styles) base(kind Kind, right bool) lipgloss.Style {
	switch kind {
	case RowAdded:
		return st.added
	case RowRemoved:
		return st.removed
	case RowChanged:
		if right {
			return st.added
		}
		return st.removed
	}
	return st.same
}

// renderSideBySide paints two aligned columns with a dual gutter:
// "NNN old │ NNN new". Both sides wrap to their column budget; the shorter
// side pads with gap rows so the pair stays aligned.
func (m *Model) renderSideBySide() {
	st := m.styles()
	lw, rw := m.gutterWidths()
	avail := m.w - (lw + 1) - (rw + 1) - 3 // two gutters + " │ "
	colL := max(1, avail/2)
	colR := max(1, avail-avail/2)
	sep := st.gutter.Render(" │ ")
	for ri, row := range m.res.Rows {
		m.rowStarts[ri] = len(m.lines)
		leftRunes := expand(row.Left)
		rightRunes := expand(row.Right)
		segsL := viewport.WrapSegments(leftRunes, colL, 1)
		segsR := viewport.WrapSegments(rightRunes, colR, 1)
		height := max(len(segsL), len(segsR))
		if row.Kind == RowAdded {
			segsL = nil // gap side: no content rows, not one empty row
		}
		if row.Kind == RowRemoved {
			segsR = nil
		}
		for v := 0; v < height; v++ {
			var b strings.Builder
			b.WriteString(m.gutterCell(row.LeftNo, lw, v, row.Kind != RowAdded, st))
			b.WriteString(renderSegment(leftRunes, segsL, v, colL, st.base(row.Kind, false), st.span, expandSpans(row.Left, row.LeftSpans)))
			b.WriteString(sep)
			b.WriteString(m.gutterCell(row.RightNo, rw, v, row.Kind != RowRemoved, st))
			b.WriteString(renderSegment(rightRunes, segsR, v, colR, st.base(row.Kind, true), st.span, expandSpans(row.Right, row.RightSpans)))
			m.lines = append(m.lines, b.String())
		}
	}
}

// renderUnified paints a single column with a dual line-number gutter; a
// changed pair renders as its removed line followed by its added line.
func (m *Model) renderUnified() {
	st := m.styles()
	lw, rw := m.gutterWidths()
	col := max(1, m.w-(lw+1)-(rw+1))
	emit := func(text string, leftNo, rightNo int, base lipgloss.Style, spans []Span) {
		runes := expand(text)
		segs := viewport.WrapSegments(runes, col, 1)
		espans := expandSpans(text, spans)
		for v := range segs {
			var b strings.Builder
			b.WriteString(m.gutterCell(leftNo, lw, v, true, st))
			b.WriteString(m.gutterCell(rightNo, rw, v, true, st))
			b.WriteString(renderSegment(runes, segs, v, col, base, st.span, espans))
			m.lines = append(m.lines, b.String())
		}
	}
	for ri, row := range m.res.Rows {
		m.rowStarts[ri] = len(m.lines)
		switch row.Kind {
		case RowSame:
			emit(row.Left, row.LeftNo, row.RightNo, st.same, nil)
		case RowChanged:
			emit(row.Left, row.LeftNo, 0, st.removed, row.LeftSpans)
			emit(row.Right, 0, row.RightNo, st.added, row.RightSpans)
		case RowRemoved:
			emit(row.Left, row.LeftNo, 0, st.removed, row.LeftSpans)
		case RowAdded:
			emit(row.Right, 0, row.RightNo, st.added, row.RightSpans)
		}
	}
}

// gutterWidths returns the digit widths of the two line-number columns.
func (m Model) gutterWidths() (lw, rw int) {
	maxL, maxR := 1, 1
	for _, r := range m.res.Rows {
		if r.LeftNo > maxL {
			maxL = r.LeftNo
		}
		if r.RightNo > maxR {
			maxR = r.RightNo
		}
	}
	return max(3, digits(maxL)), max(3, digits(maxR))
}

// gutterCell renders one line-number cell: the number on the row's first
// visual line, a wrap marker on continuations, blank on the gap side.
func (m Model) gutterCell(no, width, visual int, present bool, st styles) string {
	switch {
	case !present || no == 0:
		return strings.Repeat(" ", width+1)
	case visual > 0:
		if width >= 2 {
			return st.gutter.Render(strings.Repeat(" ", width-1) + "↪ ")
		}
		return strings.Repeat(" ", width+1)
	}
	return st.gutter.Render(fmt.Sprintf("%*d ", width, no))
}

// renderSegment paints visual row v of a wrapped line, padded to width cells:
// base-styled text with span ranges emphasized. segs == nil renders a gap row
// (blank, unstyled).
func renderSegment(runes []rune, segs []int, v, width int, base, span lipgloss.Style, spans []Span) string {
	if v >= len(segs) {
		return strings.Repeat(" ", width)
	}
	start := segs[v]
	end := viewport.SegmentEnd(segs, v, len(runes))
	var b strings.Builder
	col := start
	for col < end {
		emph := inSpan(spans, col)
		e := col + 1
		for e < end && inSpan(spans, e) == emph {
			e++
		}
		st := base
		if emph {
			st = span
		}
		b.WriteString(st.Render(string(runes[col:e])))
		col = e
	}
	if pad := width - (end - start); pad > 0 {
		b.WriteString(base.Render(strings.Repeat(" ", pad)))
	}
	return b.String()
}

// inSpan reports whether rune column col lies inside any span.
func inSpan(spans []Span, col int) bool {
	for _, s := range spans {
		if col >= s.Start && col < s.End {
			return true
		}
	}
	return false
}

// expand widens tabs to spaces for display; the diff itself runs on raw text.
func expand(line string) []rune {
	if !strings.ContainsRune(line, '\t') {
		return []rune(line)
	}
	var out []rune
	for _, r := range line {
		if r == '\t' {
			for i := 0; i < tabWidth; i++ {
				out = append(out, ' ')
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

// expandSpans maps spans from raw rune columns to tab-expanded columns.
func expandSpans(line string, spans []Span) []Span {
	if len(spans) == 0 || !strings.ContainsRune(line, '\t') {
		return spans
	}
	// offset[i] = expanded column of raw column i.
	runes := []rune(line)
	offset := make([]int, len(runes)+1)
	col := 0
	for i, r := range runes {
		offset[i] = col
		if r == '\t' {
			col += tabWidth
		} else {
			col++
		}
	}
	offset[len(runes)] = col
	out := make([]Span, len(spans))
	for i, s := range spans {
		out[i] = Span{Start: offset[clamp(s.Start, 0, len(runes))], End: offset[clamp(s.End, 0, len(runes))]}
	}
	return out
}

// digits returns the decimal width of n (n >= 1).
func digits(n int) int {
	d := 1
	for n >= 10 {
		n /= 10
		d++
	}
	return d
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
