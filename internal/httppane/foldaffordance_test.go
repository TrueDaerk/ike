package httppane

import (
	"strings"
	"testing"
)

// foldaffordance_test.go covers the response viewer's copy affordance on a
// collapsed fold (#1787): the ⧉ glyph, its one-cell hit target next to the
// toggle/select meaning of the rest of the header row, and the copied text.

// mappingFold is the fold's full content — header row through end row, the
// hidden rows included — as CopyFoldAt should hand it to the host.
const mappingFold = mappingRows

func TestCollapsedHeaderRendersCopyGlyph(t *testing.T) {
	m := foldViewer(t)
	header := bodyRow(m, "  \"mapping\": {")
	m.ToggleFold(header)
	view := m.View()
	if !strings.Contains(view, foldCopyGlyph) {
		t.Errorf("collapsed header should carry %q:\n%s", foldCopyGlyph, view)
	}
}

func TestOpenFoldRendersNoCopyGlyph(t *testing.T) {
	m := foldViewer(t) // nothing collapsed
	if view := m.View(); strings.Contains(view, foldCopyGlyph) {
		t.Errorf("no fold is collapsed, so no row may carry %q:\n%s", foldCopyGlyph, view)
	}
}

// TestFoldCopyHitIsOnlyTheGlyphCell is the hit-target contract: the glyph cell
// copies, the gutter still toggles, the text cells still select.
func TestFoldCopyHitIsOnlyTheGlyphCell(t *testing.T) {
	m := foldViewer(t)
	header := bodyRow(m, "  \"mapping\": {")
	m.ToggleFold(header)
	x, ok := m.foldCopyColumn(header)
	if !ok {
		t.Fatal("a collapsed header must have a copy column")
	}
	// The header is the row at display index header (nothing above it is
	// hidden), one line below the title bar.
	y := m.DisplayRow(header) - m.top + 1

	if row, hit := m.FoldCopyHit(x, y); !hit || row != header {
		t.Fatalf("the glyph cell: hit=%v row=%d, want true/%d", hit, row, header)
	}
	for _, bad := range []int{0, 1, x - 1, x + 1} {
		if _, hit := m.FoldCopyHit(bad, y); hit {
			t.Errorf("cell %d is not the affordance but reported a copy hit", bad)
		}
	}
	// A row with no collapsed fold never hits — not even at the same column.
	if _, hit := m.FoldCopyHit(x, 1); hit {
		t.Error("the status row heads no collapsed fold but reported a copy hit")
	}
}

// TestFoldCopyGlyphClickCopiesHiddenRows is the acceptance case: clicking the
// glyph copies the whole JSON object, hidden rows and all.
func TestFoldCopyGlyphClickCopiesHiddenRows(t *testing.T) {
	m := foldViewer(t)
	header := bodyRow(m, "  \"mapping\": {")
	m.ToggleFold(header)
	x, _ := m.foldCopyColumn(header)
	row, ok := m.FoldCopyHit(x, m.DisplayRow(header)-m.top+1)
	if !ok {
		t.Fatal("the glyph cell should be a copy hit")
	}
	cmd := m.CopyFoldAt(row)
	if cmd == nil {
		t.Fatal("copying a collapsed fold should emit a command")
	}
	msg, isCopy := cmd().(CopyMsg)
	if !isCopy {
		t.Fatalf("copy message = %T, want CopyMsg", cmd())
	}
	if msg.Text != mappingFold {
		t.Fatalf("copied %q, want %q", msg.Text, mappingFold)
	}
	if !strings.Contains(msg.What, "4 folded lines") {
		t.Errorf("copy label = %q, want it to report 4 folded lines", msg.What)
	}
	// The fold stays collapsed: the glyph copies, it does not toggle.
	if _, still := m.FoldedAt(header); !still {
		t.Error("copying must not expand the fold")
	}
}

// TestHeaderClickOutsideGlyphStillToggles is the counter-case the issue calls
// out: the rest of the header keeps its meaning.
func TestHeaderClickOutsideGlyphStillToggles(t *testing.T) {
	m := foldViewer(t)
	header := bodyRow(m, "  \"mapping\": {")
	m.ToggleFold(header)
	y := m.DisplayRow(header) - m.top + 1
	if _, hit := m.FoldCopyHit(0, y); hit {
		t.Fatal("the gutter cell must not be a copy target")
	}
	m.MousePress(0, y) // the gutter toggle, unchanged
	if _, still := m.FoldedAt(header); still {
		t.Error("a gutter click on a collapsed header should expand it")
	}
}

