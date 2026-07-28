package hltest

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/highlight"
	"ike/internal/httppane"
	"ike/internal/theme"
)

// foldBody is a JSON response body whose pretty-printed form has an outer
// object and a nested one, so the viewer sees two fold ranges.
const foldBody = `{"name":"x","nested":{"a":1,"b":2},"tail":3}`

func foldViewer(t *testing.T) *httppane.Model {
	t.Helper()
	if !highlight.FencedSupported("json") {
		t.Skip("no JSON grammar in this build — nothing to fold")
	}
	m := httppane.New(theme.DefaultPalette())
	// Tall enough that the whole response fits: the folding assertions are
	// about what the projection hides, not about scrolling.
	m.SetSize(80, 30)
	m.Set("create", resp("application/json", foldBody))
	return &m
}

// rowWith returns the index of the first row whose text contains want.
func rowWith(t *testing.T, m *httppane.Model, want string) int {
	t.Helper()
	for i := 0; i < m.Rows(); i++ {
		if strings.Contains(m.RowText(i), want) {
			return i
		}
	}
	t.Fatalf("no row containing %q", want)
	return -1
}

// TestBodyIsFoldable guards #1330: the response body exposes its own
// language's fold ranges — the outer object and the nested one.
func TestBodyIsFoldable(t *testing.T) {
	m := foldViewer(t)
	for _, want := range []string{"{", `"nested"`} {
		if row := rowWith(t, m, want); !m.Foldable(row) {
			t.Errorf("row %d (%q) should be foldable", row, m.RowText(row))
		}
	}
	if row := rowWith(t, m, `"name"`); m.Foldable(row) {
		t.Errorf("a plain member row must not be foldable: %q", m.RowText(row))
	}
}

// TestCollapseHidesRowsAndCounts guards the projection and the placeholder.
func TestCollapseHidesRowsAndCounts(t *testing.T) {
	m := foldViewer(t)
	row := rowWith(t, m, `"nested"`)
	hidden := rowWith(t, m, `"a": 1`)
	before := m.VisibleCount()

	if !m.ToggleFold(row) {
		t.Fatal("toggling a foldable row must collapse it")
	}
	end, ok := m.FoldedAt(row)
	if !ok {
		t.Fatal("the fold must be recorded as collapsed")
	}
	if got, want := m.VisibleCount(), before-(end-row); got != want {
		t.Errorf("visible rows = %d, want %d", got, want)
	}
	if m.RowVisible(hidden) {
		t.Error("a collapsed fold's body rows must leave the projection")
	}
	if view := m.View(); !strings.Contains(view, "⋯ 3 lines") {
		t.Errorf("placeholder with the hidden-line count missing:\n%s", view)
	}

	m.ToggleFold(row)
	if !m.RowVisible(hidden) || m.VisibleCount() != before {
		t.Errorf("expanding must restore every row: %d visible, want %d", m.VisibleCount(), before)
	}
	if strings.Contains(m.View(), "⋯ ") {
		t.Error("no placeholder may survive the expand")
	}
}

// TestFoldKeys drives the keyboard path: zM collapses everything, zR opens it
// again, and za toggles the fold at the top of the view.
func TestFoldKeys(t *testing.T) {
	m := foldViewer(t)
	full := m.VisibleCount()

	typeKeys(m, "z", "M")
	if m.VisibleCount() >= full {
		t.Fatalf("zM must hide rows: %d visible of %d", m.VisibleCount(), full)
	}
	typeKeys(m, "z", "R")
	if m.VisibleCount() != full {
		t.Fatalf("zR must restore every row: %d of %d", m.VisibleCount(), full)
	}

	outer := rowWith(t, m, "{")
	typeKeys(m, "z", "a") // the fold at the top of the view is the body object
	if _, ok := m.FoldedAt(outer); !ok {
		t.Fatalf("za should collapse the fold at the top of the view (row %d)", outer)
	}
	typeKeys(m, "z", "a")
	if _, ok := m.FoldedAt(outer); ok {
		t.Error("za should reopen it")
	}
	// zc/zo are the explicit halves of the same target.
	typeKeys(m, "z", "c")
	if _, ok := m.FoldedAt(outer); !ok {
		t.Error("zc should collapse the target fold")
	}
	typeKeys(m, "z", "o")
	if _, ok := m.FoldedAt(outer); ok {
		t.Error("zo should open the target fold")
	}
}

// TestGutterClickToggles guards the mouse path: a press in the marker column
// folds instead of starting a selection.
func TestGutterClickToggles(t *testing.T) {
	m := foldViewer(t)
	row := rowWith(t, m, `"nested"`)
	y := m.DisplayRow(row) + 1 // the title line occupies y == 0

	m.MousePress(0, y)
	if _, ok := m.FoldedAt(row); !ok {
		t.Fatal("a gutter click must collapse the fold")
	}
	if m.SelectionText() != "" {
		t.Error("a gutter click must not start a selection")
	}
	m.MousePress(0, y)
	if _, ok := m.FoldedAt(row); ok {
		t.Error("a second gutter click must expand it again")
	}
}

// TestSearchRevealsFoldedMatch guards the search interplay: a hit inside a
// collapsed fold opens it rather than being invisible.
func TestSearchRevealsFoldedMatch(t *testing.T) {
	m := foldViewer(t)
	row := rowWith(t, m, `"nested"`)
	m.ToggleFold(row)
	if m.RowVisible(rowWith(t, m, `"b": 2`)) {
		t.Fatal("precondition: the match row must be hidden")
	}
	// "b" appears only inside the collapsed object.
	typeKeys(m, "/", "b", "enter")
	if n, _ := m.MatchPosition(); n == 0 {
		t.Fatal("the search must find the hidden match")
	}
	if _, ok := m.FoldedAt(row); ok {
		t.Error("a match inside a collapsed fold must reveal it")
	}
}

// TestCopyIgnoresFoldState guards the copy interplay: hidden content is real
// content, and the placeholder is never copied.
func TestCopyIgnoresFoldState(t *testing.T) {
	m := foldViewer(t)
	body := m.BodyText()
	first := rowWith(t, m, "{")
	last := m.Rows() - 1
	m.ToggleFold(rowWith(t, m, `"nested"`))

	if got := m.BodyText(); got != body {
		t.Errorf("collapsing must not change the copied body:\n%q\nwant\n%q", got, body)
	}
	// Drag from the first body row to the last row of the response; the
	// selection spans the collapsed fold.
	m.MousePress(1, m.DisplayRow(first)+1)
	m.MouseDrag(len([]rune(m.RowText(last)))+1, m.DisplayRow(last)+1)
	m.MouseRelease()
	sel := m.SelectionText()
	if !strings.Contains(sel, `"b": 2`) {
		t.Errorf("the selection must copy hidden content:\n%s", sel)
	}
	if strings.Contains(sel, "⋯") {
		t.Errorf("the placeholder must never be copied:\n%s", sel)
	}
}

// typeKeys feeds plain key presses to the viewer.
func typeKeys(m *httppane.Model, keys ...string) {
	for _, k := range keys {
		if k == "enter" {
			m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			continue
		}
		r := []rune(k)[0]
		m.Update(tea.KeyPressMsg{Text: k, Code: r})
	}
}
