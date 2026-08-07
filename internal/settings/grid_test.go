package settings

// grid_test.go covers the three-column master·detail grid (0460, #1295): the
// column geometry, the focus cycle across it, and the typed editor the detail
// column dispatches per entry type.

import (
	"fmt"
	"strings"
	"testing"

	"ike/internal/config"
)

func gridPages() []Page {
	return []Page{{Section: "CORE", Title: "Interface", Description: "Chrome and colors.", Entries: []Entry{
		{Key: "ui.menu_bar", Type: Bool, Title: "Menu bar", Description: "b", Scope: config.UserScope},
		{Key: "editor.tab_width", Type: Int, Title: "Tab width", Description: "i", Scope: config.UserScope, Min: 1, Max: 16},
		{Key: "theme.name", Type: Enum, Title: "Theme", Description: "e", Scope: config.UserScope, Options: []string{"default", "tokyo-night"}},
		{Key: "palette.toggle_key", Type: Chord, Title: "Palette key", Description: "c", Scope: config.UserScope},
		{Key: "explorer.exclude", Type: List, Title: "Excluded", Description: "l", Scope: config.UserScope},
		{Key: "debug.php.hostname", Type: String, Title: "Host", Description: "s", Scope: config.ProjectScope},
	}}}
}

// TestGridColumnWidths: a wide panel gets the nominal 24/44/rest raster; a
// narrower one shrinks the settings column before it gives up the detail
// column; a very narrow one stacks the detail under the list.
func TestGridColumnWidths(t *testing.T) {
	m := New(gridPages(), config.Options{})

	m.SetSize(140, 24)
	g := m.gridFor()
	if !g.side || g.formW != formWidth {
		t.Fatalf("wide panel: side=%v formW=%d, want the nominal %d", g.side, g.formW, formWidth)
	}
	if want := 140 - 2 - catWidth - sepWidth - formWidth - sepWidth; g.detailW != want {
		t.Fatalf("detailW = %d, want the remainder %d", g.detailW, want)
	}

	m.SetSize(90, 24)
	if g := m.gridFor(); !g.side || g.formW >= formWidth || g.detailW < minDetailWidth {
		t.Fatalf("medium panel must keep three columns by shrinking the settings column: %+v", g)
	}

	m.SetSize(70, 24)
	g = m.gridFor()
	if g.side {
		t.Fatalf("narrow panel must stack the detail band: %+v", g)
	}
	if g.listH+g.detailH+1 != 24-chromeRows {
		t.Fatalf("stacked heights must fill the body: %+v", g)
	}
}

// TestFocusCyclesThreeColumns: tab walks nav → settings → detail → nav.
func TestFocusCyclesThreeColumns(t *testing.T) {
	restoreConfig(t)
	m := New(gridPages(), testOpts(t))
	m.SetSize(140, 24)
	m.Open()
	if m.focus != catColumn {
		t.Fatalf("the panel must open on the rail, focus = %v", m.focus)
	}
	m.Update(key("tab"))
	if m.focus != formColumn {
		t.Fatalf("tab 1: focus = %v, want the settings column", m.focus)
	}
	m.Update(key("tab"))
	if m.focus != detailColumn {
		t.Fatalf("tab 2: focus = %v, want the detail column", m.focus)
	}
	m.Update(key("tab"))
	if m.focus != catColumn {
		t.Fatalf("tab 3: focus = %v, want the rail again", m.focus)
	}
}

// TestEditorDispatchPerType: every entry type gets its own editor, so adding a
// setting needs a type and documentation, never new UI.
func TestEditorDispatchPerType(t *testing.T) {
	restoreConfig(t)
	m := New(gridPages(), testOpts(t))
	m.SetSize(140, 24)
	m.Open()
	m.focus = formColumn
	for i, want := range []string{"*settings.boolEditor", "*settings.intEditor", "*settings.enumEditor",
		"*settings.chordEditor", "*settings.listEditor", "*settings.textEditor"} {
		m.sel = i
		m.editor, m.editorKey = nil, ""
		m.syncEditor()
		if got := fmt.Sprintf("%T", m.editor); got != want {
			t.Fatalf("row %d editor = %s, want %s", i, got, want)
		}
	}
}

// TestValueMarkersRender: every row announces its editor with a marker.
func TestValueMarkersRender(t *testing.T) {
	restoreConfig(t)
	m := New(gridPages(), testOpts(t))
	m.SetSize(140, 24)
	m.Open()
	v := m.View()
	for _, want := range []string{"◉", "‹›", "▸", "⌨", "≡", "✎"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing the %q marker:\n%s", want, v)
		}
	}
}

// TestBasePagesCarryDescriptions: the detail column's no-selection state is
// only useful when every built-in page explains itself.
func TestBasePagesCarryDescriptions(t *testing.T) {
	for _, p := range BasePages([]string{"default"}, nil, nil) {
		if p.Custom == nil && strings.TrimSpace(p.Description) == "" {
			t.Errorf("page %q has no Description", p.Title)
		}
	}
}

// TestChordEditorPushesCapture: enter on a chord row goes straight into the
// shared capture sub-panel, the one editor that needs esc and tab verbatim.
func TestChordEditorPushesCapture(t *testing.T) {
	restoreConfig(t)
	m := New(gridPages(), testOpts(t))
	m.SetSize(140, 24)
	m.Open()
	m.focus = formColumn
	m.sel = 3 // the chord row
	m.Update(key("enter"))
	if !m.SubOpen() {
		t.Fatal("enter on a chord row must open the capture sub-panel")
	}
}
