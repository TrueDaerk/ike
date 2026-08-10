package editor

import (
	"strings"
	"testing"

	"ike/internal/editor/operator"
)

// closedFoldModel is foldModel with the 2-7 fold collapsed and the cursor
// parked back on its header row — the state "one visible row hides six lines".
func closedFoldModel(t *testing.T) Model {
	t.Helper()
	m := foldModel(t)
	m = send(m, keys("2jzc")...) // cursor to line 2, close the fold 2-7
	if e, ok := m.folded[2]; !ok || e != 7 {
		t.Fatalf("folded = %v, want {2: 7}", m.folded)
	}
	if m.cursor.Line != 2 {
		t.Fatalf("cursor line = %d, want the fold header 2", m.cursor.Line)
	}
	return m
}

// foldedText is the body of the 2-7 fold as the register should hold it.
const foldedText = "line2\nline3\nline4\nline5\nline6\nline7\n"

func TestYankOnClosedFoldTakesWholeFold(t *testing.T) {
	m := closedFoldModel(t)
	m = send(m, keys("yy")...)
	e := m.regs.Get(0)
	if !e.Linewise {
		t.Fatalf("yank over a fold must stay linewise, got %+v", e)
	}
	if e.Text != foldedText {
		t.Fatalf("yy on a closed fold = %q, want %q", e.Text, foldedText)
	}
}

func TestVisualLineYankOnClosedFoldTakesWholeFold(t *testing.T) {
	m := closedFoldModel(t)
	m = send(m, keys("Vy")...)
	if e := m.regs.Get(0); e.Text != foldedText || !e.Linewise {
		t.Fatalf("V+y on a closed fold = %+v, want %q linewise", e, foldedText)
	}
}

// TestYankOnOpenFoldHeaderUnchanged is the counter-case of the acceptance
// criteria: an *open* fold's header is an ordinary line.
func TestYankOnOpenFoldHeaderUnchanged(t *testing.T) {
	m := foldModel(t)
	m = send(m, keys("2jyy")...)
	if e := m.regs.Get(0); e.Text != "line2\n" {
		t.Fatalf("yy on an open fold header = %q, want %q", e.Text, "line2\n")
	}
}

// TestCharwiseYankOnFoldHeaderUnchanged guards the other half: a charwise
// selection inside the visible header line never means "the whole fold".
func TestCharwiseYankOnFoldHeaderUnchanged(t *testing.T) {
	m := closedFoldModel(t)
	m = send(m, keys("vlly")...) // three runes of "line2"
	e := m.regs.Get(0)
	if e.Linewise || e.Text != "lin" {
		t.Fatalf("charwise yank on a fold header = %+v, want %q charwise", e, "lin")
	}
}

// TestVisualLineYankOverSeveralFoldsTakesMaxEnd covers a selection covering
// more than one collapsed header: it grows to the largest fold end.
func TestVisualLineYankOverSeveralFoldsTakesMaxEnd(t *testing.T) {
	m := closedFoldModel(t)
	m.folded[9] = 11 // close the second fold too, without moving the cursor
	// V on line 2, then two fold-aware j: line 2 → line 8 → line 9.
	m = send(m, keys("Vjj")...)
	if m.cursor.Line != 9 {
		t.Fatalf("cursor line = %d, want 9 (two visible rows below the fold)", m.cursor.Line)
	}
	m = send(m, keys("y")...)
	want := ""
	for i := 2; i <= 11; i++ {
		want += "line" + itoa(i) + "\n"
	}
	if e := m.regs.Get(0); e.Text != want {
		t.Fatalf("yank over two closed folds = %q, want %q", e.Text, want)
	}
}

// TestClipboardCopyOnClosedFoldCopiesWholeFold is the Cmd+C acceptance
// criterion, including the toast's line count (#252).
func TestClipboardCopyOnClosedFoldCopiesWholeFold(t *testing.T) {
	m := closedFoldModel(t)
	clip := &failClipboard{}
	m.SetClipboard(clip)

	m, cmd := m.runAction("copy")
	if e := m.regs.Get('+'); e.Text != foldedText || !e.Linewise {
		t.Fatalf("Cmd+C on a closed fold = %+v, want %q linewise", e, foldedText)
	}
	if clip.lastGot != foldedText {
		t.Fatalf("system clipboard got %q, want %q", clip.lastGot, foldedText)
	}
	if cmd == nil {
		t.Fatal("copy should report a toast")
	}
	n, ok := cmd().(NoticeMsg)
	if !ok || !strings.Contains(n.Text, "copied 6 lines") {
		t.Fatalf("copy toast = %+v, want \"copied 6 lines\"", cmd())
	}
}

