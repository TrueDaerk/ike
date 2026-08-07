package settings

// issue1664_test.go covers the settings-panel usability fixes of #1664: the
// column-header focus cue, the finished detail-column mouse support (stacked
// band wheel, page-detail click guard), grid reflow on resize, and the theme
// enums' palette swatch previews.

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// TestColumnHeadersMarkFocus: the header row marks exactly the focused column
// with ▸, and tab moves the mark along the grid cycle.
func TestColumnHeadersMarkFocus(t *testing.T) {
	restoreConfig(t)
	m := New(gridPages(), testOpts(t))
	m.SetSize(140, 24)
	m.Open()

	check := func(step string, want string, notWant ...string) {
		t.Helper()
		v := m.View()
		if !strings.Contains(v, "▸ "+want) {
			t.Fatalf("%s: header must mark %q focused:\n%s", step, want, v)
		}
		for _, n := range notWant {
			if strings.Contains(v, "▸ "+n) {
				t.Fatalf("%s: header must not mark %q focused:\n%s", step, n, v)
			}
		}
	}
	check("open", "Pages", "Settings", "Detail")
	m.Update(key("tab"))
	check("tab 1", "Settings", "Pages", "Detail")
	m.Update(key("tab"))
	check("tab 2", "Detail", "Pages", "Settings")
	m.Update(key("tab"))
	check("tab 3", "Pages", "Settings", "Detail")
}

// TestColumnHeadersStackedNamesFocusedHalf: the narrow fallback has one shared
// right column; its caption names whichever half owns the keys.
func TestColumnHeadersStackedNamesFocusedHalf(t *testing.T) {
	restoreConfig(t)
	m := New(gridPages(), testOpts(t))
	m.SetSize(60, 24)
	m.Open()
	if m.gridFor().side {
		t.Skip("panel wide enough for the three-column grid")
	}
	m.focus = formColumn
	if v := m.View(); !strings.Contains(v, "▸ Settings") {
		t.Fatalf("stacked header must mark Settings:\n%s", v)
	}
	m.syncEditor()
	m.focus = detailColumn
	if v := m.View(); !strings.Contains(v, "▸ Detail") {
		t.Fatalf("stacked header must mark Detail while the band is focused:\n%s", v)
	}
}

// TestDetailWheelInStackedBand: the wheel over the stacked detail band drives
// the editor, not the settings list above it (#1664 finishing #1325).
func TestDetailWheelInStackedBand(t *testing.T) {
	restoreConfig(t)
	m := New(detailPages(), testOpts(t))
	m.SetSize(60, 24)
	m.Open()
	m.cat, m.sel, m.focus = 1, 0, formColumn // theme.name, an Enum
	m.syncEditor()
	g := m.gridFor()
	if g.side {
		t.Skip("panel wide enough for the three-column grid")
	}
	_ = m.View()
	n, ok := m.editor.(*enumEditor)
	if !ok {
		t.Fatalf("editor = %T, want an enum editor", m.editor)
	}
	n.idx = 0
	m.Wheel(formX()+2, bodyTop+g.listH+2, 1) // inside the band
	if n.idx != 1 {
		t.Fatalf("band wheel must move the option selection, idx = %d", n.idx)
	}
	m.Wheel(formX()+2, bodyTop, 1) // over the list
	if n.idx != 1 {
		t.Fatalf("list wheel must stay with the form viewport, idx = %d", n.idx)
	}
}

// TestDetailClickWhileRailFocusedOnlyFocuses: with the rail focused the detail
// column shows the page explanation — a press there focuses the column and
// must not operate the (not yet drawn) editor rows via a stale body offset.
func TestDetailClickWhileRailFocusedOnlyFocuses(t *testing.T) {
	m := detailModel(t, 0, 0) // ui.menu_bar, a Bool
	m.focus = catColumn
	_ = m.View() // draws the page detail; no editor body this frame
	m.Click(m.detailX()+2, bodyTop+1)
	if m.focus != detailColumn {
		t.Fatalf("focus = %v, want the detail column", m.focus)
	}
	if _, staged := m.change("ui.menu_bar"); staged {
		t.Fatal("a press on the page explanation must not change the value")
	}
}

// TestGridReflowsOnSetSize: SetSize while open recomputes the grid — a shrink
// drops to the stacked fallback, growing again restores the three columns.
func TestGridReflowsOnSetSize(t *testing.T) {
	restoreConfig(t)
	m := New(gridPages(), testOpts(t))
	m.SetSize(140, 24)
	m.Open()
	if !m.gridFor().side {
		t.Fatal("wide panel must lay out three columns")
	}
	m.SetSize(60, 20)
	if m.gridFor().side {
		t.Fatal("after shrinking, the grid must reflow to the stacked fallback")
	}
	if w, h := m.Size(); w != 60 || h != 20 {
		t.Fatalf("Size() = (%d,%d), want the new (60,20)", w, h)
	}
	m.SetSize(140, 24)
	if !m.gridFor().side {
		t.Fatal("growing back must restore the three-column grid")
	}
}

// TestThemeSwatchRendering: a registered theme yields a non-empty block strip,
// an unknown name yields nothing (it must not preview as the default theme).
func TestThemeSwatchRendering(t *testing.T) {
	sw := themePreviewFn(nil)
	if s := sw("default"); s == "" || !strings.Contains(s, "█") {
		t.Fatalf("default theme must render a block swatch, got %q", s)
	}
	if s := sw("tokyo-night"); s == "" {
		t.Fatal("tokyo-night must render a swatch")
	}
	if s := sw("no-such-theme"); s != "" {
		t.Fatalf("an unknown theme must render no swatch, got %q", s)
	}
	if a, b := sw("default"), sw("default"); a != b {
		t.Fatal("the memoized swatch must be stable")
	}
}

// TestThemeEnumRowsCarrySwatches: BasePages wires the preview into the theme
// enums, and the option list renders it on every row.
func TestThemeEnumRowsCarrySwatches(t *testing.T) {
	restoreConfig(t)
	pages := BasePages([]string{"default", "tokyo-night"}, nil, nil)
	var entry Entry
	for _, p := range pages {
		for _, e := range p.Entries {
			if e.Key == "theme.name" {
				entry = e
			}
		}
	}
	if entry.Preview == nil {
		t.Fatal("theme.name must carry a Preview")
	}
	m := New(pages, testOpts(t))
	ed := newEnumEditor(m, entry)
	view := strings.Join(ed.View(60, 10), "\n")
	if strings.Count(view, "██") < 2 {
		t.Fatalf("every theme option row must carry its swatch strip:\n%s", view)
	}
}

// TestNonThemeEnumsKeepPlainRows: an enum without a Preview renders as before.
func TestNonThemeEnumsKeepPlainRows(t *testing.T) {
	restoreConfig(t)
	m := New(gridPages(), testOpts(t))
	e := Entry{Key: "editor.auto_save", Type: Enum, Title: "Auto save", Scope: config.UserScope, Options: []string{"focus", "idle", "off"}}
	view := strings.Join(newEnumEditor(m, e).View(60, 10), "\n")
	if strings.Contains(view, "█") {
		t.Fatalf("a preview-less enum must not render swatches:\n%s", view)
	}
}