// TestFoldCopyHitWithRequestLine mirrors TestFoldCopyHitIsOnlyTheGlyphCell
// with a two-row header: the extra request-line row must shift the hit test
// down with it (#2450).
func TestFoldCopyHitWithRequestLine(t *testing.T) {
	m := foldViewerWithRequest(t)
	if got, want := m.headerLineCount(), 2; got != want {
		t.Fatalf("headerLineCount = %d, want %d", got, want)
	}
	header := bodyRow(m, "  \"mapping\": {")
	m.ToggleFold(header)
	x, ok := m.foldCopyColumn(header)
	if !ok {
		t.Fatal("a collapsed header must have a copy column")
	}
	y := m.DisplayRow(header) - m.top + m.headerLineCount()

	if row, hit := m.FoldCopyHit(x, y); !hit || row != header {
		t.Fatalf("the glyph cell: hit=%v row=%d, want true/%d", hit, row, header)
	}
	// One row up — where the click would land under the old, hard-coded
	// single-row header offset — must miss.
	if _, hit := m.FoldCopyHit(x, y-1); hit {
		t.Error("the request-line row must not report a copy hit")
	}
}

// TestGutterToggleWithRequestLine mirrors TestHeaderClickOutsideGlyphStillToggles
// with a two-row header: the gutter toggle must hit the row under the
// pointer, not the row above it (#2450).
func TestGutterToggleWithRequestLine(t *testing.T) {
	m := foldViewerWithRequest(t)
	header := bodyRow(m, "  \"mapping\": {")
	y := m.DisplayRow(header) - m.top + m.headerLineCount()
	m.MousePress(0, y)
	if _, folded := m.FoldedAt(header); !folded {
		t.Error("a gutter click on the row under the pointer should fold it")
	}
}

// TestCopyFoldAtOpenFoldIsNoOp: no affordance, no copy.
func TestCopyFoldAtOpenFoldIsNoOp(t *testing.T) {
	m := foldViewer(t)
	header := bodyRow(m, "  \"mapping\": {")
	if _, ok := m.foldCopyColumn(header); ok {
		t.Error("an open fold header has no copy column")
	}
	if cmd := m.CopyFoldAt(header); cmd != nil {
		t.Error("CopyFoldAt on an open fold must be a no-op")
	}
}

// TestFoldCopyKeyCopiesTargetFold is the keyboard pendant (zy): it copies the
// viewer's target fold whole, collapsed or not.
func TestFoldCopyKeyCopiesTargetFold(t *testing.T) {
	m := foldViewer(t)
	header := bodyRow(m, "  \"mapping\": {")
	m.ToggleFold(header)
	m.ScrollToRow(header)

	if cmd := m.handleKey(keyPress("z")); cmd != nil {
		t.Fatal("the z prefix alone should not copy")
	}
	cmd := m.handleKey(keyPress("y"))
	if cmd == nil {
		t.Fatal("zy should emit a copy command")
	}
	msg, ok := cmd().(CopyMsg)
	if !ok {
		t.Fatalf("copy message = %T, want CopyMsg", cmd())
	}
	if msg.Text != mappingFold {
		t.Fatalf("zy copied %q, want %q", msg.Text, mappingFold)
	}
}

// TestFoldCopyKeyOnOpenFoldCopiesRange: zy without collapsing still copies the
// target fold's full range — the point of the command is to skip the marking.
func TestFoldCopyKeyOnOpenFoldCopiesRange(t *testing.T) {
	m := foldViewer(t)
	m.ScrollToRow(bodyRow(m, "  \"mapping\": {"))
	cmd := m.CopyTargetFold()
	if cmd == nil {
		t.Fatal("an open target fold should still be copyable")
	}
	if msg := cmd().(CopyMsg); msg.Text != mappingFold {
		t.Fatalf("copied %q, want %q", msg.Text, mappingFold)
	}
}
