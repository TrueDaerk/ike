package editor

import (
	"strings"

	"ike/internal/highlight"
	"ike/internal/sv"
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
	return m.svRender && !m.softWrap && m.svLangID() != ""
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
