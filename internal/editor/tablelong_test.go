package editor

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"ike/internal/editor/buffer"
)

// longTableDoc builds a markdown document holding a pipe table of rows rows
// — the Markdown export of the Complexity tool window on a large repository
// (#2963), the shape behind #2993.
func longTableDoc(rows int) string {
	var b strings.Builder
	b.WriteString("# Complexity\n\n")
	b.WriteString("| File | Function | Cyclomatic | Cognitive | Lines | Params | Nesting |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
	for i := 1; i <= rows; i++ {
		fmt.Fprintf(&b, "| path/to/file%d.go | Some.function%d | 12 | 34 | 56 | 7 | 8 |\n", i, i)
	}
	b.WriteString("\n## Notes\n\nclosing text\n")
	return b.String()
}

// TestTableCaretMoveRendersNoBlock (#2993): with the caret inside a pipe
// table, a caret move or click lays out only the visible rows — the block is
// never re-rendered. The old path re-rendered the whole block once per
// visible row and per column mapping, which made a click in a 5000-row table
// cost seconds.
func TestTableCaretMoveRendersNoBlock(t *testing.T) {
	m, _ := mdLoaded(t, longTableDoc(5000))
	m.SetSize(80, 40)
	m.cursor = buffer.Position{Line: 0}
	_ = m.View()
	before := tableRenders.Load()
	for _, p := range []buffer.Position{{Line: 10, Col: 3}, {Line: 2000, Col: 20}, {Line: 2000, Col: 0}, {Line: 3, Col: 5}} {
		m.cursor = p
		m.scroll()
		if v := m.View(); !strings.Contains(v, "│") {
			t.Fatalf("caret at %v: table lost its box drawing:\n%s", p, v)
		}
		// The column mappings a caret move and a click go through.
		m.tableDisplayCol(p.Line, p.Col)
		m.tableClickCol(p.Line, 5)
	}
	m.MouseClick(10, 5)
	_ = m.View()
	if got := tableRenders.Load() - before; got != 0 {
		t.Fatalf("caret moves re-rendered the table block %d times, want 0", got)
	}
}

// referenceRawRows is the layout the raw-revealed cell used to get from a
// whole-block re-render: the cell swapped into its row, every width
// recomputed over all rows, each row laid out afresh.
func referenceRawRows(blk mdTableBlock, raw *mdRawCell) []string {
	cells := make([][]string, len(blk.cells))
	for ri, row := range blk.cells {
		cells[ri] = append([]string(nil), row...)
	}
	cells[raw.row][raw.cell] = raw.display()
	widths := make([]int, len(blk.widths))
	for ri, row := range cells {
		if ri == 1 {
			continue
		}
		for ci, c := range row {
			if w := lipgloss.Width(c); w > widths[ci] {
				widths[ci] = w
			}
		}
	}
	out := make([]string, len(cells))
	for ri, row := range cells {
		rawCell := -1
		if ri == raw.row {
			rawCell = raw.cell
		}
		out[ri] = layoutTableRow(row, widths, blk.aligns, ri, rawCell)
	}
	return out
}

// TestTableRawCellViewMatchesFullRender (#2993): the on-demand row layout
// with the cursor's raw cell reproduces the whole-block re-render it
// replaced, for the cursor in every cell of every row, including a cell
// whose raw source is wider than the column.
func TestTableRawCellViewMatchesFullRender(t *testing.T) {
	doc := "| Name | Qty | Note |\n| :--- | ---: | :---: |\n| **apple** | 3 | `x` |\n| [pear](p) | 12 | long *text* here |\n"
	m, _ := mdLoaded(t, doc)
	for line := 0; line < 4; line++ {
		if line == 1 {
			continue
		}
		segs := rowCellSegs(m.buf.Line(line))
		for ci, seg := range segs {
			for col := seg.from; col < seg.to; col++ {
				m.cursor = buffer.Position{Line: line, Col: col}
				v, rawLine, ok := m.tableRowsAt(line)
				if !ok || rawLine >= 0 || v.raw == nil {
					t.Fatalf("line %d col %d: expected a raw cell view, got ok=%v rawLine=%d", line, col, ok, rawLine)
				}
				if v.raw.cell != ci {
					t.Fatalf("line %d col %d: raw cell %d, want %d", line, col, v.raw.cell, ci)
				}
				want := referenceRawRows(v.blk, v.raw)
				for ri := range want {
					if got := v.row(v.blk.start + ri); got != want[ri] {
						t.Fatalf("line %d col %d: row %d differs\n got %q\nwant %q", line, col, ri, got, want[ri])
					}
				}
			}
		}
	}
}

// BenchmarkTableCaretMoveView measures one frame with the caret inside a
// 5000-row pipe table after a caret move (the line cache is cold, as after a
// click): the #2993 hot path.
func BenchmarkTableCaretMoveView(b *testing.B) {
	m, _ := mdLoaded(b, longTableDoc(5000))
	m.SetSize(80, 40)
	m.cursor = buffer.Position{Line: 2000, Col: 3}
	m.scroll()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.cursor.Col = 3 + i%10 // a new caret state clears the line cache
		_ = m.View()
	}
}
