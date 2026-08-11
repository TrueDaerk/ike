package editor

import (
	"strings"
	"testing"
)

// foldaffordance_test.go covers the collapsed fold's copy affordance (#1787):
// the ⧉ glyph on the header row, its hit target, and what the click and the
// zy / "Copy Folded Range" command put on the clipboard.

func TestClosedFoldHeaderShowsCopyGlyph(t *testing.T) {
	m := closedFoldModel(t)
	m = send(m, keys("kk")...) // cursor off the header row
	rows := strings.Split(m.View(), "\n")
	if !strings.Contains(rows[2], foldCopyGlyph) {
		t.Errorf("collapsed header row should carry %q, got %q", foldCopyGlyph, rows[2])
	}
	// An open fold's header is an ordinary row: no affordance.
	for i, r := range rows {
		if i != 2 && strings.Contains(r, foldCopyGlyph) {
			t.Errorf("row %d is not a collapsed header but shows %q: %q", i, foldCopyGlyph, r)
		}
	}
}

func TestOpenFoldHeaderHasNoCopyGlyph(t *testing.T) {
	m := foldModel(t) // nothing collapsed
	if v := m.View(); strings.Contains(v, foldCopyGlyph) {
		t.Errorf("no fold is collapsed, so no row may carry %q:\n%s", foldCopyGlyph, v)
	}
}

// TestFoldCopyHitTargetsOnlyTheGlyph is the hit-target contract: the one cell
// the glyph occupies copies, every other cell of the header row does not.
func TestFoldCopyHitTargetsOnlyTheGlyph(t *testing.T) {
	m := closedFoldModel(t)
	off, width, ok := m.foldCopyCell(2, 7)
	if !ok {
		t.Fatal("the 40-column pane should have room for the affordance")
	}
	gutter := m.view.GutterWidth(m.buf.LineCount())
	y := 2 - m.view.Top

	for x := off; x < off+width; x++ {
		if line, hit := m.FoldCopyHit(gutter+x, y); !hit || line != 2 {
			t.Fatalf("cell %d of the glyph: hit=%v line=%d, want true/2", x, hit, line)
		}
	}
	for _, x := range []int{0, 1, off - 1, off + width} {
		if _, hit := m.FoldCopyHit(gutter+x, y); hit {
			t.Errorf("text cell %d is not the affordance but reported a hit", x)
		}
	}
	// A row that heads no collapsed fold never hits, whatever the column.
	if _, hit := m.FoldCopyHit(gutter+off, 0); hit {
		t.Error("row 0 heads no collapsed fold but reported a copy hit")
	}
}

// TestFoldCopyHitClickCopiesWholeFold walks the mouse path: the click copies
// the header through the end line, hidden rows included.
func TestFoldCopyHitClickCopiesWholeFold(t *testing.T) {
	m := closedFoldModel(t)
	off, _, _ := m.foldCopyCell(2, 7)
	line, ok := m.FoldCopyHit(m.view.GutterWidth(m.buf.LineCount())+off, 2-m.view.Top)
	if !ok {
		t.Fatal("the glyph cell should be a copy hit")
	}
	cmd := m.CopyFoldAt(line)
	if cmd == nil {
		t.Fatal("copying a collapsed fold should return a feedback command")
	}
	if e := m.regs.Get(0); e.Text != foldedText || !e.Linewise {
		t.Fatalf("clipboard copy = %+v, want %q linewise", e, foldedText)
	}
	if got := noticeIn(t, cmd); !strings.Contains(got, "copied 6 lines") {
		t.Errorf("toast = %q, want it to report 6 copied lines", got)
	}
	// The fold stays collapsed and the cursor does not move: copying is not
	// a fold toggle (#1787).
	if _, still := m.foldedAt(2); !still {
		t.Error("copying must not expand the fold")
	}
}

// TestCopyFoldAtOpenFoldIsNoOp guards the mouse path against rows that carry
// no affordance.
func TestCopyFoldAtOpenFoldIsNoOp(t *testing.T) {
	m := foldModel(t)
	if cmd := m.CopyFoldAt(2); cmd != nil {
		t.Error("an open fold has no copy affordance, so CopyFoldAt must be a no-op")
	}
}

// TestFoldCopyCommandCopiesFoldUnderCursor is the keyboard pendant: zy on the
// collapsed header copies the same range as the click.
func TestFoldCopyCommandCopiesFoldUnderCursor(t *testing.T) {
	m := closedFoldModel(t)
	m = send(m, keys("zy")...)
	if e := m.regs.Get(0); e.Text != foldedText {
		t.Fatalf("zy on a collapsed fold = %q, want %q", e.Text, foldedText)
	}
}

// TestFoldCopyCommandOnOpenFoldTakesTheRange is why the command is useful
// beyond collapsed folds: with the cursor inside an open fold it copies the
// innermost enclosing range, no selection needed.
func TestFoldCopyCommandOnOpenFoldTakesTheRange(t *testing.T) {
	m := foldModel(t)
	m = send(m, keys("4j")...) // line 4, inside the nested fold 3-5
	m = send(m, keys("zy")...)
	want := "line3\nline4\nline5\n"
	if e := m.regs.Get(0); e.Text != want {
		t.Fatalf("zy inside the nested fold = %q, want %q", e.Text, want)
	}
}

// TestFoldCopyCommandWithoutFoldReports keeps the command honest where there
// is nothing to copy.
func TestFoldCopyCommandWithoutFoldReports(t *testing.T) {
	m := foldModel(t)
	m = send(m, keys("G")...) // line 13, outside every fold
	before := m.regs.Get(0)
	cmd := m.foldCopy()
	if got := noticeIn(t, cmd); !strings.Contains(got, "no fold") {
		t.Errorf("toast = %q, want a 'no fold at the cursor' notice", got)
	}
	if m.regs.Get(0) != before {
		t.Error("a copy with no fold must leave the registers untouched")
	}
}
