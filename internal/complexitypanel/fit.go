package complexitypanel

import (
	"strings"

	"charm.land/lipgloss/v2"

	"ike/internal/gridview"
)

// The column budget (#2992): the text columns — file, function / tree
// name, class, section, commit subject — share whatever the pane leaves
// after the numeric columns, proportionally to their natural width and
// never below gridview.MinTextWidth, so a long path or qualified name can
// no longer push the metrics off the pane. A clipped cell is truncated with
// an ellipsis — a path from the left (the file name stays), a name from the
// right — keeping its caret and accepted / pinned marks; the footer then
// shows the cursor row's full text. The exports read the rows, not the
// cells, so they keep the full text.

// fitWidths applies the budget to the natural widths ws of the tab's
// layout; an unsized pane keeps them.
func (m *Model) fitWidths(ws []int) []int {
	layout := m.layout()
	if m.width <= 0 || len(layout) != len(ws) {
		return ws
	}
	text := make([]bool, len(ws))
	budget := m.width - 1 - 2*(len(ws)-1) // leading space, two-space gaps
	for i, c := range layout {
		if c.numeric() {
			budget -= ws[i]
		} else {
			text[i] = true
		}
	}
	return gridview.FitWidths(ws, text, budget, gridview.MinTextWidth)
}

// fitCells truncates the text cells that overflow their column width, in
// place.
func (m *Model) fitCells(layout []Column, cells []gridview.Cell) {
	for i, c := range layout {
		if i < len(cells) && i < len(m.widths) && !c.numeric() {
			cells[i].Text = fitText(c, cells[i].Text, m.widths[i])
		}
	}
}

// fitText truncates one text cell of column c to width cells: a file path
// from the left, every other text from the right; the function cell's
// caret and trailing marks survive.
func fitText(c Column, s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	if c == ColFile {
		return gridview.TruncLeft(s, width)
	}
	prefix, core, suffix := "", s, ""
	if c == ColFunc {
		prefix, core, suffix = splitMarks(s)
	}
	return gridview.TruncMarked(prefix, core, suffix, width, false)
}

// splitMarks splits a function cell into the caret prefix, the name and
// the trailing accepted / pinned / dirty marks.
func splitMarks(s string) (prefix, core, suffix string) {
	if strings.HasPrefix(s, caretMark) {
		prefix, s = caretMark, s[len(caretMark):]
	}
	for _, mark := range []string{dirtyMark, pinMark, acceptedMark} {
		if strings.HasSuffix(s, mark) {
			suffix = mark + suffix
			s = s[:len(s)-len(mark)]
		}
	}
	return prefix, s, suffix
}

// fitLabels truncates the header labels to the column widths, keeping the
// sort marker.
func (m *Model) fitLabels(labels []string) []string {
	out := append([]string(nil), labels...)
	for i, l := range out {
		if i >= len(m.widths) || lipgloss.Width(l) <= m.widths[i] {
			continue
		}
		core, suffix := l, ""
		for _, mark := range []string{" ▲", " ▼"} {
			if strings.HasSuffix(l, mark) {
				core, suffix = l[:len(l)-len(mark)], mark
			}
		}
		out[i] = gridview.TruncMarked("", core, suffix, m.widths[i], false)
	}
	return out
}

// dataRow draws one grid row through gridview.DataRow after fitting its
// text cells.
func (m *Model) dataRow(cells []gridview.Cell, selected bool) string {
	m.fitCells(m.layout(), cells)
	return gridview.DataRow(m.theme(), cells, m.widths, 0, m.width, selected, m.focused)
}

// clippedText is the full text of the cursor row's truncated cells, " · "
// joined in column order, "" when nothing is truncated (the footer's lead).
func (m *Model) clippedText() string {
	var texts, full []string
	layout := m.layout()
	switch {
	case m.treeActive():
		r := m.tree.Current()
		if r == nil {
			return ""
		}
		texts = m.treeCells(r, layout)
		full = append([]string(nil), texts...)
		if fi := m.layoutIndex(ColFunc); fi >= 0 {
			full[fi] = r.Item.Name // the name, not the indent and fold marker
		}
	case m.cursor >= 0 && m.cursor < len(m.rows):
		r := m.rows[m.cursor]
		full = r.cells(layout)
		texts = append([]string(nil), full...)
		if fi := m.layoutIndex(ColFunc); fi >= 0 && r.Caret {
			texts[fi] = caretMark + texts[fi] // measured as drawn
		}
	default:
		return ""
	}
	var parts []string
	for i, c := range layout {
		if i < len(texts) && i < len(m.widths) && !c.numeric() && lipgloss.Width(texts[i]) > m.widths[i] {
			parts = append(parts, full[i])
		}
	}
	return strings.Join(parts, " · ")
}
