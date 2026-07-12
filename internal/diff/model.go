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

	// Collapsed context (0340, #494): unchanged runs longer than the context
	// budget fold into separator rows. gaps records the foldable runs and
	// their per-gap expansion; sepLines maps each rendered separator's visual
	// line to its gap for the expand key.
	ctx       int // context lines kept around changes; <0 disables collapsing
	collapsed bool
	gaps      []gap
	sepLines  map[int]int // visual line → gap index

	// Editable current side (0340, #496): worktree-backed diffs may swap
	// their right column for a live editor; the pane layer owns the editor,
	// this model re-diffs against the retained left text and renders the
	// aligned left column. rightRow maps RightNo → row index for alignment.
	editable   bool
	leftText   string
	rightRow   map[int]int
	editModeOn bool

	// leftRev/rightRev name the revision backing each side ("" = file),
	// persisted so a restart can re-read the blobs (#508).
	leftRev  string
	rightRev string
}

// EditRequestMsg asks the root model to start edit mode on the diff pane Key
// (the 'e' key, #496); the root validates editability and builds the editor.
type EditRequestMsg struct {
	Key  string
	Path string
}

// gap is one foldable run of RowSame rows: [start, end) row indices of the
// hidden middle (context rows around it stay visible).
type gap struct {
	start, end int
	expanded   bool
}

// defaultContext is the context-line budget when no config overrides it.
const defaultContext = 3

// minHidden is the smallest run worth a separator: folding one or two lines
// reads worse than showing them.
const minHidden = 3

// New returns a diff view keyed to its owning pane, comparing the two texts.
// leftTitle/rightTitle label the columns (file names, "HEAD", "snapshot", …);
// rightPath, when non-empty, is the file enter jumps the editor to.
func New(key, leftTitle, rightTitle, rightPath string, pal *theme.Palette) Model {
	return Model{key: key, leftTitle: leftTitle, rightTitle: rightTitle, rightPath: rightPath,
		pal: pal, cur: -1, ctx: defaultContext, collapsed: true}
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
// resets; the current hunk and every gap expansion clear.
func (m *Model) SetContents(left, right string) {
	m.leftText = left
	m.res = Compute(left, right)
	m.cur = -1
	m.top = 0
	m.gaps = computeGaps(m.res, m.ctx)
	m.buildRightRow()
	m.render()
}

// SetRevs records which revision backs each side ("" = a working-tree file),
// for persistence (#508): a restored pane re-reads revision sides via git.
func (m *Model) SetRevs(left, right string) { m.leftRev, m.rightRev = left, right }

// Revs returns the per-side backing revisions ("" = file-backed).
func (m Model) Revs() (left, right string) { return m.leftRev, m.rightRev }

// SetEditable marks the right side as backed by the working tree (#496);
// revision-only diffs stay read-only.
func (m *Model) SetEditable(e bool) { m.editable = e }

// Editable reports whether edit mode may start on this diff.
func (m Model) Editable() bool { return m.editable && m.rightPath != "" }

// SetEditMode flips the pane-owned edit mode flag; while on, View is unused
// (the pane composes RenderEditSplit) and the model only re-diffs.
func (m *Model) SetEditMode(on bool) { m.editModeOn = on }

// EditMode reports whether the pane drives an embedded editor.
func (m Model) EditMode() bool { return m.editModeOn }

// Rediff recomputes the rows for new right-side content against the retained
// left text (per keystroke in edit mode); scroll and hunk state stay.
func (m *Model) Rediff(right string) {
	m.res = Compute(m.leftText, right)
	if m.cur >= len(m.res.Hunks) {
		m.cur = len(m.res.Hunks) - 1
	}
	m.gaps = computeGaps(m.res, m.ctx)
	m.buildRightRow()
	m.render()
}

// buildRightRow indexes rows by their right line number for edit alignment.
func (m *Model) buildRightRow() {
	m.rightRow = make(map[int]int, len(m.res.Rows))
	for i, r := range m.res.Rows {
		if r.RightNo > 0 {
			m.rightRow[r.RightNo] = i
		}
	}
}

// SetContext sets the context-line budget (config diff.context); n < 0
// disables collapsing entirely.
func (m *Model) SetContext(n int) {
	m.ctx = n
	m.gaps = computeGaps(m.res, m.ctx)
	m.render()
}

// Collapsed reports whether the view folds unchanged runs.
func (m Model) Collapsed() bool { return m.collapsed && m.ctx >= 0 }

// computeGaps finds the foldable RowSame runs: each keeps ctx context rows
// toward any adjacent change (none toward the file edges) and folds the rest
// when at least minHidden rows would hide.
func computeGaps(res Result, ctx int) []gap {
	if ctx < 0 {
		return nil
	}
	var gaps []gap
	i := 0
	for i < len(res.Rows) {
		if res.Rows[i].Kind != RowSame {
			i++
			continue
		}
		j := i
		for j < len(res.Rows) && res.Rows[j].Kind == RowSame {
			j++
		}
		lead, trail := ctx, ctx
		if i == 0 {
			lead = 0 // run touches the file start: no change above to anchor context
		}
		if j == len(res.Rows) {
			trail = 0 // run touches the file end
		}
		if hidden := (j - i) - lead - trail; hidden >= minHidden {
			gaps = append(gaps, gap{start: i + lead, end: j - trail})
		}
		i = j
	}
	return gaps
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
	case "c":
		// Toggle collapsed context (#494); the current hunk stays in view.
		m.collapsed = !m.collapsed
		m.render()
		m.scrollToHunk(m.cur)
	case "o":
		m.expandNearestGap()
	case "e":
		// Edit mode (#496): the root model validates and mounts the editor.
		key, path := m.key, m.rightPath
		return func() tea.Msg { return EditRequestMsg{Key: key, Path: path} }
	case "enter":
		return m.jump()
	}
	return nil
}

