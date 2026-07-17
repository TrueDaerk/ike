package debugpanel

import (
	"testing"
	"time"

	"ike/internal/dap"
)

// mouseModel builds a sized panel with frames + a small variables tree and an
// injectable clock, shared by the mouse tests (#639).
func mouseModel(t *testing.T) (*Model, *time.Time) {
	t.Helper()
	m := New(nil)
	m.SetSize(80, 4) // interior 80x4: title row + bodyHeight 3
	m.SetFrames(frames())
	m.SetScopes([]dap.Scope{{Name: "Locals", VariablesReference: 100}})
	m.SetChildren(100, []dap.Variable{
		{Name: "x", Value: "42"},
		{Name: "y", Value: "7"},
		{Name: "z", Value: "9"},
	})
	clk := time.Unix(0, 0)
	m.now = func() time.Time { return clk }
	return &m, &clk
}

// TestClickBottomBorderNoop guards the #639 off-by-one: layout.Hit counts the
// bottom border as part of the pane, so a border click arrives with y equal to
// the interior height — one past the last visible row. With enough rows the
// plain length guard would accept it; the click must be rejected instead.
func TestClickBottomBorderNoop(t *testing.T) {
	m, _ := mouseModel(t)
	// h=4 → the bottom border arrives as y == 4; frameTop 0 + (4-1) = 3 == len
	// (guarded), so shrink so the mapped row is in range: frameTop 0, 3 frames,
	// h=3 → y==3 maps to row 2 which exists.
	m.SetSize(80, 3)
	m.frameSel, m.col = 0, colFrames
	if cmd := m.Click(4, 3); cmd != nil {
		t.Fatal("bottom-border click must not activate")
	}
	if m.frameSel != 0 {
		t.Fatalf("frameSel = %d, bottom-border click must not select", m.frameSel)
	}
	// And it never arms a double-click for a following interior click.
	if cmd := m.Click(4, 3); cmd != nil {
		t.Fatal("repeated border clicks must stay no-ops")
	}
}

// TestClickLeftBorderNoop: the left border/padding arrive as x < 0, which used
// to map onto the frames column; they must be rejected.
func TestClickLeftBorderNoop(t *testing.T) {
	m, _ := mouseModel(t)
	m.col = colVars
	if cmd := m.Click(-2, 1); cmd != nil {
		t.Fatal("left-border click must not activate")
	}
	if m.col != colVars || m.frameSel != 0 {
		t.Fatalf("col=%d frameSel=%d, left-border click must not focus/select", m.col, m.frameSel)
	}
	// Same for x past the interior width (right padding/border).
	if cmd := m.Click(m.w, 1); cmd != nil {
		t.Fatal("right-border click must not activate")
	}
}

// TestBorderClickVoidsPendingDoubleClick: a border click between two clicks on
// the same row is an intervening click — the pair must not double-click.
func TestBorderClickVoidsPendingDoubleClick(t *testing.T) {
	m, clk := mouseModel(t)
	m.Click(4, 2)
	*clk = clk.Add(100 * time.Millisecond)
	m.Click(-2, 2) // border
	*clk = clk.Add(100 * time.Millisecond)
	if cmd := m.Click(4, 2); cmd != nil {
		t.Fatal("border click must void the pending double-click")
	}
}

// TestDoubleClickExpires: two clicks on the same row further apart than the
// 400ms window must not activate.
func TestDoubleClickExpires(t *testing.T) {
	m, clk := mouseModel(t)
	m.Click(4, 2)
	*clk = clk.Add(doubleClickWindow + time.Millisecond)
	if cmd := m.Click(4, 2); cmd != nil {
		t.Fatal("clicks beyond the double-click window must not activate")
	}
	// A third click right after the second (within the window) completes one.
	*clk = clk.Add(100 * time.Millisecond)
	if cmd := m.Click(4, 2); cmd == nil {
		t.Fatal("the second and third click form a valid double-click")
	}
}

// TestCrossColumnClickResetsDoubleClick guards #639's stale-tracker defect:
// frames-click → output-click → frames-click within the window must not count
// as a double-click on the frame row.
func TestCrossColumnClickResetsDoubleClick(t *testing.T) {
	m, clk := mouseModel(t)
	m.Click(4, 2) // frames row 1
	*clk = clk.Add(100 * time.Millisecond)
	m.Click(70, 2) // output column: focus only, but it must record
	if m.col != colOutput {
		t.Fatalf("col = %d, output click must focus the output column", m.col)
	}
	*clk = clk.Add(100 * time.Millisecond)
	if cmd := m.Click(4, 2); cmd != nil {
		t.Fatal("an intervening output click must reset the double-click state")
	}
}

// TestClickWhileEditingCancelsEdit: mouse input while the inline value editor
// is open cancels the edit before selection moves, so the editor never renders
// on a different row with the stale buffer (#627/#639).
func TestClickWhileEditingCancelsEdit(t *testing.T) {
	m, _ := mouseModel(t)
	m.SetSize(80, 10)
	m.SetEditable(true)
	m.col = colVars
	m.varSel = 1 // "x"
	m.Update(key("e"))
	if !m.Editing() {
		t.Fatal("precondition: editor open")
	}
	if cmd := m.Click(40, 3); cmd != nil { // click "y" (row 2) in the vars column
		t.Fatal("a first click while editing must not activate")
	}
	if m.Editing() {
		t.Fatal("a click while editing must cancel the edit")
	}
	if m.varSel != 2 {
		t.Fatalf("varSel = %d, the click still selects normally after cancel", m.varSel)
	}
}

// TestWheelWhileEditingKeepsSelection: the wheel scrolls but never moves the
// selection while the editor is open — moving varSel would re-anchor the
// editor onto a different row.
func TestWheelWhileEditingKeepsSelection(t *testing.T) {
	m, _ := mouseModel(t)
	m.SetEditable(true)
	m.col = colVars
	m.varSel = 1
	m.Update(key("e"))
	if !m.Editing() {
		t.Fatal("precondition: editor open")
	}
	m.Wheel(1) // 4 rows, bodyHeight 3 → varTop 1
	if m.varTop != 1 {
		t.Fatalf("varTop = %d, wheel must still scroll while editing", m.varTop)
	}
	if m.varSel != 1 || !m.Editing() {
		t.Fatalf("varSel=%d editing=%v, wheel while editing must not move the selection", m.varSel, m.Editing())
	}
}

// TestWheelClampsSelectionIntoView: like the vcs panel, wheeling drags the
// selection along so it stays inside the visible window (#639).
func TestWheelClampsSelectionIntoView(t *testing.T) {
	m, _ := mouseModel(t)
	// Frames: 3 rows, bodyHeight 3 at h=4 → shrink to force scrolling.
	m.SetSize(80, 3) // bodyHeight 2
	m.col = colFrames
	m.frameSel = 0
	m.Wheel(1)
	if m.frameTop != 1 || m.frameSel != 1 {
		t.Fatalf("frames top/sel = %d/%d, want 1/1 (selection pulled into view)", m.frameTop, m.frameSel)
	}
	// Vars: 4 rows (Locals + 3 children), bodyHeight 2.
	m.col = colVars
	m.varSel = 0
	m.Wheel(2)
	if m.varTop != 2 || m.varSel != 2 {
		t.Fatalf("vars top/sel = %d/%d, want 2/2", m.varTop, m.varSel)
	}
	m.Wheel(-2)
	if m.varTop != 0 || m.varSel != 1 {
		t.Fatalf("vars top/sel = %d/%d after wheel-up, want 0/1", m.varTop, m.varSel)
	}
}
