package editor

import (
	"fmt"
	"strings"

	"ike/internal/highlight"
	"ike/internal/sv"
)

// svtable.go is the table-like rendering of separator-delimited files
// (#1589), display-only like the markdown layer (#881): on lines the caret
// is not on, fields render padded to shared column widths with the separator
// concealed behind the spacing, colored by the theme-derived rainbow.N slots
// the csv language plugin already assigns (plugins/languages/csv). The
// caret line — and any selected line — shows raw source, so editing stays
// exact; the buffer never changes. Column widths are measured over the
// *visible* rows plus the header row, keeping the cost viewport-bound.
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

// svRawLine reports whether line must show raw source: the caret sits on it
// or a selection touches it — the same inspection rules as conceal (#1585).
func (m Model) svRawLine(line int) bool {
	if m.focused && line == m.cursor.Line {
		return true
	}
	for _, c := range m.carets {
		if c.pos.Line == line {
			return true
		}
	}
	_, _, sel := m.selectionOnLine(line, len([]rune(m.buf.Line(line))))
	return sel
}

// svRow returns the pre-styled aligned display row for line when the table
// rendering applies and the line is not held raw; renderSpanUncached slices
// it by display cells like a markdown table row.
func (m Model) svRow(line int) (string, bool) {
	if !m.svActive() || line >= m.buf.LineCount() || m.svRawLine(line) {
		return "", false
	}
	st := m.svLayout()
	runes := []rune(m.buf.Line(line))
	fields := sv.Fields(m.buf.Line(line), st.sep)
	var b strings.Builder
	for i, f := range fields {
		text := string(runes[f.Start:f.End])
		if style, ok := m.hlTheme.Style(fmt.Sprintf("rainbow.%d", i%highlight.RainbowColors)); ok {
			text = style.Render(text)
		}
		b.WriteString(text)
		if i+1 < len(fields) {
			pad := svColGap
			if i < len(st.widths) {
				pad += st.widths[i] - (f.End - f.Start)
			}
			b.WriteString(strings.Repeat(" ", pad))
		}
	}
	return b.String(), true
}

// svClickCol maps a display-cell offset on an aligned row back to a buffer
// column: within a field's text the mapping is 1:1, a click in the padding
// (the concealed separator and gap) lands on the separator, i.e. the field's
// end. ok is false when the line renders raw and the ordinary mapping
// applies.
func (m Model) svClickCol(line, offset int) (int, bool) {
	if _, ok := m.svRow(line); !ok {
		return 0, false
	}
	st := m.svLayout()
	fields := sv.Fields(m.buf.Line(line), st.sep)
	for i, f := range fields {
		flen := f.End - f.Start
		if i+1 == len(fields) {
			if offset < flen {
				return f.Start + offset, true
			}
			return f.End + (offset - flen), true
		}
		colw := flen
		if i < len(st.widths) && st.widths[i] > colw {
			colw = st.widths[i]
		}
		if offset < flen {
			return f.Start + offset, true
		}
		if offset < colw+svColGap {
			return f.End, true // the concealed separator
		}
		offset -= colw + svColGap
	}
	return 0, false
}

// svDisplayOffset converts a buffer column on an aligned row to its display
// cell: prior fields occupy their padded width plus the gap; the separator
// column itself sits at its field's rendered end. ok is false when the line
// renders raw.
func (m Model) svDisplayOffset(line, col int) (int, bool) {
	if _, ok := m.svRow(line); !ok {
		return 0, false
	}
	st := m.svLayout()
	disp := 0
	for i, f := range sv.Fields(m.buf.Line(line), st.sep) {
		flen := f.End - f.Start
		if col <= f.End {
			off := col - f.Start
			if off < 0 {
				off = 0
			}
			if off > flen {
				off = flen
			}
			return disp + off, true
		}
		colw := flen
		if i < len(st.widths) && st.widths[i] > colw {
			colw = st.widths[i]
		}
		disp += colw + svColGap
	}
	return disp, true
}
