package diff

// model.go is the pane half of the diff viewer (#60): a value-type component
// mirroring the other pane models (preview, terminal), embedded in a
// pane.Instance or — via ui.ModelContent — in the floating shell. It renders
// the computed rows side by side (default) or unified, clips long lines at the
// column edge and scrolls them horizontally (#1700), and navigates hunks with
// n/N; enter asks the root model to jump the real editor to the hunk. The view
// is read-only; hunk-level staging is a later increment for #28.

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/highlight"
	"ike/internal/textsel"
	"ike/internal/theme"
	"ike/internal/ui"
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

	// Ignore whitespace (#2170): a viewer-level toggle ('w', persisted as
	// diff.ignore_whitespace) feeding the engine's Options — lines differing
	// only in whitespace pair up as unchanged and intra-line refinement
	// reports only non-whitespace ranges, so a reformat-heavy diff shows the
	// changes that carry meaning.
	ignoreWS bool

	res       Result
	cur       int // current hunk index, -1 before the first n/N
	top       int // first visible visual row
	lines     []string
	rowStarts []int // visual row each Row starts on, for hunk navigation

	// Horizontal scrolling (#1700): the diff never soft-wraps — every row is
	// exactly one visual line, clipped at its column edge. hoff is the shared
	// first visible display column: both sides render from it, so the columns
	// move in lockstep and row alignment survives any line length. hmax is the
	// widest displayed line and hcol the visible column budget, both recorded
	// by the render pass to clamp hoff.
	hoff int
	hmax int
	hcol int
	// hMarks draws the horizontal-scroll edge marks (#2377, ui.h_scroll_marks)
	// on every rendered segment; see hscroll.go.
	hMarks bool

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

	// Syntax highlighting (#1699): each side parses independently with the
	// compared file's language — the right side is (or mirrors) a real
	// buffer, the left side is a virtual document (HEAD blob, snapshot)
	// sharing the same path-resolved language. hl resolves capture names to
	// foreground colours, composed under the diff-state backgrounds at
	// render time; it is built lazily so a zero-value Model stays safe.
	hl        *highlight.Theme
	leftIx    highlight.Index
	rightIx   highlight.Index
	rightText string

	// Mouse text selection (#2070): the shared click-streak engine over the
	// rendered visual lines. vrows maps each visual line back onto its row
	// and side; selRight pins a side-by-side selection to one column.
	sel      textsel.Selection
	selRight bool
	vrows    []vrow

	// In-pane search (#2409): "/" and the shared find chord open a prompt on
	// the pane's last row and n/N walk the matching rows. It lives behind a
	// pointer so the value-receiver View copies share it, like the explorer's
	// speed search; nil means no search is open and n/N step hunks.
	search *ui.LineSearch
}

// IgnoreWhitespaceMsg reports that the diff pane Key flipped its
// ignore-whitespace mode (#2170), so the root model can persist the new state
// as the diff.ignore_whitespace preference. The pane has already applied it.
type IgnoreWhitespaceMsg struct {
	Key string
	On  bool
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
		pal: pal, cur: -1, ctx: defaultContext, collapsed: true, hMarks: true}
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

// IgnoreWhitespace reports whether whitespace-only changes are ignored.
func (m Model) IgnoreWhitespace() bool { return m.ignoreWS }

// SetIgnoreWhitespace switches whitespace-insensitive comparison on or off
// and re-diffs the retained texts in place: the scroll position and the
// current hunk survive as far as the new hunk list allows.
func (m *Model) SetIgnoreWhitespace(on bool) {
	if m.ignoreWS == on {
		return
	}
	m.ignoreWS = on
	m.recompute()
}

// diffOpts is the engine option set the view currently compares under.
func (m Model) diffOpts() Options { return Options{IgnoreWhitespace: m.ignoreWS} }

// HunkCount returns how many hunks the diff holds.
func (m Model) HunkCount() int { return len(m.res.Hunks) }

// CurrentHunk returns the hunk index n/N last landed on, -1 before the first.
func (m Model) CurrentHunk() int { return m.cur }

// SetFocused marks the view focused; the focused view consumes its keys.
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-themes and re-renders the view.
func (m *Model) SetPalette(p *theme.Palette) {
	m.pal = p
	m.hl = nil // capture colours follow the palette; rebuild lazily
	m.render()
}

