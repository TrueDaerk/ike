package diff

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/textsel"
)

// selection.go is the diff viewer's mouse text selection and copy support
// (#2070), after the HTTP response viewer's model (#1266): a drag selects
// across the rendered lines with the shared click-streak gestures (word on
// double click, line on triple), and y / ctrl+c / cmd+c put the selection on
// the clipboard. The view composes lines from gutters and content — in
// side-by-side even two columns — so the selection lives in the *content*
// cells: positions are (visual line, display column) pairs keyed to one side,
// and extraction maps them back onto the raw row text. A collapsed-context
// separator stands in for its hidden rows, so a selection touching it copies
// the real hidden lines, never the placeholder label (#1741's rule).

// CopyMsg asks the host to put Text on the system clipboard; What names the
// copied thing for the confirmation notice ("selection", "hunk"). The pane
// cannot reach the clipboard itself, mirroring httppane.CopyMsg.
type CopyMsg struct {
	Text string
	What string
}

// CopyCmd wraps text in a CopyMsg command, or nil when there is nothing to
// copy — an empty clipboard write would silently destroy what the user had.
// Exported for the merge view, which shares the message type (#2070).
func CopyCmd(text, what string) tea.Cmd {
	if text == "" {
		return nil
	}
	return func() tea.Msg { return CopyMsg{Text: text, What: what} }
}

// vrow describes one rendered visual line for selection: the res.Rows index
// it shows (-1 for a collapsed-context separator, with gi the gap index), and
// — in unified layout, where a changed pair renders as two lines — which side
// the line's text comes from.
type vrow struct {
	row   int
	gi    int
	right bool
}

// buildVRows records the visual-line map the render pass is about to paint,
// so selection positions resolve to row text without re-deriving the layout.
func (m *Model) buildVRows(items []displayItem) {
	m.vrows = m.vrows[:0]
	for _, it := range items {
		if it.row < 0 {
			m.vrows = append(m.vrows, vrow{row: -1, gi: it.gi})
			continue
		}
		if !m.unified {
			m.vrows = append(m.vrows, vrow{row: it.row})
			continue
		}
		switch m.res.Rows[it.row].Kind {
		case RowChanged:
			m.vrows = append(m.vrows, vrow{row: it.row}, vrow{row: it.row, right: true})
		case RowAdded:
			m.vrows = append(m.vrows, vrow{row: it.row, right: true})
		default: // RowSame renders its Left text, like renderUnified
			m.vrows = append(m.vrows, vrow{row: it.row})
		}
	}
}

// MousePress anchors a selection at the pane-local cell (x, y). In
// side-by-side layout the pressed column claims the selection: it never mixes
// the two sides. Edit mode's embedded editor brings its own selection (#496),
// so the press is a no-op there.
func (m *Model) MousePress(x, y int) {
	if m.editModeOn || len(m.vrows) == 0 {
		return
	}
	p, right := m.posAt(x, y)
	if !m.unified && right != m.selRight {
		// Switching sides restarts the gesture: the click streak and any old
		// selection belong to the other column.
		m.sel.Clear()
	}
	m.selRight = right
	m.sel.Press(p, m.selRunes)
	m.render()
}

// MouseDrag extends the selection to (x, y), clamped into the column the
// press chose.
func (m *Model) MouseDrag(x, y int) {
	if m.editModeOn || len(m.vrows) == 0 {
		return
	}
	m.sel.Drag(m.posAtSide(x, y, m.selRight), m.selRunes)
	m.render()
}

// MouseRelease ends the drag; the selection stays visible until it is copied
// or a new press lands.
func (m *Model) MouseRelease() { m.sel.Release() }

// HasSelection reports whether a selection exists.
func (m *Model) HasSelection() bool { return m.sel.Active() }

// ClearSelection drops the selection and re-renders the highlight away.
func (m *Model) ClearSelection() {
	if !m.sel.Active() {
		m.sel.Clear()
		return
	}
	m.sel.Clear()
	m.render()
}

// clearSelection drops the selection without re-rendering, for mutators whose
// own render pass follows.
func (m *Model) clearSelection() { m.sel.Clear() }

// posAt maps a pane-local cell onto a selection position; right reports the
// side-by-side column the cell falls in (always false in unified layout).
func (m *Model) posAt(x, y int) (p textsel.Pos, right bool) {
	if !m.unified {
		lw, _ := m.gutterWidths()
		colL, _ := m.columnWidths()
		// The separator's middle splits the pane: presses left of it select in
		// the left column, right of it in the right column.
		right = x >= lw+1+colL+2
	}
	return m.posAtSide(x, y, right), right
}

