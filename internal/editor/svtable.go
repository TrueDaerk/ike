package editor

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"ike/internal/highlight"
	"ike/internal/sv"
	"ike/internal/theme"
)

// svtable.go is the table-like rendering of separator-delimited files
// (#1589), display-only: every separator becomes a dynamic conceal range
// (#1594) whose stand-in is the alignment padding — fields line up at shared
// column widths, colored by the theme-derived rainbow.N spans the csv
// language plugin already assigns (plugins/languages/csv). Because the rows
// render through the ordinary cell loop (not a pre-rendered bypass), cursor,
// selection and search styling all work on aligned rows; only the one
// separator the caret sits on — or a selection crosses — reverts to its raw
// character (lineConcealRanges). The buffer never changes. Column widths are
// measured over the *visible* rows plus the header row, keeping the cost
// viewport-bound.
//
// The first line additionally pins at the top of the viewport while
// scrolling (stickyLines), riding the sticky-scroll machinery (#168).
//
// The column the caret sits in is tinted over the whole visible height and
// named in the status line (#1659), so a wide table stays readable once the
// header row has scrolled away.

// svColGap is the display spacing between padded columns; the concealed
// separator reads as part of it.
const svColGap = 2

// svState caches the visible-row column layout. A pointer field on Model,
// like mdTables, so the value copies each Update returns share it.
type svState struct {
	valid       bool
	version     int
	top, height int
	sep         rune
	widths      []int
}

// svLangID returns the buffer's *sv language id, or "" when the buffer is
// not a separator-delimited file.
func (m Model) svLangID() string {
	if id := highlight.Lang(m.path); sv.IsLang(id) {
		return id
	}
	return ""
}

// svActive reports whether the table rendering applies to this buffer right
// now: the editor.csv_rendering toggle is on, the buffer is a *sv language,
// and soft wrap is off (a padded row sliced by raw-text wrap segments would
// tear, like the markdown tables).
func (m Model) svActive() bool {
	return m.svRenderOn() && !m.softWrap && m.svLangID() != ""
}

// svLayout returns the current column layout, recomputing when the document
// version or the viewport moved: widths[i] is the widest field i across the
// visible rows plus the header row (display-only "longest visible row"
// scoping, so large files never measure beyond the viewport).
func (m Model) svLayout() *svState {
	st := m.svTable
	if st.valid && st.version == m.docVersion && st.top == m.view.Top && st.height == m.view.Height() {
		return st
	}
	st.valid, st.version, st.top, st.height = true, m.docVersion, m.view.Top, m.view.Height()
	st.sep = sv.Separator(m.svLangID(), m.buf.Lines())
	st.widths = st.widths[:0]
	measure := func(line int) {
		for i, f := range sv.Fields(m.buf.Line(line), st.sep) {
			w := f.End - f.Start
			if i == len(st.widths) {
				st.widths = append(st.widths, w)
			} else if w > st.widths[i] {
				st.widths[i] = w
			}
		}
	}
	measure(0) // the pinned header always participates in the layout
	end := m.view.Top + m.view.Height()
	if n := m.buf.LineCount(); end > n {
		end = n
	}
	for line := m.view.Top; line < end; line++ {
		if line > 0 {
			measure(line)
		}
	}
	return st
}

// svCacheState folds the table layout into the line cache's validity check
// (syncEpoch): a vertical scroll can change the visible-row column widths —
// and so every rendered row — without bumping the render epoch. Zero for
// non-sv buffers, so ordinary files keep their scroll cache reuse.
func (m Model) svCacheState() uint64 {
	if !m.svActive() {
		return 0
	}
	st := m.svLayout()
	const prime = 1099511628211
	h := uint64(1469598103934665603)
	mix := func(v uint64) { h ^= v; h *= prime }
	mix(uint64(st.version))
	mix(uint64(st.top))
	mix(uint64(st.height))
	for _, w := range st.widths {
		mix(uint64(w))
	}
	if h == 0 {
		h = 1
	}
	return h
}

// svConcealRanges builds the dynamic conceal ranges for line (#1594): each
// separator conceals behind its column's alignment padding — the column's
// width minus the field's own length, plus the fixed gap — so the ordinary
// cell loop renders the row aligned. Ranges are plain spacing (standIn
// false), never tinted like decoded stand-ins. lineConcealRanges applies the
// caret/selection reveal on top.
func (m Model) svConcealRanges(line int) []concealRange {
	if !m.svActive() || line >= m.buf.LineCount() {
		return nil
	}
	st := m.svLayout()
	fields := sv.Fields(m.buf.Line(line), st.sep)
	if len(fields) < 2 {
		return nil
	}
	out := make([]concealRange, 0, len(fields)-1)
	for i, f := range fields[:len(fields)-1] {
		pad := svColGap
		if flen := f.End - f.Start; i < len(st.widths) && st.widths[i] > flen {
			pad += st.widths[i] - flen
		}
		out = append(out, concealRange{start: f.End, end: f.End + 1, repl: strings.Repeat(" ", pad)})
	}
	return out
}

