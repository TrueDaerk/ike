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
type Cell struct {
	Text string
	Null bool
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