// hlTheme returns the capture→style theme, built from the palette's capture
// table on first use (the HTTP pane's shape — palette captures, no config
// overlay).
func (m *Model) hlTheme() *highlight.Theme {
	if m.hl == nil {
		var captures map[string]string
		if m.pal != nil {
			captures = m.pal.Captures
		}
		t := highlight.NewTheme(captures, nil)
		m.hl = &t
	}
	return m.hl
}

// hlPath returns the path the syntax language resolves from: the right
// (current) side when file-backed, else the left. "" disables highlighting.
func (m Model) hlPath() string {
	if m.rightPath != "" {
		return m.rightPath
	}
	return m.leftPath
}

// rehighlight re-parses the requested sides (#1699). The two sides parse
// independently — the left is a virtual document (HEAD blob, snapshot) with
// no buffer behind it — but share the language resolved from the file path.
// No path, no language support, or a CGo-free build leaves the indexes empty
// and the rows render with diff colouring only.
func (m *Model) rehighlight(left, right bool) {
	path := m.hlPath()
	if path == "" || !highlight.Supported(path) {
		m.leftIx, m.rightIx = highlight.Index{}, highlight.Index{}
		return
	}
	if left {
		m.leftIx = highlight.NewIndex(highlight.Highlight(path, splitLines(m.leftText)))
	}
	if right {
		m.rightIx = highlight.NewIndex(highlight.Highlight(path, splitLines(m.rightText)))
	}
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
	m.rightText = right
	m.res = ComputeWith(left, right, m.diffOpts())
	m.cur = -1
	m.top = 0
	m.hoff = 0
	m.clearSelection()
	m.gaps = computeGaps(m.res, m.ctx)
	m.buildRightRow()
	m.rehighlight(true, true)
	m.render()
}