// svDisplayCol is the display-cell offset of buffer column col from the start
// of line through the concealed expansion (#1724): concealed separators count
// as their alignment padding, a tab as tabWidth cells. It is the sv analogue
// of a raw rune column — view.Left holds this display measure while the table
// rendering is active, so every row can shed the same width when the view
// scrolls horizontally.
func (m Model) svDisplayCol(line, col int) int {
	runes := []rune(m.buf.Line(line))
	conceals := m.lineConcealRanges(line)
	disp := 0
	for c := 0; c < col; c++ {
		if cr, ok := rangeAt(conceals, c); ok {
			if cr.repl != "" && c == cr.start {
				disp += lipgloss.Width(cr.repl)
			}
			continue
		}
		if c < len(runes) && runes[c] == '\t' {
			disp += m.tabWidth
		} else {
			disp++
		}
	}
	return disp
}

// svDisplaySlice maps the display-cell offset left back into line's concealed
// expansion (#1724): the buffer column rendering starts at, plus the cells of
// that column's stand-in already scrolled off — a window edge landing inside
// alignment padding emits only the padding's tail. A tab straddling the edge
// snaps to its own start (like the raw path, which never splits a tab).
// Offsets past the line end map on 1:1, mirroring displayClickCol.
func (m Model) svDisplaySlice(line, left int) (col, skip int) {
	runes := []rune(m.buf.Line(line))
	conceals := m.lineConcealRanges(line)
	disp := 0
	for c := 0; c < len(runes); c++ {
		w := 0
		if cr, ok := rangeAt(conceals, c); ok {
			if cr.repl != "" && c == cr.start {
				w = lipgloss.Width(cr.repl)
			}
		} else if runes[c] == '\t' {
			w = m.tabWidth
		} else {
			w = 1
		}
		if disp+w > left {
			if _, ok := rangeAt(conceals, c); ok {
				return c, left - disp
			}
			return c, 0
		}
		disp += w
	}
	return len(runes) + (left - disp), 0
}

// svColumnTintFrac is how much of the selection colour survives the mix toward
// the editor surface: the column stripe marks a whole screen height, so it has
// to stay well below a selection in strength. Deriving it from two palette
// colours keeps it subtle on light and dark themes alike.
const svColumnTintFrac = 0.45

// svColumnTint is the background of the caret's column (#1659).
func (m Model) svColumnTint() color.Color {
	return theme.Mix(m.theme().SelectionMuted, m.theme().Surface, svColumnTintFrac)
}

// svCursorColumn is the zero-based index of the field the caret sits in, and
// whether the buffer is table-rendered at all.
func (m Model) svCursorColumn() (int, bool) {
	if !m.svActive() || m.cursor.Line >= m.buf.LineCount() {
		return 0, false
	}
	return sv.IndexAt(m.buf.Line(m.cursor.Line), m.svLayout().sep, m.cursor.Col), true
}

// svColumnRange is the rune-column range [start, end) that the caret's column
// covers on line — the field plus the separator closing it, so the stripe
// spans the alignment padding too and reads as one full-width column. It is
// empty (ok false) when the line has no such column: rows are only tinted
// where they actually have the field, which keeps the cost to one split of the
// visible line. Unfocused panes show no stripe, like they show no caret.
func (m Model) svColumnRange(line int) (start, end int, ok bool) {
	if !m.focused {
		return 0, 0, false
	}
	idx, ok := m.svCursorColumn()
	if !ok || line >= m.buf.LineCount() {
		return 0, 0, false
	}
	fields := sv.Fields(m.buf.Line(line), m.svLayout().sep)
	if idx >= len(fields) {
		return 0, 0, false
	}
	f := fields[idx]
	end = f.End
	if idx < len(fields)-1 {
		end++ // the concealed separator carries this column's padding
	}
	if end <= f.Start {
		return 0, 0, false
	}
	return f.Start, end, true
}

// SVColumnLabel is the status-line label for the caret's column in a
// table-rendered buffer (#1659): "column 3: qty" when the first row reads like
// a header, "column 3" when it does not (or has fewer fields). Empty for every
// other buffer, which hides the segment.
func (m Model) SVColumnLabel() string {
	idx, ok := m.svCursorColumn()
	if !ok {
		return ""
	}
	label := "column " + strconv.Itoa(idx+1)
	if names, ok := sv.Header(m.buf.Line(0), m.svLayout().sep); ok && idx < len(names) {
		label += ": " + names[idx]
	}
	return label
}
