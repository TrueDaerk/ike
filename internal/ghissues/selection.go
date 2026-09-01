package ghissues

// selection.go is the detail views' mouse text selection and copy support
// (#2374). Both full-area details — the issue detail and the PR detail — are
// lists of pre-rendered lines scrolled by an offset, exactly the shape the
// HTTP response viewer (#1266) and the diff viewer (#2070) select over, so
// the gesture engine is the shared one: internal/textsel. A press anchors, a
// drag extends, a release closes the span, the click streak cycles
// char → word → line, and 'y' / cmd+c put the span on the clipboard through
// the host — the pane never touches the clipboard itself.
//
// What is copied is the *rendered* text, not the markdown source underneath:
// the detail wraps and styles its body through glamour, and the user selects
// what they see. That is the rule the response viewer follows for a
// pretty-printed body too. Trailing render padding is trimmed off every line,
// so a whole-line selection copies the text, not the block's spaces.
//
// The selection belongs to one detail: selTab/selNum record which view and
// which issue/PR it was taken in, so walking to the next issue, switching
// tabs or closing the detail retires it without a Clear() at every exit.

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/textsel"
)

// CopyMsg asks the host to put Text on the system clipboard; What names the
// copied thing for the confirmation notice. The pane cannot reach the
// clipboard, mirroring httppane.CopyMsg and diff.CopyMsg.
type CopyMsg struct {
	Text string
	What string
}

// MousePress anchors a selection at the pane-local cell (x, y) and reports
// whether a selection drag was armed. Only the body of an open detail
// selects: the tab bar, the filter row and the position header keep their
// click semantics (#2090, #2104), and an open modal owns the pointer.
func (m *Model) MousePress(x, y int) bool {
	if !m.detailShown() || m.ov != ovNone || y < m.bodyTop() {
		return false
	}
	m.selTab, m.selNum = m.tab, m.detailNumber()
	m.selX, m.selY = x, y
	m.selDrag = true
	m.sel.Press(m.posAt(x, y), m.selRunes)
	return true
}

// MouseDrag extends the selection to (x, y) in the unit the press chose.
func (m *Model) MouseDrag(x, y int) {
	if !m.selDrag {
		return
	}
	m.selX, m.selY = x, y
	m.sel.Drag(m.posAt(x, y), m.selRunes)
}

// MouseRelease ends the drag; the selection stays visible until it is copied
// or a new press lands.
func (m *Model) MouseRelease() {
	m.selDrag = false
	m.sel.Release()
}

// dragScroll extends a running selection after the view scrolled under the
// resting pointer (#2374): the wheel during a drag must grow the span, not
// abandon it, so the last pointer cell is re-resolved against the new offset.
func (m *Model) dragScroll() {
	if !m.selDrag {
		return
	}
	m.sel.Drag(m.posAt(m.selX, m.selY), m.selRunes)
}

// HasSelection reports whether the shown detail carries a selection.
func (m *Model) HasSelection() bool { return m.selActive() }

// ClearSelection drops the selection and any drag in progress.
func (m *Model) ClearSelection() {
	m.sel.Clear()
	m.selDrag = false
}

// selActive reports whether a live selection belongs to the detail on screen.
// A selection taken in another issue, another PR or the other tab is stale —
// its line indices address lines that are no longer rendered.
func (m *Model) selActive() bool {
	return m.sel.Active() && m.detailShown() &&
		m.selTab == m.tab && m.selNum == m.detailNumber()
}

// detailNumber is the issue or pull request the open detail shows; 0 when no
// detail is open.
func (m *Model) detailNumber() int {
	switch {
	case m.detail && m.tab == TabIssues:
		if is := m.Selected(); is != nil {
			return is.Number
		}
	case m.prDetail && m.tab == TabPRs:
		return m.prdFor
	}
	return 0
}

// selLines are the rendered lines of the open detail and the offset they are
// scrolled by — the grid the selection addresses.
func (m *Model) selLines() ([]string, int) {
	if m.prDetail && m.tab == TabPRs {
		return m.prdLines, m.prdTop
	}
	return m.detailLines, m.detailTop
}