// EditSplitWidths returns the column budget of the edit-mode split: the left
// (read-only) column including its gutter, and the right editor width.
func (m Model) EditSplitWidths() (left, right int) {
	lw, _ := m.gutterWidths()
	avail := m.w - (lw + 1) - 3 // left gutter + " │ "
	left = max(1, avail/2)
	right = max(1, avail-avail/2)
	return left, right
}

// RenderEditSplit composes the edit-mode frame: for each of the editor's
// visible buffer lines (starting at topLine, 0-based) the aligned left-side
// cell renders beside the editor's own row. Removed-only left lines have no
// right counterpart and stay hidden while editing — the re-diff restores
// them the moment the deletion is undone.
func (m *Model) RenderEditSplit(edLines []string, topLine, height int) string {
	st := m.styles()
	lw, _ := m.gutterWidths()
	colL, colR := m.EditSplitWidths()
	sep := st.gutter.Render(" │ ")
	var b strings.Builder
	for v := 0; v < height; v++ {
		if v > 0 {
			b.WriteByte('\n')
		}
		bufLine := topLine + v + 1 // 1-based right line number
		left := strings.Repeat(" ", lw+1+colL)
		if ri, ok := m.rightRow[bufLine]; ok {
			row := m.res.Rows[ri]
			runes := expand(row.Left)
			segs := viewport.WrapSegments(runes, colL, 1)
			if row.Kind == RowAdded {
				segs = nil // no left counterpart: gap cell
			}
			left = m.gutterCell(row.LeftNo, lw, 0, row.Kind != RowAdded, st) +
				renderSegment(runes, segs, 0, colL, st.base(row.Kind, false), st.span, expandSpans(row.Left, row.LeftSpans))
		}
		b.WriteString(left)
		b.WriteString(sep)
		if v < len(edLines) {
			b.WriteString(ansi.Truncate(edLines[v], colR, "…"))
		}
	}
	return b.String()
}