// TestClipboardCutOnClosedFoldRemovesWholeFold: half-deleting a collapsed fold
// would leave hidden remnants, so Cmd+X expands like the yank does.
func TestClipboardCutOnClosedFoldRemovesWholeFold(t *testing.T) {
	m := closedFoldModel(t)
	m.SetClipboard(&failClipboard{})
	m, _ = m.runAction("cut")
	if e := m.regs.Get('+'); e.Text != foldedText {
		t.Fatalf("Cmd+X on a closed fold = %+v, want %q", e, foldedText)
	}
	if m.buf.LineCount() != 8 {
		t.Fatalf("line count = %d, want 8 (14 - 6)", m.buf.LineCount())
	}
	if line(m, 2) != "line8" {
		t.Fatalf("line 2 after the cut = %q, want %q", line(m, 2), "line8")
	}
}

func TestDeleteOnClosedFoldRemovesWholeFold(t *testing.T) {
	m := closedFoldModel(t)
	m = send(m, keys("dd")...)
	if m.buf.LineCount() != 8 {
		t.Fatalf("line count = %d, want 8 (14 - 6)", m.buf.LineCount())
	}
	if line(m, 2) != "line8" {
		t.Fatalf("line 2 after dd = %q, want %q", line(m, 2), "line8")
	}
	if e := m.regs.Get(0); e.Text != foldedText {
		t.Fatalf("dd register = %+v, want %q", e, foldedText)
	}
}

func TestChangeOnClosedFoldReplacesWholeFold(t *testing.T) {
	m := closedFoldModel(t)
	m = send(m, keys("cc")...)
	if m.buf.LineCount() != 9 {
		t.Fatalf("line count = %d, want 9 (14 - 6 + the empty line cc keeps)", m.buf.LineCount())
	}
	if line(m, 2) != "" {
		t.Fatalf("line 2 after cc = %q, want the empty replacement line", line(m, 2))
	}
	if line(m, 3) != "line8" {
		t.Fatalf("line 3 after cc = %q, want %q", line(m, 3), "line8")
	}
}

func TestExYankOnClosedFoldTakesWholeFold(t *testing.T) {
	m := closedFoldModel(t)
	m = runEx(m, "y")
	if e := m.regs.Get(0); e.Text != foldedText {
		t.Fatalf(":y on a closed fold = %+v, want %q", e, foldedText)
	}
}

// TestExDeleteOnClosedFoldRemovesWholeFold covers the ex range form: the range
// names line 3 only, but line 3 is the header of the nested 3-5 fold.
func TestExDeleteOnClosedFoldRemovesWholeFold(t *testing.T) {
	m := foldModel(t)
	m = send(m, keys("3jzc")...) // close the nested fold 3-5
	if e, ok := m.folded[3]; !ok || e != 5 {
		t.Fatalf("folded = %v, want {3: 5}", m.folded)
	}
	m = runEx(m, "4d") // 1-based: line index 3
	if m.buf.LineCount() != 11 {
		t.Fatalf("line count = %d, want 11 (14 - 3)", m.buf.LineCount())
	}
	if line(m, 3) != "line6" {
		t.Fatalf("line 3 after :4d = %q, want %q", line(m, 3), "line6")
	}
}

// TestPasteOverClosedFoldReplacesWholeFold: the visual put deletes the
// selection first, so it expands with it.
func TestPasteOverClosedFoldReplacesWholeFold(t *testing.T) {
	m := closedFoldModel(t)
	m = send(m, keys("Gyy")...)    // yank the last line into the unnamed register
	m = send(m, keys("gg2jVp")...) // select the (still collapsed) fold and put over it
	if m.buf.LineCount() != 9 {
		t.Fatalf("line count = %d, want 9 (14 - 6 + 1)", m.buf.LineCount())
	}
	if line(m, 2) != "line13" {
		t.Fatalf("line 2 after the put = %q, want %q", line(m, 2), "line13")
	}
	if line(m, 3) != "line8" {
		t.Fatalf("line 3 after the put = %q, want %q", line(m, 3), "line8")
	}
}

// TestExpandFoldTargetIsIdempotentWithoutFolds guards the fast path: with no
// collapsed fold the target is handed on untouched.
func TestExpandFoldTargetIsIdempotentWithoutFolds(t *testing.T) {
	m := foldModel(t)
	in := operator.LineTarget(1, 3)
	if got := m.expandFoldTarget(in); got != in {
		t.Fatalf("expandFoldTarget without folds = %+v, want %+v", got, in)
	}
}
