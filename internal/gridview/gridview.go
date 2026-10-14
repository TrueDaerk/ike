// Package gridview is the shared renderer behind the two sidebar-plus-grid
// panes (#2468): the data viewer (internal/dataview) and the Elasticsearch
// console (internal/espane). Both draw a list of objects on the left and a
// paged read-only grid of cells on the right, and until #2468 each carried a
// verbatim copy of the row arithmetic — the width budget, the NULL glyph, the
// selection styling, the blank sidebar filler. This package is a leaf: it
// knows nothing about tables, hits, cursors or keys, only how one row of
// cells and one column of list lines are drawn, so a pane's model stays the
// sole owner of what is selected and why.
//
// The panes' own cell types (datasrc.Cell, esq.Cell) stay where they are; a
// pane converts the one row it is about to draw, which keeps the backend
// packages free of a UI dependency.
package gridview

import (
	"strings"

	"charm.land/lipgloss/v2"

	"ike/internal/theme"
)

// Cell is one grid value, already rendered to display text. Null marks a
// value that is absent (SQL NULL, a field a document does not carry) — the
// grid draws it as NullCell, faint, so it never reads as an empty string.
//
// Cursor and Match are the cell-level marks of a grid that walks cells rather
// than rows (the playground's table view, #2794): the cell cursor draws
// reversed while the pane is focused, a search hit on the occurrence colour.
// Only the cell's own text takes the mark, never the gap after it.
type Cell struct {
	Text   string
	Null   bool
	Cursor bool
	Match  bool
	// Warn paints the cell's text in the warning colour: a metric at or
	// above its threshold (the Complexity window, #2917).
	Warn bool
	// Muted paints the cell faint: a warning the baseline accepted (the
	// Complexity window, #2958). Warn wins when both are set.
	Muted bool
	// Good paints the cell's text in the success colour: a pinned
	// function whose metrics fell (the Complexity window, #2959). Warn
	// wins when both are set.
	Good bool
}

// NullCell is the glyph a Null cell draws as — visibly distinct from the
// empty string, which renders as nothing.
const NullCell = "∅"

// DataRow draws one grid row from column colOff on inside a budget of w
// cells: a leading space, then each cell padded to its column width and
// separated by two spaces, clipped where the budget runs out. A Null cell
// draws the NullCell glyph, faint. selected paints the row's background —
// the selection colour while the pane is focused, the muted one while it is
// not — and extends it over the unused budget so the highlight spans the
// whole grid width. widths must cover every column index the row has.
func DataRow(pal *theme.Palette, cells []Cell, widths []int, colOff, w int, selected, focused bool) string {
	base := lipgloss.NewStyle().Foreground(pal.Foreground)
	nullStyle := lipgloss.NewStyle().Faint(true)
	if selected {
		if focused {
			base = base.Background(pal.Selection).Bold(true)
			nullStyle = nullStyle.Background(pal.Selection)
		} else {
			base = base.Background(pal.SelectionMuted)
			nullStyle = nullStyle.Background(pal.SelectionMuted)
		}
	}
	budget := w
	var b strings.Builder
	b.WriteString(base.Render(" "))
	budget--
	for c := colOff; c < len(cells) && budget > 0; c++ {
		cell := cells[c]
		text, style := cell.Text, base
		if cell.Null {
			text, style = NullCell, nullStyle
		}
		chunk := PadTo(text, widths[c])
		if cell.Cursor || cell.Match || cell.Warn || cell.Muted || cell.Good {
			chunk = ClipTo(chunk, budget)
			b.WriteString(markStyle(pal, style, cell, focused).Render(chunk))
			budget -= lipgloss.Width(chunk)
			if c < len(cells)-1 && budget > 0 {
				gap := ClipTo("  ", budget)
				b.WriteString(base.Render(gap))
				budget -= lipgloss.Width(gap)
			}
			continue
		}
		if c < len(cells)-1 {
			chunk += "  "
		}
		chunk = ClipTo(chunk, budget)
		b.WriteString(style.Render(chunk))
		budget -= lipgloss.Width(chunk)
	}
	if selected && budget > 0 {
		b.WriteString(base.Render(strings.Repeat(" ", budget)))
	}
	return b.String()
}

// markStyle is the style of a Cursor or Match cell on top of the row's.
func markStyle(pal *theme.Palette, style lipgloss.Style, cell Cell, focused bool) lipgloss.Style {
	if cell.Warn {
		style = style.Foreground(pal.Warning)
	} else if cell.Good {
		style = style.Foreground(pal.Success)
	} else if cell.Muted {
		style = style.Faint(true)
	}
	if cell.Match {
		style = style.Background(pal.OccurrenceRead)
	}
	if cell.Cursor && focused {
		style = style.Reverse(true)
	}
	return style
}

// HeaderRow draws the column labels from colOff on — each padded to its
// column width, two spaces apart, behind a leading space — bold in the
// accent colour and clipped to w cells. A label is whatever the pane wants
// in the header cell, a sort marker included, so widths should have been
// sized against the same labels.
func HeaderRow(pal *theme.Palette, labels []string, widths []int, colOff, w int) string {
	var cells []string
	for i := colOff; i < len(labels); i++ {
		cells = append(cells, PadTo(labels[i], widths[i]))
	}
	line := " " + strings.Join(cells, "  ")
	return lipgloss.NewStyle().Bold(true).Foreground(pal.Accent).Render(ClipTo(line, w))
}