// Retarget points the pane at a different comparison (#513): titles, paths,
// per-side revisions, and editability swap; layout, context, and collapse
// preferences stay. The caller feeds the new texts via SetContents and must
// dismount any edit-mode editor first.
func (m *Model) Retarget(leftTitle, rightTitle, leftPath, rightPath, leftRev, rightRev string, editable bool) {
	m.leftTitle, m.rightTitle = leftTitle, rightTitle
	m.leftPath, m.rightPath = leftPath, rightPath
	m.leftRev, m.rightRev = leftRev, rightRev
	m.editable = editable
	m.editModeOn = false
	// The path (and thus the language) may change; the following SetContents
	// re-parses both sides against the new one.
	m.leftIx, m.rightIx = highlight.Index{}, highlight.Index{}
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
// (the pane composes RenderEditSplit) and the model only re-diffs. Leaving
// edit mode re-parses the right side once from the final buffer state — the
// per-keystroke Rediffs skipped it while the embedded editor (with its own
// live highlighting) rendered that column.
func (m *Model) SetEditMode(on bool) {
	if m.editModeOn == on {
		return
	}
	m.editModeOn = on
	if on {
		// The embedded editor owns the right column's horizontal view; the
		// read-only left column starts at column 0 beside it (#1700). It also
		// brings its own selection (#2070), so the pane's drops.
		m.hoff = 0
		m.clearSelection()
	}
	if !on {
		m.rehighlight(false, true)
		m.render()
	}
}

// EditMode reports whether the pane drives an embedded editor.
func (m Model) EditMode() bool { return m.editModeOn }

// Rediff recomputes the rows for new right-side content against the retained
// left text (per keystroke in edit mode); scroll and hunk state stay.
func (m *Model) Rediff(right string) {
	m.rightText = right
	m.rediff()
	if !m.editModeOn {
		// In edit mode the right column is the embedded editor's own render;
		// re-parsing it here per keystroke would be wasted work (#1699).
		m.rehighlight(false, true)
	}
	m.render()
}

// rediff re-runs the comparison over the retained texts and rebuilds the
// derived state around it, without rendering: the caller decides whether the
// sides need re-parsing and where the view should land afterwards.
func (m *Model) rediff() {
	m.res = ComputeWith(m.leftText, m.rightText, m.diffOpts())
	m.clearSelection()
	if m.cur >= len(m.res.Hunks) {
		m.cur = len(m.res.Hunks) - 1
	}
	m.gaps = computeGaps(m.res, m.ctx)
	m.buildRightRow()
}

// recompute re-diffs under changed options (#2170) and re-renders, keeping the
// view on the hunk it was on — clamped to the new hunk list, since ignoring
// whitespace usually makes hunks disappear. The texts themselves did not
// change, so neither side is re-parsed.
func (m *Model) recompute() {
	m.rediff()
	m.render()
	m.scrollToHunk(m.cur)
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
	m.clearSelection() // the visual-line map the selection lives in changed
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
	m.clearSelection() // positions are layout-specific
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
	// The open search prompt owns the keyboard (#2409): every key is query
	// text until enter applies it or esc abandons the search.
	if m.search != nil && m.search.Open {
		return m.searchKey(msg)
	}
	switch msg.String() {
	case "/", "ctrl+f", "cmd+f", "super+f":
		// The shared search key and the find chord open the same prompt.
		// ctrl+f is deliberately unbound in the keymap table (#2409) so
		// vim's page-forward survives in the editor; the panes that have a
		// search answer the chord themselves.
		m.openSearch()
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
	case "left", "h":
		m.scrollX(m.hoff - 1)
	case "right", "l":
		m.scrollX(m.hoff + 1)
	case "shift+left":
		m.scrollX(m.hoff - m.hStep())
	case "shift+right":
		m.scrollX(m.hoff + m.hStep())
	case "0":
		m.scrollX(0)
	case "$":
		m.scrollX(m.maxHOff())
	case "n":
		// With a search applied n/N walk its matches, vim-style; without one
		// they keep their hunk meaning — a diff is navigated by change.
		if m.search != nil {
			m.stepMatch(1)
			break
		}
		m.stepHunk(1)
	case "N":
		if m.search != nil {
			m.stepMatch(-1)
			break
		}
		m.stepHunk(-1)
	case "u":
		m.SetUnified(!m.unified)
	case "w":
		// Ignore whitespace (#2170): flips the live diff and asks the root
		// model to persist the preference.
		m.SetIgnoreWhitespace(!m.ignoreWS)
		key, on := m.key, m.ignoreWS
		return func() tea.Msg { return IgnoreWhitespaceMsg{Key: key, On: on} }
	case "c":
		// Toggle collapsed context (#494); the current hunk stays in view.
		m.collapsed = !m.collapsed
		m.clearSelection()
		m.render()
		m.scrollToHunk(m.cur)
	case "o":
		m.expandNearestGap()
	case "y", "ctrl+c", "cmd+c", "super+c":
		// Copy (#2070): the selection, else the current hunk as a patch.
		return m.copyKey()
	case "esc":
		// esc closes the search first, then clears a selection: one escape
		// per state, never both at once.
		if m.search != nil {
			m.closeSearch()
			break
		}
		m.ClearSelection()
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
	hl := m.hlTheme()
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
			gap := row.Kind == RowAdded
			left = m.gutterCell(row.LeftNo, lw, row.Kind != RowAdded, st) +
				m.stampHScroll(renderSegment(runes, gap, m.hoff, colL,
					st.base(row.Kind, false), st.emph(row.Kind, false), expandSpans(row.Left, row.LeftSpans),
					sideCaps(m.leftIx, row.LeftNo, row.Left), hl, 0, 0, st.sel), runes, gap, colL)
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
	m.clearSelection() // the visual-line map shifts under the selection
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

// ScrollXBy shifts the horizontal offset by delta display columns — the
// horizontal wheel and shift+wheel (#1700). Both sides move together.
func (m *Model) ScrollXBy(delta int) { m.scrollX(m.hoff + delta) }

// HOffset returns the first visible display column, shared by both sides.
func (m Model) HOffset() int { return m.hoff }

// hStep is one horizontal page-scroll increment: half a column of cells.
func (m Model) hStep() int { return max(1, m.hcol/2) }

// maxHOff is the largest useful horizontal offset: the widest displayed line
// minus the visible column budget.
func (m Model) maxHOff() int { return max(0, m.hmax-m.hcol) }

// scrollX clamps and applies a new horizontal offset, re-rendering the rows
// (each line is painted from the offset, so the styled text changes).
func (m *Model) scrollX(off int) {
	off = clamp(off, 0, m.maxHOff())
	if off == m.hoff {
		return
	}
	m.hoff = off
	m.render()
}

// scrollTo clamps and applies a new top row.
func (m *Model) scrollTo(top int) {
	m.top = clamp(top, 0, max(0, len(m.lines)-m.viewHeight()))
}

// viewHeight is the room the diff rows get: the whole pane, minus the search
// prompt row while a search is open (#2409).
func (m Model) viewHeight() int {
	if m.search == nil {
		return m.h
	}
	if m.h <= 1 {
		return m.h
	}
	return m.h - 1
}

// View renders the visible window, hard-clamped to the pane interior.
func (m Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	var b strings.Builder
	body := m.viewHeight()
	for row := 0; row < body; row++ {
		if row > 0 {
			b.WriteByte('\n')
		}
		if i := m.top + row; i >= 0 && i < len(m.lines) {
			b.WriteString(ansi.Truncate(m.lines[i], m.w, "…"))
		}
	}
	if body < m.h {
		b.WriteByte('\n')
		b.WriteString(ansi.Truncate(m.searchLine(), m.w, "…"))
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
	m.buildVRows(items)
	m.measure(items)
	if m.unified {
		m.renderUnified(items)
	} else {
		m.renderSideBySide(items)
	}
	m.scrollTo(m.top)
}

// measure records the widest displayed line (hmax) and the visible column
// budget (hcol) for the current layout, then re-clamps the horizontal offset:
// a resize, a layout toggle, or new content may leave hoff past the end.
func (m *Model) measure(items []displayItem) {
	if m.unified {
		lw, rw := m.gutterWidths()
		m.hcol = max(1, m.w-(lw+1)-(rw+1))
	} else {
		colL, _ := m.columnWidths()
		m.hcol = colL
	}
	m.hmax = 0
	for _, it := range items {
		if it.row < 0 {
			continue
		}
		row := m.res.Rows[it.row]
		m.hmax = max(m.hmax, displayWidth(row.Left), displayWidth(row.Right))
	}
	m.hoff = clamp(m.hoff, 0, m.maxHOff())
}

// columnWidths returns the side-by-side text budget of the left and right
// column (gutters and separator already subtracted).
func (m Model) columnWidths() (colL, colR int) {
	lw, rw := m.gutterWidths()
	avail := m.w - (lw + 1) - (rw + 1) - 3 // two gutters + " │ "
	return max(1, avail/2), max(1, avail-avail/2)
}

// emitSeparator renders one collapsed-gap row and stamps the hidden rows'
// rowStarts onto it. A selection covering part of the label highlights it —
// and copies the gap's hidden rows in full (#2070).
func (m *Model) emitSeparator(gi int, st styles) {
	g := m.gaps[gi]
	line := len(m.lines)
	m.sepLines[line] = gi
	for r := g.start; r < g.end; r++ {
		m.rowStarts[r] = line
	}
	label := m.sepLabel(gi)
	rendered := st.gutter.Render(label)
	runes := []rune(label)
	if a, b := m.sel.LineRange(line, len(runes)); m.sel.Active() && a < b {
		a = clamp(a, 0, len(runes))
		rendered = st.gutter.Render(string(runes[:a])) + st.sel.Render(string(runes[a:b])) +
			st.gutter.Render(string(runes[b:]))
	}
	m.lines = append(m.lines, rendered)
}

// sepLabel is the placeholder text of one collapsed gap, centering padding
// included — selection column math and the render agree on the same runes.
func (m *Model) sepLabel(gi int) string {
	g := m.gaps[gi]
	label := fmt.Sprintf("··· %d unchanged lines (o expands, c shows all) ···", g.end-g.start)
	if pad := (m.w - len([]rune(label))) / 2; pad > 0 {
		label = strings.Repeat(" ", pad) + label
	}
	return label
}

// styles bundles the resolved lipgloss styles one render pass reuses.
type styles struct {
	gutter      lipgloss.Style
	same        lipgloss.Style
	added       lipgloss.Style
	removed     lipgloss.Style
	addedEmph   lipgloss.Style
	removedEmph lipgloss.Style
	sel         lipgloss.Style
}

func (m Model) styles() styles {
	pal := m.pal
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	// Intra-line emphasis (#2170) is the line's own colour one step stronger
	// (the DiffAddedEmph/DiffRemovedEmph slots) plus bold: the background
	// step alone stays inside the palette's readability envelope and is easy
	// to miss, while bold marks the changed runes in every theme, including
	// the monochrome-ish ones. (Underline would be the other candidate, but
	// lipgloss emits it per grapheme — one escape pair per rune — which
	// bloats every changed line and breaks plain-text matching downstream.)
	emph := func(bg color.Color) lipgloss.Style {
		return lipgloss.NewStyle().Background(bg).Bold(true)
	}
	return styles{
		gutter:      lipgloss.NewStyle().Faint(true),
		same:        lipgloss.NewStyle(),
		added:       lipgloss.NewStyle().Background(pal.DiffAdded),
		removed:     lipgloss.NewStyle().Background(pal.DiffRemoved),
		addedEmph:   emph(pal.DiffAddedEmph),
		removedEmph: emph(pal.DiffRemovedEmph),
		sel:         lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText),
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

// emph returns the intra-line emphasis style belonging to one side of a row —
// the added side's on the right of a changed pair (and on an added line), the
// removed side's on the left (and on a removed line), so an emphasized range
// never leaves the colour of the line carrying it.
func (st styles) emph(kind Kind, right bool) lipgloss.Style {
	if kind == RowAdded || (kind == RowChanged && right) {
		return st.addedEmph
	}
	return st.removedEmph
}

// renderSideBySide paints two aligned columns with a dual gutter:
// "NNN old │ NNN new". Neither side wraps (#1700): every row is exactly one
// visual line, both sides clipped to their column budget from the shared
// horizontal offset, so corresponding rows and columns stay aligned.
func (m *Model) renderSideBySide(items []displayItem) {
	st := m.styles()
	hl := m.hlTheme()
	lw, rw := m.gutterWidths()
	colL, colR := m.columnWidths()
	sep := st.gutter.Render(" │ ")
	for _, it := range items {
		if it.row < 0 {
			m.emitSeparator(it.gi, st)
			continue
		}
		ri := it.row
		row := m.res.Rows[ri]
		line := len(m.lines)
		m.rowStarts[ri] = line
		capsL := sideCaps(m.leftIx, row.LeftNo, row.Left)
		capsR := sideCaps(m.rightIx, row.RightNo, row.Right)
		selL0, selL1 := m.selCols(line, false)
		selR0, selR1 := m.selCols(line, true)
		var b strings.Builder
		runesL, gapL := expand(row.Left), row.Kind == RowAdded
		runesR, gapR := expand(row.Right), row.Kind == RowRemoved
		b.WriteString(m.gutterCell(row.LeftNo, lw, row.Kind != RowAdded, st))
		b.WriteString(m.stampHScroll(renderSegment(runesL, gapL, m.hoff, colL,
			st.base(row.Kind, false), st.emph(row.Kind, false), expandSpans(row.Left, row.LeftSpans), capsL, hl,
			selL0, selL1, st.sel), runesL, gapL, colL))
		b.WriteString(sep)
		b.WriteString(m.gutterCell(row.RightNo, rw, row.Kind != RowRemoved, st))
		b.WriteString(m.stampHScroll(renderSegment(runesR, gapR, m.hoff, colR,
			st.base(row.Kind, true), st.emph(row.Kind, true), expandSpans(row.Right, row.RightSpans), capsR, hl,
			selR0, selR1, st.sel), runesR, gapR, colR))
		m.lines = append(m.lines, b.String())
	}
}

// renderUnified paints a single column with a dual line-number gutter; a
// changed pair renders as its removed line followed by its added line. Lines
// never wrap — they clip at the column edge and scroll horizontally (#1700).
func (m *Model) renderUnified(items []displayItem) {
	st := m.styles()
	hl := m.hlTheme()
	lw, rw := m.gutterWidths()
	col := max(1, m.w-(lw+1)-(rw+1))
	emit := func(text string, leftNo, rightNo int, base, emph lipgloss.Style, spans []Span, caps []string) {
		sel0, sel1 := m.selCols(len(m.lines), false)
		var b strings.Builder
		b.WriteString(m.gutterCell(leftNo, lw, true, st))
		b.WriteString(m.gutterCell(rightNo, rw, true, st))
		runes := expand(text)
		b.WriteString(m.stampHScroll(renderSegment(runes, false, m.hoff, col, base, emph, expandSpans(text, spans), caps, hl,
			sel0, sel1, st.sel), runes, false, col))
		m.lines = append(m.lines, b.String())
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
			emit(row.Left, row.LeftNo, row.RightNo, st.same, st.same, nil, sideCaps(m.rightIx, row.RightNo, row.Right))
		case RowChanged:
			emit(row.Left, row.LeftNo, 0, st.removed, st.removedEmph, row.LeftSpans, sideCaps(m.leftIx, row.LeftNo, row.Left))
			emit(row.Right, 0, row.RightNo, st.added, st.addedEmph, row.RightSpans, sideCaps(m.rightIx, row.RightNo, row.Right))
		case RowRemoved:
			emit(row.Left, row.LeftNo, 0, st.removed, st.removedEmph, row.LeftSpans, sideCaps(m.leftIx, row.LeftNo, row.Left))
		case RowAdded:
			emit(row.Right, 0, row.RightNo, st.added, st.addedEmph, row.RightSpans, sideCaps(m.rightIx, row.RightNo, row.Right))
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

// gutterCell renders one line-number cell: the line number, blank on the gap
// side. Rows are one visual line each (#1700), so there are no continuations.
func (m Model) gutterCell(no, width int, present bool, st styles) string {
	if !present || no == 0 {
		return strings.Repeat(" ", width+1)
	}
	return st.gutter.Render(fmt.Sprintf("%*d ", width, no))
}

// renderSegment paints one row's visible window — display columns [hoff,
// hoff+width) of the tab-expanded line, padded to width cells: base-styled
// text with span ranges painted in emphSt — the side's stronger emphasis
// background, in bold (#2170) — and syntax captures as
// foreground (#1699).
// Syntax colours the runes, the diff state colours the background, and both
// survive inside an emphasized range. Spans and captures are indexed in
// absolute display columns, so intra-line emphasis stays on the right runes at
// any horizontal offset. gap renders the empty counterpart of a one-sided row
// (blank, unstyled) rather than an empty styled line. [selA, selB) is the
// mouse selection's covered column interval (#2070); selected cells take selSt
// whole — the selection outranks emphasis and syntax while it lives.
func renderSegment(runes []rune, gap bool, hoff, width int, base, emphSt lipgloss.Style, spans []Span, caps []string, hl *highlight.Theme, selA, selB int, selSt lipgloss.Style) string {
	if gap {
		return strings.Repeat(" ", width)
	}
	start := clamp(hoff, 0, len(runes))
	end := clamp(hoff+width, start, len(runes))
	var b strings.Builder
	col := start
	for col < end {
		emph := inSpan(spans, col)
		cname := capAt(caps, col)
		selOn := col >= selA && col < selB
		e := col + 1
		for e < end && inSpan(spans, e) == emph && capAt(caps, e) == cname &&
			(e >= selA && e < selB) == selOn {
			e++
		}
		st := base
		if emph {
			st = emphSt
		}
		if cname != "" && hl != nil {
			if cs, ok := hl.Style(cname); ok {
				st = st.Foreground(cs.GetForeground())
			}
		}
		if selOn {
			st = selSt
		}
		b.WriteString(st.Render(string(runes[col:e])))
		col = e
	}
	if pad := width - (end - start); pad > 0 {
		b.WriteString(base.Render(strings.Repeat(" ", pad)))
	}
	return b.String()
}

// capAt returns the capture covering display column col, "" past the line end.
func capAt(caps []string, col int) string {
	if col < 0 || col >= len(caps) {
		return ""
	}
	return caps[col]
}

// sideCaps returns the syntax capture per tab-expanded display column of one
// side's raw line, aligned with expand(raw); nil when that side is a gap or
// its document produced no spans (#1699). lineNo is the 1-based line number
// within that side's document.
func sideCaps(ix highlight.Index, lineNo int, raw string) []string {
	if lineNo <= 0 || ix.Empty() || raw == "" {
		return nil
	}
	out := make([]string, 0, len(raw))
	for i, r := range []rune(raw) {
		c := ix.CaptureAt(lineNo-1, i)
		n := 1
		if r == '\t' {
			n = tabWidth
		}
		for k := 0; k < n; k++ {
			out = append(out, c)
		}
	}
	return out
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

// displayWidth returns the tab-expanded display width of a raw line, matching
// expand without allocating the rune slice.
func displayWidth(line string) int {
	w := 0
	for _, r := range line {
		if r == '\t' {
			w += tabWidth
			continue
		}
		w++
	}
	return w
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
