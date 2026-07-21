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

// TestHoverMovesPickerHighlight: hovering an open enum picker follows the
// pointer like the menu dropdown.
func TestHoverMovesPickerHighlight(t *testing.T) {
	m := mouseModel(t)
	m.focus = formColumn
	// Find an enum row on the first page.
	found := false
	for i, r := range m.rows() {
		if r.entry.Type == Enum && len(r.entry.Options) >= 2 {
			m.sel, found = i, true
			break
		}
	}
	if !found {
		t.Skip("no enum row in the test pages")
	}
	m.activate()
	if !m.picking {
		t.Fatal("setup: picker must open")
	}
	m.Hover(1+catWidth+4, 2+m.sel-m.formOff+2) // second option line
	if m.pickIdx != 1 {
		t.Fatalf("pickIdx = %d, want 1", m.pickIdx)
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
		if h.action == "scope" {
			scopeHit = h
		}
	}
	if scopeHit.end == 0 {
		t.Fatal("hint spans must include the scope action")
	}
	m.Click(scopeHit.start, m.hintRowY())
	if m.writeScope != scopeUser {
		t.Fatalf("hint click must cycle the scope, got %v", m.writeScope)
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

// TestPathSuggestionClickCompletes guards #885: clicking a rendered
// completion suggestion takes it instead of cancelling the edit.
func TestPathSuggestionClickCompletes(t *testing.T) {
	m := mouseModel(t)
	m.focus = formColumn
	found := false
	for i, r := range m.rows() {
		if r.entry.Type == Path {
			m.sel, found = i, true
			break
		}
	}
	if !found {
		t.Skip("no path row in the test pages")
	}
	m.activate()
	if !m.editing {
		t.Fatal("setup: edit must open")
	}
	m.suggest.candidates = []string{"/tmp/sugg-a", "/tmp/sugg-b"}
	m.Click(1+catWidth+4, 2+(m.sel-m.formOff)+2) // second suggestion line
	if !m.editing || m.input != "/tmp/sugg-b" {
		t.Fatalf("suggestion click: editing=%v input=%q", m.editing, m.input)
	}
}
