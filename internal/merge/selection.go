package merge

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/diff"
	"ike/internal/textsel"
)

// selection.go is the merge view's mouse text selection over its read-only
// side columns (#2070): ours (left) and theirs (right) get the shared
// click-streak gestures, and y / ctrl+c / cmd+c copy the selection through
// diff.CopyMsg. The editable result column in the middle belongs to the
// embedded editor, which brings its own selection and copy — presses there
// are not the view's to take.

// tabWidth matches expandTabs: side-column tabs render four cells wide.
const tabWidth = 4

// MousePress anchors a selection at the pane-local cell (x, y) and reports
// whether the press landed in a side column; a middle-column or header press
// returns false and leaves the view untouched.
func (m *Model) MousePress(x, y int) bool {
	if y < 1 {
		return false // the header row
	}
	left, mid, _ := m.colWidths()
	// The separators' middles split the pane: left of the first is ours,
	// right of the second is theirs, between them the editor's ground.
	theirs := false
	switch {
	case x < left+sepWidth/2+1:
	case x >= left+sepWidth+mid+sepWidth/2:
		theirs = true
	default:
		return false
	}
	if theirs != m.selTheirs {
		// Switching sides restarts the gesture: the click streak and any old
		// selection belong to the other column.
		m.sel.Clear()
	}
	m.selTheirs = theirs
	m.sel.Press(m.posAt(x, y), m.selRunes)
	return true
}

// MouseDrag extends the selection to (x, y), clamped into the column the
// press chose.
func (m *Model) MouseDrag(x, y int) { m.sel.Drag(m.posAt(x, y), m.selRunes) }

// MouseRelease ends the drag; the selection stays visible until it is copied
// or a new press lands.
func (m *Model) MouseRelease() { m.sel.Release() }

// HasSelection reports whether a side-column selection exists.
func (m *Model) HasSelection() bool { return m.sel.Active() }

// ClearSelection drops the selection and any drag in progress.
func (m *Model) ClearSelection() { m.sel.Clear() }

// posAt maps a pane-local cell into the selected side's grid: document lines
// (the columns follow the result editor's scroll) by display columns.
func (m *Model) posAt(x, y int) textsel.Pos {
	line := m.ed.ScrollTop() + y - 1 // the header takes y == 0
	if line < 0 {
		line = 0
	}
	left, mid, _ := m.colWidths()
	col := x
	if m.selTheirs {
		col = x - (left + sepWidth + mid + sepWidth)
	}
	return textsel.Pos{Line: line, Col: clampInt(col, 0, len(m.selRunes(line)))}
}

// sideLines returns the selected column's document lines.
func (m *Model) sideLines() []string {
	if m.selTheirs {
		return m.theirs
	}
	return m.ours
}

// selRunes returns the display runes of one document line of the selected
// side, tab-expanded like the render.
func (m *Model) selRunes(line int) []rune {
	lines := m.sideLines()
	if line < 0 || line >= len(lines) {
		return nil
	}
	return []rune(expandTabs(lines[line]))
}

// selCols returns the display-column interval [a, b) the selection covers on
// one document line of the given side; a >= b means none.
func (m *Model) selCols(line int, theirs bool) (a, b int) {
	if !m.sel.Active() || theirs != m.selTheirs {
		return 0, 0
	}
	return m.sel.LineRange(line, len(m.selRunes(line)))
}

// SelectionText extracts the selected text of the chosen side column, display
// columns mapped back onto the raw lines (undoing the tab expansion).
func (m *Model) SelectionText() string {
	start, end, ok := m.sel.Range()
	if !ok {
		return ""
	}
	lines := m.sideLines()
	var b []byte
	for line := max(0, start.Line); line <= end.Line && line < len(lines); line++ {
		if line > start.Line {
			b = append(b, '\n')
		}
		a, e := m.sel.LineRange(line, len(m.selRunes(line)))
		b = append(b, textsel.RawSlice(lines[line], a, e, tabWidth)...)
	}
	return string(b)
}

// selKey intercepts the copy chords while a side selection lives: they copy
// it instead of reaching the result editor. Bare "y" stays with the editor
// while it captures text (insert mode typing must not be stolen); esc clears
// the selection and still falls through — the editor may need it for its
// mode. ok reports whether the key was consumed.
func (m *Model) selKey(k tea.KeyPressMsg) (tea.Cmd, bool) {
	if !m.sel.Active() {
		return nil, false
	}
	switch k.String() {
	case "y":
		if m.ed.Capturing() {
			return nil, false
		}
	case "ctrl+c", "cmd+c", "super+c":
	case "esc":
		m.sel.Clear()
		return nil, false
	default:
		return nil, false
	}
	text := m.SelectionText()
	m.sel.Clear()
	return diff.CopyCmd(text, "selection"), true
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
