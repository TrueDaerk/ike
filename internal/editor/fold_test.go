package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
)

// foldModel builds an editor over 14 numbered lines with folds at 2-7
// (holding a nested fold at 3-5) and 9-11, sized to 10 visible rows.
func foldModel(t *testing.T) Model {
	t.Helper()
	lines := make([]string, 14)
	for i := range lines {
		lines[i] = "line" + itoa(i)
	}
	m := New()
	m.buf = buffer.FromString(strings.Join(lines, "\n"))
	m.path = "main.go"
	m.SetSize(40, 10)
	m.SetFocused(true)
	m.foldLines = m.buf.LineCount()
	m = feedSpans(t, m, highlight.SpansMsg{
		Path: "main.go",
		Folds: []highlight.Fold{
			{HeaderLine: 2, EndLine: 7},
			{HeaderLine: 3, EndLine: 5},
			{HeaderLine: 9, EndLine: 11},
		},
	})
	return m
}

func TestFoldCloseHidesBodyWithPlaceholder(t *testing.T) {
	m := foldModel(t)
	m = send(m, keys("2jzc")...) // cursor to line 2, close fold 2-7
	if e, ok := m.folded[2]; !ok || e != 7 {
		t.Fatalf("folded = %v, want {2: 7}", m.folded)
	}
	m = send(m, keys("kk")...) // move the cursor style off the header row
	rows := strings.Split(m.View(), "\n")
	if !strings.Contains(rows[2], "line2") || !strings.Contains(rows[2], "5 lines") {
		t.Errorf("header row should show line2 + hidden count, got %q", rows[2])
	}
	// Body lines 3-7 are hidden: the next row is line8.
	if !strings.Contains(rows[3], "line8") {
		t.Errorf("row after fold should be line8, got %q", rows[3])
	}
	for _, r := range rows {
		if strings.Contains(r, "line5") {
			t.Errorf("hidden line5 rendered: %q", r)
		}
	}
}

func TestFoldMotionTreatsFoldAsOneRow(t *testing.T) {
	m := foldModel(t)
	m = send(m, keys("2jzc")...) // close 2-7, cursor on header 2
	m = send(m, key('j'))
	if m.cursor.Line != 8 {
		t.Fatalf("j from closed header should land on 8, got %d", m.cursor.Line)
	}
	m = send(m, key('k'))
	if m.cursor.Line != 2 {
		t.Fatalf("k back should land on header 2, got %d", m.cursor.Line)
	}
	if len(m.folded) == 0 {
		t.Fatal("moving across the fold must not open it")
	}
	// A count steps visible rows: 2j from line 1 crosses the fold.
	m = send(m, key('k'), key('2'), key('j'))
	if m.cursor.Line != 8 {
		t.Errorf("2j from line 1 should land on 8, got %d", m.cursor.Line)
	}
}

func TestFoldCloseInsideMovesCursorToHeader(t *testing.T) {
	m := foldModel(t)
	m = send(m, keys("4jzc")...) // cursor line 4: innermost fold is 3-5
	if _, ok := m.folded[3]; !ok || m.cursor.Line != 3 {
		t.Fatalf("zc at line 4: folded=%v cursor=%d, want fold 3 closed, cursor 3", m.folded, m.cursor.Line)
	}
	m = send(m, keys("zc")...) // next level out: 2-7
	if _, ok := m.folded[2]; !ok || m.cursor.Line != 2 {
		t.Fatalf("second zc: folded=%v cursor=%d, want fold 2 closed, cursor 2", m.folded, m.cursor.Line)
	}
}

func TestFoldToggleAndOpen(t *testing.T) {
	m := foldModel(t)
	m = send(m, keys("2jza")...)
	if _, ok := m.folded[2]; !ok {
		t.Fatal("za on an open fold should close it")
	}
	m = send(m, keys("za")...)
	if _, ok := m.folded[2]; ok {
		t.Fatal("za on the closed header should open it")
	}
	m = send(m, keys("zc"+"zo")...)
	if len(m.folded) != 0 {
		t.Fatalf("zo should open the fold, folded=%v", m.folded)
	}
}

