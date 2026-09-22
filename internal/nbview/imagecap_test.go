package nbview

import "testing"

// imagecap_test.go covers the notebook.image_max_cols placement cap (#2683):
// an image output is fitted into the smaller of the pane width and the cap,
// 0 lifts the cap back to the pane width, and a change re-renders the pane.

// placedCols returns the columns the fixture's single image output occupies,
// 0 when nothing is placed.
func placedCols(m *Model) int {
	for _, im := range m.images {
		if im.Cols > 0 {
			return im.Cols
		}
	}
	return 0
}

// TestImageMaxColsCapsPlacement (#2683): on a 200-column pane the default cap
// keeps the picture at 80 columns, while 0 lets it use the full width.
func TestImageMaxColsCapsPlacement(t *testing.T) {
	m := newModel(t, writeFixture(t))
	// A tall pane, so the fit is width-bound rather than height-bound. The
	// placement budget is the pane width minus the cell gutter.
	m.SetSize(200, 200)
	m.SetGraphics(true)
	full := 200 - m.gutterWidth()
	if got := placedCols(&m); got != full {
		t.Fatalf("uncapped placement = %d columns, want the full body width %d", got, full)
	}
	m.SetImageMaxCols(80)
	if got := placedCols(&m); got != 80 {
		t.Fatalf("capped placement = %d columns, want 80", got)
	}
	// The aspect ratio still comes from FitGrid: the 8x4 fixture is 4 columns
	// per row, so 80 columns are 20 rows.
	for _, im := range m.images {
		if im.Cols == 80 && im.Rows != 20 {
			t.Fatalf("rows = %d for an 80-column placement, want the aspect-preserving 20", im.Rows)
		}
	}
	// 0 lifts the cap again, and the pane re-renders with the wider picture.
	m.SetImageMaxCols(0)
	if got := placedCols(&m); got != full {
		t.Fatalf("placement after lifting the cap = %d columns, want %d", got, full)
	}
}

// TestImageMaxColsBelowPaneWidthOnly (#2683): a cap wider than the pane never
// stretches the picture past the pane, and a negative value reads as no cap.
func TestImageMaxColsBelowPaneWidthOnly(t *testing.T) {
	m := newModel(t, writeFixture(t))
	m.SetSize(60, 200)
	m.SetGraphics(true)
	full := 60 - m.gutterWidth()
	m.SetImageMaxCols(400)
	if got := placedCols(&m); got != full {
		t.Fatalf("placement = %d columns, want the body width %d", got, full)
	}
	m.SetImageMaxCols(-5)
	if m.imgMaxCols != 0 {
		t.Fatalf("imgMaxCols = %d for a negative cap, want 0 (no cap)", m.imgMaxCols)
	}
	if got := placedCols(&m); got != full {
		t.Fatalf("placement = %d columns, want the body width %d", got, full)
	}
}

// TestImageMaxColsWithoutGraphics (#2683): with no Kitty support the cap
// changes nothing — the output stays its metadata label.
func TestImageMaxColsWithoutGraphics(t *testing.T) {
	m := newModel(t, writeFixture(t))
	m.SetSize(200, 200)
	m.SetImageMaxCols(40)
	if ids := m.ImageIDs(); len(ids) != 0 {
		t.Fatalf("placed %d images without graphics support", len(ids))
	}
}
