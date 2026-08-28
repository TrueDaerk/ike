package help

import (
	"sort"

	"charm.land/lipgloss/v2"
)

// gutter is the blank space inserted between adjacent columns.
const gutter = 2

// defaultMinColWidth is the floor used when no per-entry width or config value
// drives the column width.
const defaultMinColWidth = 20

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

// TypicalColumnWidth returns the narrowest width that still shows pct percent
// of the cells in full, floored like MinColumnWidth. It is the counterpart to
// MinColumnWidth: where that one answers "how wide must a column be so nothing
// is clipped", this one answers "how wide must it be so almost nothing is" —
// the width a single unusually long command title must not be allowed to
// dictate (#2215). pct outside 1..100 clamps into range.
func TypicalColumnWidth(cells []string, configMin, pct int) int {
	floor := configMin
	if floor < 1 {
		floor = defaultMinColWidth
	}
	if len(cells) == 0 {
		return floor
	}
	if pct < 1 {
		pct = 1
	}
	if pct > 100 {
		pct = 100
	}
	widths := make([]int, len(cells))
	for i, c := range cells {
		widths[i] = lipgloss.Width(c)
	}
	sort.Ints(widths)
	// ceil(pct% of n) cells fit, so at least one always does.
	at := (len(widths)*pct + 99) / 100
	if at < 1 {
		at = 1
	}
	w := widths[at-1]
	if w < floor {
		return floor
	}
	return w
}

// ColumnLayout picks how many columns of what width to render into a body
// budget of width cells. natural is the width that clips nothing, floor the
// narrowest column still worth having (see TypicalColumnWidth). It returns the
// largest column count up to maxCols whose columns stay at or above floor,
// preferring natural when the budget affords it — so a lone overlong entry
// costs that one row a truncation instead of collapsing the whole sheet into a
// single column (#2215).
func ColumnLayout(width, natural, floor, maxCols int) (cols, colW int) {
	if maxCols < 1 {
		maxCols = 1
	}
	if floor < 1 {
		floor = defaultMinColWidth
	}
	if natural < floor {
		natural = floor
	}
	for c := maxCols; c > 1; c-- {
		share := (width - gutter*(c-1)) / c
		if share < floor {
			continue
		}
		if share > natural {
			share = natural
		}
		return c, share
	}
	if natural > width {
		natural = width
	}
	if natural < 1 {
		natural = 1
	}
	return 1, natural
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