func TestFoldCloseAllOpenAll(t *testing.T) {
	m := foldModel(t)
	m = send(m, keys("4j")...) // cursor inside both nested folds
	m = send(m, keys("zM")...)
	if len(m.folded) != 3 {
		t.Fatalf("zM should close all 3 folds, folded=%v", m.folded)
	}
	if m.cursor.Line != 2 {
		t.Errorf("cursor must snap out of hidden body onto header 2, got %d", m.cursor.Line)
	}
	m = send(m, keys("zR")...)
	if len(m.folded) != 0 {
		t.Fatalf("zR should open everything, folded=%v", m.folded)
	}
}

func TestFoldSearchAutoUnfolds(t *testing.T) {
	m := foldModel(t)
	m = send(m, keys("2jzc")...)
	m = typeKeys(m, "/line5")
	m = send(m, special(tea.KeyEnter))
	if m.cursor.Line != 5 {
		t.Fatalf("search should land on line 5, got %d", m.cursor.Line)
	}
	if m.lineHidden(5) {
		t.Error("jumping into a fold must auto-unfold it")
	}
}

func TestFoldEditOnFoldDissolvesIt(t *testing.T) {
	m := foldModel(t)
	m = send(m, keys("2jzc")...)
	m = send(m, key('x')) // edit on the header line
	if _, ok := m.folded[2]; ok {
		t.Fatalf("edit overlapping the fold should dissolve it, folded=%v", m.folded)
	}
}

func TestFoldEditAboveShiftsFold(t *testing.T) {
	m := foldModel(t)
	m = send(m, keys("9jzc")...)              // close 9-11
	m = send(m, key('g'), key('g'), key('O')) // open a line above line 0
	m = send(m, special(tea.KeyEscape))
	if e, ok := m.folded[10]; !ok || e != 12 {
		t.Fatalf("fold should shift with the inserted line, folded=%v want {10: 12}", m.folded)
	}
}

func TestFoldMouseClickMapsThroughFold(t *testing.T) {
	m := foldModel(t)
	m = send(m, keys("2jzc")...)
	m.MouseClick(0, 3) // rows: 0,1,2(fold),8 — row 3 is line 8
	if m.cursor.Line != 8 {
		t.Errorf("click on row 3 should land on line 8, got %d", m.cursor.Line)
	}
}

func TestFoldScrollByTreatsFoldAsOneRow(t *testing.T) {
	m := foldModel(t)
	m = send(m, keys("2jzc")...)
	m.ScrollBy(3) // visible from 0: 0,1,2,8 — three steps land Top on 8
	if m.view.Top != 8 {
		t.Errorf("ScrollBy(3) over a closed fold: Top=%d, want 8", m.view.Top)
	}
}

func TestFoldStateIsPerView(t *testing.T) {
	m := foldModel(t)
	m = send(m, keys("2jzc")...)
	var m2 Model = New()
	m2.SetSize(40, 10)
	m2.ShareDocumentWith(&m)
	if len(m2.folded) != 0 {
		t.Fatalf("a new view starts with no collapsed folds, got %v", m2.folded)
	}
	if len(m2.folds) != 3 {
		t.Fatalf("fold ranges travel with the share, got %v", m2.folds)
	}
	m2 = send(m2, keys("9jzc")...)
	if _, ok := m.folded[9]; ok {
		t.Error("closing a fold in one view must not fold the other view")
	}
	if _, ok := m2.folded[2]; ok {
		t.Error("view 2 must not inherit view 1's collapsed fold")
	}
}

func TestFoldReconcileDropsVanishedFold(t *testing.T) {
	m := foldModel(t)
	m = send(m, keys("9jzc")...)
	// A fresh parse no longer contains a fold at header 9 — the collapsed
	// fold must dissolve rather than hide the wrong lines.
	m = feedSpans(t, m, highlight.SpansMsg{
		Path:    "main.go",
		Version: m.docVersion,
		Folds:   []highlight.Fold{{HeaderLine: 2, EndLine: 7}},
	})
	if len(m.folded) != 0 {
		t.Fatalf("vanished fold must dissolve on reconcile, folded=%v", m.folded)
	}
}