// Sidebar draws height lines of a width-cell list column, rows top … top+
// height-1 of n entries through row, and blank (unstyled) filler below the
// last entry. Lines are newline-joined without a trailing newline, so the
// result joins horizontally against a grid of the same height.
func Sidebar(top, height, n, width int, row func(i, w int) string) string {
	var b strings.Builder
	for k := 0; k < height; k++ {
		line := strings.Repeat(" ", width)
		if i := top + k; i < n {
			line = row(i, width)
		}
		b.WriteString(line)
		if k < height-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// PadTo right-pads (or ellipsis-clips) s to exactly width cells.
func PadTo(s string, width int) string {
	if lipgloss.Width(s) > width {
		return ClipTo(s, width)
	}
	return s + strings.Repeat(" ", width-lipgloss.Width(s))
}

// ClipTo bounds one rendered chunk to width cells, ellipsis on overflow.
func ClipTo(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	r := []rune(s)
	if width < 1 {
		return ""
	}
	// Runes approximate cells closely enough here: grid text is
	// backend-rendered plain strings.
	if len(r) > width {
		r = r[:width-1]
		return string(r) + "…"
	}
	return s
}

// MinTextWidth is the narrowest a text column is squeezed to by FitWidths
// (#2992): below it a name is no longer recognisable.
const MinTextWidth = 12

// FitWidths caps the text columns of a table so the numeric ones stay
// visible (#2992). natural holds every column's natural width, text marks
// the text columns and budget is the room all text columns may take
// together (the line width minus the numeric columns and the gaps).
// Numeric columns keep their width. When the text columns fit they keep
// theirs too; otherwise the budget is split proportionally to the natural
// widths, a column never shrinking below min(natural, minW). A budget too
// small for those minima squeezes every text column to its minimum — the
// line then overflows and the renderer clips it as before. The result is
// a new slice.
func FitWidths(natural []int, text []bool, budget, minW int) []int {
	out := append([]int(nil), natural...)
	var open []int
	sum, floor := 0, 0
	for i, w := range natural {
		if i < len(text) && text[i] {
			open = append(open, i)
			sum += w
			floor += min(w, minW)
		}
	}
	if sum <= budget {
		return out
	}
	if budget <= floor {
		for _, i := range open {
			out[i] = min(natural[i], minW)
		}
		return out
	}
	// Water-fill: a column whose proportional share falls below its
	// minimum is pinned there and the rest re-share what is left.
	for len(open) > 0 {
		total := 0
		for _, i := range open {
			total += natural[i]
		}
		var keep []int
		left := budget
		for _, i := range open {
			if share := budget * natural[i] / total; share < min(natural[i], minW) {
				out[i] = min(natural[i], minW)
				left -= out[i]
			} else {
				keep = append(keep, i)
			}
		}
		if len(keep) == len(open) {
			break
		}
		open, budget = keep, left
	}
	total, used := 0, 0
	for _, i := range open {
		total += natural[i]
	}
	for _, i := range open {
		out[i] = budget * natural[i] / total
		used += out[i]
	}
	// Hand the rounding remainder out left to right.
	for k := 0; used < budget && k < len(open); k++ {
		if i := open[k]; out[i] < natural[i] {
			out[i]++
			used++
		}
	}
	return out
}

// TruncRight shortens plain text s to at most width cells, cutting from the
// right and ending in "…" (a name: "SomeLongClass::someVeryLo…"). Widths are
// measured with lipgloss.Width, so wide characters count double; a width
// below one yields "", exactly one the bare ellipsis.
func TruncRight(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	if width < 1 {
		return ""
	}
	room := width - 1
	var b strings.Builder
	for _, r := range s {
		w := lipgloss.Width(string(r))
		if w > room {
			break
		}
		b.WriteRune(r)
		room -= w
	}
	return b.String() + "…"
}

// TruncLeft shortens plain text s to at most width cells, cutting from the
// left and starting with "…" (a path keeps its file name:
// "…/complexitypanel/complexitypanel.go"). Measured like TruncRight.
func TruncLeft(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	if width < 1 {
		return ""
	}
	room := width - 1
	r := []rune(s)
	start := len(r)
	for start > 0 {
		w := lipgloss.Width(string(r[start-1]))
		if w > room {
			break
		}
		room -= w
		start--
	}
	return "…" + string(r[start:])
}

// TruncMarked shortens prefix+core+suffix to width cells by truncating only
// core (from the left when left is set, else from the right), so a leading
// mark (the caret) and a trailing one (accepted / pinned, a sort marker)
// survive. When the marks leave core less than two cells, the whole string
// is cut from the right instead.
func TruncMarked(prefix, core, suffix string, width int, left bool) string {
	s := prefix + core + suffix
	if lipgloss.Width(s) <= width {
		return s
	}
	room := width - lipgloss.Width(prefix) - lipgloss.Width(suffix)
	if room < 2 {
		return TruncRight(s, width)
	}
	if left {
		return prefix + TruncLeft(core, room) + suffix
	}
	return prefix + TruncRight(core, room) + suffix
}