// expandNearestGap expands the separator closest to the viewport center; a
// view without visible separators is a no-op.
func (m *Model) expandNearestGap() {
	if len(m.sepLines) == 0 {
		return
	}
	center := m.top + m.h/2
	best, bestDist := -1, 1<<30
	for line, gi := range m.sepLines {
		d := line - center
		if d < 0 {
			d = -d
		}
		if d < bestDist {
			best, bestDist = gi, d
		}
	}
	if best < 0 {
		return
	}
	m.gaps[best].expanded = true
	m.render()
}

// StepHunk moves the current hunk by delta — the diff.nextChange /
// diff.prevChange commands (F7 / shift+F7, 0340 #495) drive it from outside
// the key handler.
func (m *Model) StepHunk(delta int) { m.stepHunk(delta) }

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

// displayItem is one render unit: a row index, or a separator for gap gi.
type displayItem struct {
	row int // index into res.Rows; -1 for a separator
	gi  int // gap index when row == -1
}

// displayItems folds the collapsed gaps into separators. Rows hidden behind
// a separator keep a rowStart pointing at it, so hunk navigation and jumps
// stay well-defined (hunks themselves are never hidden).
func (m *Model) displayItems() []displayItem {
	if !m.Collapsed() || len(m.gaps) == 0 {
		out := make([]displayItem, len(m.res.Rows))
		for i := range out {
			out[i] = displayItem{row: i}
		}
		return out
	}
	var out []displayItem
	gi := 0
	for i := 0; i < len(m.res.Rows); {
		if gi < len(m.gaps) && m.gaps[gi].start == i && !m.gaps[gi].expanded {
			out = append(out, displayItem{row: -1, gi: gi})
			i = m.gaps[gi].end
			gi++
			continue
		}
		if gi < len(m.gaps) && i >= m.gaps[gi].end {
			gi++
			continue
		}
		out = append(out, displayItem{row: i})
		i++
	}
	return out
}

// render rebuilds the styled visual lines at the current width, layout, and
// theme, and records each row's first visual line for hunk navigation.
func (m *Model) render() {
	m.lines = nil
	m.rowStarts = make([]int, len(m.res.Rows))
	m.sepLines = map[int]int{}
	if m.w <= 0 {
		return
	}
	items := m.displayItems()
	if m.unified {
		m.renderUnified(items)
	} else {
		m.renderSideBySide(items)
	}
	m.scrollTo(m.top)
}

// emitSeparator renders one collapsed-gap row and stamps the hidden rows'
// rowStarts onto it.
func (m *Model) emitSeparator(gi int, st styles) {
	g := m.gaps[gi]
	line := len(m.lines)
	m.sepLines[line] = gi
	for r := g.start; r < g.end; r++ {
		m.rowStarts[r] = line
	}
	label := fmt.Sprintf("··· %d unchanged lines (o expands, c shows all) ···", g.end-g.start)
	if pad := (m.w - len([]rune(label))) / 2; pad > 0 {
		label = strings.Repeat(" ", pad) + label
	}
	m.lines = append(m.lines, st.gutter.Render(label))
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
func (m *Model) renderSideBySide(items []displayItem) {
	st := m.styles()
	lw, rw := m.gutterWidths()
	avail := m.w - (lw + 1) - (rw + 1) - 3 // two gutters + " │ "
	colL := max(1, avail/2)
	colR := max(1, avail-avail/2)
	sep := st.gutter.Render(" │ ")
	for _, it := range items {
		if it.row < 0 {
			m.emitSeparator(it.gi, st)
			continue
		}
		ri := it.row
		row := m.res.Rows[ri]
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
func (m *Model) renderUnified(items []displayItem) {
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
	for _, it := range items {
		if it.row < 0 {
			m.emitSeparator(it.gi, st)
			continue
		}
		ri := it.row
		row := m.res.Rows[ri]
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
