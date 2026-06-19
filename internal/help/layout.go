package help

import "github.com/charmbracelet/lipgloss"

// gutter is the blank space inserted between adjacent columns.
const gutter = 2

// defaultMinColWidth is the floor used when no per-entry width or config value
// drives the column width.
const defaultMinColWidth = 20

// ColumnCount returns how many columns of minColWidth (plus a gutter each) fit
// in width. It never drops below one, so narrow terminals fall back to a single
// column.
func ColumnCount(width, minColWidth int) int {
	if minColWidth < 1 {
		minColWidth = 1
	}
	cols := width / (minColWidth + gutter)
	if cols < 1 {
		return 1
	}
	return cols
}

// MinColumnWidth derives the column width from the widest rendered cell, never
// below configMin (or defaultMinColWidth when configMin is non-positive).
func MinColumnWidth(cells []string, configMin int) int {
	floor := configMin
	if floor < 1 {
		floor = defaultMinColWidth
	}
	w := floor
	for _, c := range cells {
		if cw := lipgloss.Width(c); cw > w {
			w = cw
		}
	}
	return w
}

// Pack distributes cells column-major into cols balanced columns: each column
// is filled top-to-bottom before the next, with rows = ceil(len/cols) so the
// columns differ in height by at most one. The result is indexed [col][row].
// cols <= 0 collapses to a single column.
func Pack(cells []string, cols int) [][]string {
	if cols < 1 {
		cols = 1
	}
	n := len(cells)
	if n == 0 {
		return nil
	}
	if cols > n {
		cols = n
	}
	rows := (n + cols - 1) / cols // ceil
	out := make([][]string, 0, cols)
	for i := 0; i < n; i += rows {
		end := i + rows
		if end > n {
			end = n
		}
		col := make([]string, end-i)
		copy(col, cells[i:end])
		out = append(out, col)
	}
	return out
}

// renderColumns lays packed columns side by side, padding each cell to colWidth
// and inserting a gutter between columns. Columns are top-aligned; shorter
// columns are not padded with trailing blank rows (lipgloss handles ragged
// joins).
func renderColumns(columns [][]string, colWidth int) string {
	if len(columns) == 0 {
		return ""
	}
	cellStyle := lipgloss.NewStyle().Width(colWidth)
	rendered := make([]string, len(columns))
	for i, col := range columns {
		lines := make([]string, len(col))
		for j, cell := range col {
			lines[j] = cellStyle.Render(cell)
		}
		rendered[i] = lipgloss.JoinVertical(lipgloss.Left, lines...)
	}
	gap := lipgloss.NewStyle().Width(gutter).Render("")
	joined := rendered[0]
	for i := 1; i < len(rendered); i++ {
		joined = lipgloss.JoinHorizontal(lipgloss.Top, joined, gap, rendered[i])
	}
	return joined
}