// posAtSide maps a cell onto a position within one side's content column,
// clamping the column into that side's text — a drag past the other column
// lands on the line end instead of crossing over.
func (m *Model) posAtSide(x, y int, right bool) textsel.Pos {
	line := clamp(m.top+y, 0, len(m.vrows)-1)
	var col int
	if m.vrows[line].row < 0 {
		// A separator renders from the pane's left edge and never h-scrolls,
		// so its columns are the cell coordinates themselves.
		col = x
	} else if m.unified {
		lw, rw := m.gutterWidths()
		col = x - (lw + 1 + rw + 1) + m.hoff
	} else if right {
		lw, rw := m.gutterWidths()
		colL, _ := m.columnWidths()
		col = x - (lw + 1 + colL + 3 + rw + 1) + m.hoff
	} else {
		lw, _ := m.gutterWidths()
		col = x - (lw + 1) + m.hoff
	}
	return textsel.Pos{Line: line, Col: clamp(col, 0, len(m.selRunes(line)))}
}

// selRunes returns the display runes selection sees on one visual line: the
// selected side's tab-expanded text, or a separator's rendered label.
func (m *Model) selRunes(line int) []rune {
	if line < 0 || line >= len(m.vrows) {
		return nil
	}
	vr := m.vrows[line]
	if vr.row < 0 {
		return []rune(m.sepLabel(vr.gi))
	}
	row := m.res.Rows[vr.row]
	if m.sideOf(vr) {
		return expand(row.Right)
	}
	return expand(row.Left)
}

// sideOf resolves which side a visual line's text comes from: the emitted
// side in unified layout, the selection's column in side-by-side.
func (m *Model) sideOf(vr vrow) bool {
	if m.unified {
		return vr.right
	}
	return m.selRight
}

// selCols returns the display-column interval [a, b) the selection covers on
// one visual line; a >= b means none. right names the side-by-side column
// being painted, so the untouched column never highlights.
func (m *Model) selCols(line int, right bool) (a, b int) {
	if !m.sel.Active() || (!m.unified && right != m.selRight) {
		return 0, 0
	}
	return m.sel.LineRange(line, len(m.selRunes(line)))
}

// SelectionText extracts the selected text: for every covered visual line the
// covered display columns map back onto the raw row text (undoing the tab
// expansion), and a covered separator contributes its hidden rows in full.
func (m *Model) SelectionText() string {
	start, end, ok := m.sel.Range()
	if !ok {
		return ""
	}
	var parts []string
	for line := max(0, start.Line); line <= end.Line && line < len(m.vrows); line++ {
		vr := m.vrows[line]
		a, b := m.sel.LineRange(line, len(m.selRunes(line)))
		if vr.row < 0 {
			// The label is a placeholder; the selection covers the fold's real
			// content (#1741): every hidden row, whole.
			if a >= b {
				continue
			}
			g := m.gaps[vr.gi]
			for r := g.start; r < g.end; r++ {
				// Gap rows are RowSame, so both sides carry the same text.
				parts = append(parts, m.res.Rows[r].Left)
			}
			continue
		}
		row := m.res.Rows[vr.row]
		raw := row.Left
		if m.sideOf(vr) {
			raw = row.Right
		}
		parts = append(parts, textsel.RawSlice(raw, a, b, tabWidth))
	}
	return strings.Join(parts, "\n")
}

// HunkPatchText renders the current hunk (the first before any n/N) as a
// minimal unified patch — the copy target when no selection exists, analog to
// the response pane's "no selection → whole body".
func (m *Model) HunkPatchText() string {
	if len(m.res.Hunks) == 0 {
		return ""
	}
	i := m.cur
	if i < 0 {
		i = 0
	}
	h := m.res.Hunks[i]
	ls, rs, lc, rc := 0, 0, 0, 0
	var minus, plus []string
	for _, row := range m.res.Rows[h.Start:h.End] {
		if row.LeftNo > 0 {
			if ls == 0 {
				ls = row.LeftNo
			}
			lc++
			minus = append(minus, "-"+row.Left)
		}
		if row.RightNo > 0 {
			if rs == 0 {
				rs = row.RightNo
			}
			rc++
			plus = append(plus, "+"+row.Right)
		}
	}
	// A zero-count side anchors on the line before the change, the unified
	// diff convention for pure additions/removals.
	if lc == 0 && h.Start > 0 {
		ls = m.res.Rows[h.Start-1].LeftNo
	}
	if rc == 0 && h.Start > 0 {
		rs = m.res.Rows[h.Start-1].RightNo
	}
	lines := append([]string{fmt.Sprintf("@@ -%d,%d +%d,%d @@", ls, lc, rs, rc)}, minus...)
	return strings.Join(append(lines, plus...), "\n")
}

// copyKey handles the copy chords: the selection when one exists (cleared on
// copy, like the response pane), else the current hunk as a unified patch.
func (m *Model) copyKey() tea.Cmd {
	if m.sel.Active() {
		text := m.SelectionText()
		m.ClearSelection()
		return CopyCmd(text, "selection")
	}
	return CopyCmd(m.HunkPatchText(), "hunk")
}
