package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/sv"
)

// svMotionDoc has three rows whose fields differ in length plus a short row
// with a single field, so a raw-column vertical motion lands in a different
// table column on every step.
//
//	L0 "id,name,note"  f0[0,2]  f1[3,7]  f2[8,12]
//	L1 "1,alpha,x"     f0[0,1]  f1[2,7]  f2[8,9]
//	L2 "22222,bb,yyyy" f0[0,5]  f1[6,8]  f2[9,13]
//	L3 "z"             f0[0,1]
const svMotionDoc = "id,name,note\n1,alpha,x\n22222,bb,yyyy\nz\n"

// svFieldAt is the field index the caret sits in on its line.
func svFieldAt(m Model) int {
	return sv.IndexAt(m.buf.Line(m.cursor.Line), ',', m.cursor.Col)
}

// svPlace puts the caret at col and makes it the remembered column, like any
// horizontal motion would.
func svPlace(m Model, line, col int) Model {
	m.cursor = buffer.Position{Line: line, Col: col}
	m.desiredCol = col
	m.svWant = svWant{}
	return m
}

// TestSVVerticalKeepsTableColumn is the core of #1744: j/k stay in the same
// field, and the offset inside it survives a row whose field is too short to
// hold it — the sv analogue of desiredCol across short lines.
func TestSVVerticalKeepsTableColumn(t *testing.T) {
	m := csvLoaded(t, svMotionDoc)
	m = svPlace(m, 0, 9) // field 2 ("note"), offset 1

	m = typeKeys(m, "j")
	// L1 field 2 is the single rune "x": the offset clamps to the field end,
	// which normal mode then clamps to the last rune.
	if m.cursor.Col != 8 || svFieldAt(m) != 2 {
		t.Fatalf("after j: col %d field %d, want col 8 field 2", m.cursor.Col, svFieldAt(m))
	}
	m = typeKeys(m, "j")
	// L2 field 2 is wide again, so the original offset 1 comes back: 9+1.
	if m.cursor.Col != 10 || svFieldAt(m) != 2 {
		t.Fatalf("after jj: col %d field %d, want col 10 field 2", m.cursor.Col, svFieldAt(m))
	}
	m = typeKeys(m, "k")
	if m.cursor.Col != 8 || svFieldAt(m) != 2 {
		t.Fatalf("after jjk: col %d field %d, want col 8 field 2", m.cursor.Col, svFieldAt(m))
	}
}

// TestSVVerticalClampsToLastField: a row with fewer fields clamps to its last
// one without panicking, and leaving it restores the wanted field.
func TestSVVerticalClampsToLastField(t *testing.T) {
	m := csvLoaded(t, svMotionDoc)
	m = svPlace(m, 2, 10) // field 2, offset 1

	m = typeKeys(m, "j") // L3 has one field only
	if m.cursor.Line != 3 || m.cursor.Col != 0 {
		t.Fatalf("after j: %v, want line 3 col 0", m.cursor)
	}
	m = typeKeys(m, "k")
	if m.cursor.Col != 10 || svFieldAt(m) != 2 {
		t.Fatalf("after jk: col %d field %d, want col 10 field 2", m.cursor.Col, svFieldAt(m))
	}
}

// TestSVVerticalHorizontalMotionResetsWant: a horizontal step re-aims the
// column, exactly like it re-aims desiredCol.
func TestSVVerticalHorizontalMotionResetsWant(t *testing.T) {
	m := csvLoaded(t, svMotionDoc)
	m = svPlace(m, 0, 9) // field 2

	m = typeKeys(m, "hhh") // col 6 — inside field 1 ("name")
	if m.cursor.Col != 6 {
		t.Fatalf("after hhh: col %d, want 6", m.cursor.Col)
	}
	m = typeKeys(m, "j") // L1 field 1 is [2,7): offset 3 lands on col 5
	if m.cursor.Col != 5 || svFieldAt(m) != 1 {
		t.Fatalf("after j: col %d field %d, want col 5 field 1", m.cursor.Col, svFieldAt(m))
	}
}

// TestSVVerticalDisabledKeepsRawColumn: with the table rendering off the raw
// desiredCol applies again, unchanged from before #1744.
func TestSVVerticalDisabledKeepsRawColumn(t *testing.T) {
	on := csvLoaded(t, svMotionDoc)
	on = svPlace(on, 0, 3) // field 1, offset 0
	on = typeKeys(on, "j")
	if on.cursor.Col != 2 || svFieldAt(on) != 1 {
		t.Fatalf("rendering on: col %d field %d, want col 2 field 1", on.cursor.Col, svFieldAt(on))
	}

	off := csvLoaded(t, svMotionDoc)
	off.svRender = false
	off = svPlace(off, 0, 3)
	off = typeKeys(off, "j")
	if off.cursor.Col != 3 {
		t.Fatalf("rendering off: col %d, want the raw column 3", off.cursor.Col)
	}
}

// TestSVVerticalNonSVBuffer: a plain text buffer keeps the raw column even
// though the toggle is on.
func TestSVVerticalNonSVBuffer(t *testing.T) {
	m, _ := loaded(t, "id,name,note\n1,alpha,x\n")
	m = svPlace(m, 0, 3)
	m = typeKeys(m, "j")
	if m.cursor.Col != 3 {
		t.Fatalf("plain buffer: col %d, want the raw column 3", m.cursor.Col)
	}
}

// TestSVVerticalVisualMode: the selection's moving end is column-true too.
func TestSVVerticalVisualMode(t *testing.T) {
	m := csvLoaded(t, svMotionDoc)
	m = svPlace(m, 0, 3) // field 1, offset 0
	m = typeKeys(m, "vj")
	if m.mode != Visual {
		t.Fatalf("mode %v, want Visual", m.mode)
	}
	// L1 field 1 starts at 2; the raw column 3 would sit one rune further in.
	if m.cursor.Col != 2 || svFieldAt(m) != 1 {
		t.Fatalf("visual j: col %d field %d, want col 2 field 1", m.cursor.Col, svFieldAt(m))
	}
}

// TestSVVerticalInsertMode: the insert-mode arrow keys follow the table column
// as well (#1687 semantics, sv-aware).
func TestSVVerticalInsertMode(t *testing.T) {
	m := csvLoaded(t, svMotionDoc)
	m = svPlace(m, 0, 9)
	m = typeKeys(m, "i")
	m = send(m, special(tea.KeyDown))
	// Insert mode allows the one-past-end column, so the offset only clamps
	// to the field end (9), not to the last rune.
	if m.cursor.Col != 9 || sv.IndexAt(m.buf.Line(1), ',', m.cursor.Col) != 2 {
		t.Fatalf("insert down: col %d, want col 9 in field 2", m.cursor.Col)
	}
	m = send(m, special(tea.KeyDown))
	if m.cursor.Col != 10 || svFieldAt(m) != 2 {
		t.Fatalf("insert down twice: col %d field %d, want col 10 field 2", m.cursor.Col, svFieldAt(m))
	}
}
