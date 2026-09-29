package editor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestHostFoldSummaryBudgetKeepsRowInPane (#2782): a host summary is offered
// the cells left on the header row, and a summary that fills them exactly
// leaves the row inside the pane with the copy affordance still drawn and
// still hit — the wider jq placeholder must not cost the click target.
func TestHostFoldSummaryBudgetKeepsRowInPane(t *testing.T) {
	m := closedFoldModel(t)
	var got int
	m.SetFoldSummary(func(header, end, budget int) string {
		got = budget
		return strings.Repeat("k", budget)
	})
	rows := strings.Split(m.View(), "\n")
	if got <= 0 {
		t.Fatalf("budget = %d, want the room left on a 40-cell row", got)
	}
	header := rows[2-m.view.Top]
	if w := ansi.StringWidth(header); w > 40 {
		t.Fatalf("header row is %d cells wide, pane is 40: %q", w, ansi.Strip(header))
	}
	if !strings.Contains(header, strings.Repeat("k", got)) || !strings.Contains(header, foldCopyGlyph) {
		t.Fatalf("header row lost the summary or the copy glyph: %q", ansi.Strip(header))
	}
	off, _, ok := m.foldCopyCell(2, 7)
	if !ok {
		t.Fatal("no room for the copy affordance with a budget-sized summary")
	}
	if line, hit := m.FoldCopyHit(m.view.GutterWidth(m.buf.LineCount())+off, 2-m.view.Top); !hit || line != 2 {
		t.Fatalf("FoldCopyHit = %d, %v, want the fold header 2", line, hit)
	}
}

// TestHostFoldSummaryOverBudgetIsClipped: a host ignoring the budget still
// cannot push the row past the pane.
func TestHostFoldSummaryOverBudgetIsClipped(t *testing.T) {
	m := closedFoldModel(t)
	m.SetFoldSummary(func(header, end, budget int) string { return strings.Repeat("k", 200) })
	for _, row := range strings.Split(m.View(), "\n") {
		if w := ansi.StringWidth(row); w > 40 {
			t.Fatalf("row is %d cells wide, pane is 40: %q", w, ansi.Strip(row))
		}
	}
}