// selRunes are the display runes of one grid line: the rendered line with its
// styling and its trailing render padding removed, so a selection never
// copies escape sequences or the padding the markdown block adds.
func (m *Model) selRunes(line int) []rune {
	lines, _ := m.selLines()
	if line < 0 || line >= len(lines) {
		return nil
	}
	return []rune(strings.TrimRight(ansi.Strip(lines[line]), " "))
}

// posAt maps a pane-local cell onto a grid position. y counts from the first
// body row, x is a display cell resolved to the rune it sits in — a click
// past the end of a line lands on its end.
func (m *Model) posAt(x, y int) textsel.Pos {
	lines, top := m.selLines()
	line := top + y - m.bodyTop()
	if line > len(lines)-1 {
		line = len(lines) - 1
	}
	if line < 0 {
		line = 0
	}
	return textsel.Pos{Line: line, Col: cellToRune(m.selRunes(line), x)}
}

// cellToRune resolves a display column to the rune index covering it, or the
// line's rune count when the column is past the text.
func cellToRune(runes []rune, cell int) int {
	if cell <= 0 {
		return 0
	}
	w := 0
	for i, r := range runes {
		w += ansi.StringWidth(string(r))
		if w > cell {
			return i
		}
	}
	return len(runes)
}

// runeToCell is the inverse: the display column a rune index starts at.
func runeToCell(runes []rune, i int) int {
	if i <= 0 {
		return 0
	}
	if i > len(runes) {
		i = len(runes)
	}
	return ansi.StringWidth(string(runes[:i]))
}

// selCols is the display-rune interval [a, b) the selection covers on one
// grid line; a >= b means none.
func (m *Model) selCols(line int) (a, b int) {
	if !m.selActive() {
		return 0, 0
	}
	return m.sel.LineRange(line, len(m.selRunes(line)))
}

// highlightSel paints the selected span of one rendered line. The covered
// cells are re-rendered as plain text in the selection colours — the span's
// own styling gives way to the selection, exactly as it does in the diff and
// terminal panes.
func (m *Model) highlightSel(line string, idx int) string {
	a, b := m.selCols(idx)
	if a >= b {
		return line
	}
	runes := m.selRunes(idx)
	pal := m.theme()
	st := lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText)
	return ansi.Truncate(line, runeToCell(runes, a), "") +
		st.Render(string(runes[a:b])) +
		ansi.TruncateLeft(line, runeToCell(runes, b), "")
}

// SelectionText extracts the selected text of the open detail: the covered
// runes of every covered line, joined by newlines — what the pane shows.
func (m *Model) SelectionText() string {
	start, end, ok := m.sel.Range()
	if !ok || !m.selActive() {
		return ""
	}
	lines, _ := m.selLines()
	var parts []string
	for i := max(0, start.Line); i <= end.Line && i < len(lines); i++ {
		runes := m.selRunes(i)
		a, b := m.sel.LineRange(i, len(runes))
		if a > b {
			a = b
		}
		parts = append(parts, string(runes[a:b]))
	}
	return strings.Join(parts, "\n")
}

// copySelection is the copy chord of the detail views ('y', cmd+c): the
// selection goes to the clipboard and is dropped, like the response viewer's.
// Without a selection there is nothing to copy and the key stays inert rather
// than copying a whole rendered issue nobody asked for.
func (m *Model) copySelection() tea.Cmd {
	if !m.selActive() {
		return nil
	}
	text := m.SelectionText()
	m.ClearSelection()
	if text == "" {
		return nil
	}
	msg := CopyMsg{Text: text, What: "selection"}
	return func() tea.Msg { return msg }
}

// copyAction advertises the copy in the footer and the action menu, but only
// while a selection exists — a key that would do nothing is not an action.
func (m *Model) copyAction() []action {
	if !m.selActive() {
		return nil
	}
	return []action{act("y", "copy", "Copy the selected text", (*Model).copySelection)}
}
