package complexitycli

import (
	"bytes"
	"io"
	"strings"
	"text/tabwriter"

	"charm.land/lipgloss/v2"

	"ike/internal/cli"
	"ike/internal/gridview"
)

// ResolveWidth fills in the text tables' line limit (#2992) when --width
// was not given: termCols, the terminal's width as cmd/ike measured it on
// stdout, or cli.DefaultWidth when stdout is not a terminal or its size is
// unknown (termCols ≤ 0).
func ResolveWidth(a *cli.ComplexityArgs, termCols int) {
	if a.WidthSet {
		return
	}
	a.Width = cli.DefaultWidth
	if termCols > 0 {
		a.Width = termCols
	}
}

// colKind is how a text-table column truncates under the width limit.
type colKind int

const (
	colNum  colKind = iota // keeps its natural width
	colPath                // truncated from the left, the file name stays
	colName                // truncated from the right, marks kept
)

// table is the text renderers' tabwriter (#2992): it buffers the
// tab-separated lines and, on Flush, fits the text columns into the width
// limit — the numeric columns keep their natural width, the text columns
// share the rest proportionally (gridview.FitWidths, never below
// gridview.MinTextWidth) and are truncated with an ellipsis. Without a
// limit (width 0) the lines go through text/tabwriter unchanged.
type table struct {
	w     io.Writer
	width int
	kinds []colKind
	buf   bytes.Buffer
}

// newTable is a table over w limited to s's width; kinds name the text
// columns from the left, every later column is numeric.
func (s settings) newTable(w io.Writer, kinds ...colKind) *table {
	return &table{w: w, width: s.args.Width, kinds: kinds}
}

func (t *table) Write(p []byte) (int, error) { return t.buf.Write(p) }

func (t *table) kind(i int) colKind {
	if i < len(t.kinds) {
		return t.kinds[i]
	}
	return colNum
}

// Flush writes the buffered lines.
func (t *table) Flush() error {
	if t.width <= 0 {
		tw := tabwriter.NewWriter(t.w, 0, 0, 2, ' ', 0)
		_, _ = tw.Write(t.buf.Bytes())
		return tw.Flush()
	}
	text := strings.TrimSuffix(t.buf.String(), "\n")
	if text == "" {
		return nil
	}
	var rows [][]string
	var natural []int
	for _, line := range strings.Split(text, "\n") {
		cells := strings.Split(line, "\t")
		for i, c := range cells {
			if i == len(natural) {
				natural = append(natural, 0)
			}
			natural[i] = max(natural[i], lipgloss.Width(c))
		}
		rows = append(rows, cells)
	}
	isText := make([]bool, len(natural))
	budget := t.width - 2*(len(natural)-1)
	for i, w := range natural {
		if t.kind(i) == colNum {
			budget -= w
		} else {
			isText[i] = true
		}
	}
	fit := gridview.FitWidths(natural, isText, budget, gridview.MinTextWidth)
	// Truncate, then align like tabwriter: a cell followed by another is
	// padded to its column's widest such cell plus two spaces, a line's
	// last cell is not padded.
	pad := make([]int, len(natural))
	for _, cells := range rows {
		for i := range cells {
			cells[i] = truncCell(t.kind(i), cells[i], fit[i])
			if i < len(cells)-1 {
				pad[i] = max(pad[i], lipgloss.Width(cells[i]))
			}
		}
	}
	var b strings.Builder
	for _, cells := range rows {
		for i, c := range cells {
			if i < len(cells)-1 {
				c = gridview.PadTo(c, pad[i]) + "  "
			}
			b.WriteString(c)
		}
		b.WriteString("\n")
	}
	_, err := io.WriteString(t.w, b.String())
	return err
}

// truncCell fits one cell to width: a path from the left, a name from
// the right keeping the accepted / pinned marks; a leading indent stays.
func truncCell(k colKind, s string, width int) string {
	if k == colNum || lipgloss.Width(s) <= width {
		return s
	}
	body := strings.TrimLeft(s, " ")
	indent := s[:len(s)-len(body)]
	if k == colPath {
		return gridview.TruncMarked(indent, body, "", width, true)
	}
	suffix := ""
	for _, mark := range []string{pinMark, acceptedMark} {
		if strings.HasSuffix(body, mark) {
			suffix = mark + suffix
			body = body[:len(body)-len(mark)]
		}
	}
	return gridview.TruncMarked(indent, body, suffix, width, false)
}
