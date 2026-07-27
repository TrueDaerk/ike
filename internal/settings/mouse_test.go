package settings

import (
	"strings"
	"testing"
)

func mouseModel(t *testing.T) *Model {
	t.Helper()
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	return m
}

// TestHoverHighlightsRows guards #885: motion over the rail and form marks
// the hovered row; leaving the body clears it.
func TestHoverHighlightsRows(t *testing.T) {
	m := mouseModel(t)
	m.Hover(2, 3) // rail, second visible row
	if m.hoverCat != 1 {
		t.Fatalf("hoverCat = %d, want 1", m.hoverCat)
	}
	m.Hover(1+catWidth+4, 3) // form, second row
	if m.hoverRow != 1 || m.hoverCat != -1 {
		t.Fatalf("hover = cat %d row %d, want row 1", m.hoverCat, m.hoverRow)
	}
	m.Hover(0, 0)
	if m.hoverCat != -1 || m.hoverRow != -1 {
		t.Fatal("hover must clear outside the body")
	}
}

// TestDetailColumnClickFocusesEditor guards #1295: a press in the detail
// column moves the focus to the typed editor rendered there.
func TestDetailColumnClickFocusesEditor(t *testing.T) {
	m := mouseModel(t)
	m.focus = formColumn
	m.syncEditor()
	g := m.gridFor()
	if !g.side {
		t.Skip("panel too narrow for the three-column grid")
	}
	m.Click(m.detailX()+1, 3)
	if m.focus != detailColumn {
		t.Fatalf("focus = %v, want the detail column", m.focus)
	}
}

// TestScopeChipClickCycles guards #885: the always-visible chip cycles the
// write scope on click.
func TestScopeChipClickCycles(t *testing.T) {
	m := mouseModel(t)
	if !strings.Contains(m.View(), "scope: auto") {
		t.Fatal("the scope chip must always render")
	}
	m.Click(m.chipSpan.start+1, 1)
	if m.writeScope != scopeUser {
		t.Fatalf("writeScope = %v, want user", m.writeScope)
	}
	m.Click(m.chipSpan.start+1, 1)
	m.Click(m.chipSpan.start+1, 1)
	if m.writeScope != scopeAuto {
		t.Fatalf("writeScope = %v, want auto after full cycle", m.writeScope)
	}
}

// TestHintRowClickRunsAction guards #885: pressing "s scope" on the hint row
// cycles the scope; dead hint cells swallow the press.
func TestHintRowClickRunsAction(t *testing.T) {
	m := mouseModel(t)
	m.View() // computes the hint spans
	var scopeHit hintAction
	for _, h := range m.hintHits {
		if h.action == "filter" {
			scopeHit = h
		}
	}
	if scopeHit.end == 0 {
		t.Fatal("hint spans must include the search action")
	}
	m.Click(scopeHit.start, m.hintRowY())
	if !m.filtering {
		t.Fatal("hint click must start the search")
	}
	if m.Click(0, m.hintRowY()) != nil || !m.IsOpen() {
		t.Fatal("dead hint cells must swallow the press")
	}
}

// TestWheelScrollsViewportNotSelection guards #885: with more categories than
// window rows, the wheel moves catOff and leaves the selection alone.
func TestWheelScrollsViewportNotSelection(t *testing.T) {
	restoreConfig(t)
	pages := testPages()
	for _, title := range []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8", "P9"} {
		pages = append(pages, Page{Title: title, Entries: []Entry{{Key: "ui.menu_bar", Title: "X", Type: Bool}}})
	}
	m := New(pages, testOpts(t))
	m.SetSize(90, 10) // 6 body rows for 11+ categories
	m.Open()
	m.View()
	m.Wheel(2, 3)
	if m.cat != 0 {
		t.Fatalf("wheel must not move the selection, cat=%d", m.cat)
	}
	if m.catOff != 1 {
		t.Fatalf("rail wheels one category per notch, off=%d", m.catOff)
	}
	// The next render must not snap back to the selection.
	m.View()
	if m.catOff != 1 {
		t.Fatalf("render must keep the wheeled offset, off=%d", m.catOff)
	}
	// Moving the selection re-follows.
	m.move(1)
	m.View()
	if m.catOff > 1 {
		t.Fatalf("selection move must re-follow, off=%d", m.catOff)
	}
}

// TestSettingsRowsMapOneToOne guards #1295: nothing expands inline any more,
// so the row under the pointer is the row at that line — even with an editor
// open in the detail column.
func TestSettingsRowsMapOneToOne(t *testing.T) {
	m := mouseModel(t)
	m.focus = formColumn
	m.sel = 0
	m.activate() // opens the editor in the detail column
	m.focus = formColumn
	m.Click(formX()+1, 2+1)
	if m.sel != 1 {
		t.Fatalf("sel = %d, want the second row", m.sel)
	}
}
