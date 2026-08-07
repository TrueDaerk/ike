package settings

import (
	"testing"

	"charm.land/lipgloss/v2"

	"ike/internal/config"
)

// detailModel opens the panel on the first entry with its editor built and one
// frame rendered, so the detail column's body offset is known (#1325).
func detailModel(t *testing.T, page, row int) *Model {
	t.Helper()
	restoreConfig(t)
	m := New(detailPages(), testOpts(t))
	m.SetSize(110, 24)
	m.Open()
	m.cat, m.sel, m.focus = page, row, formColumn
	m.syncEditor()
	if !m.gridFor().side {
		t.Skip("panel too narrow for the three-column grid")
	}
	_ = m.View() // records detailBodyTop
	return m
}

// detailPages adds a list and a path entry to the shared fixture so every
// editor type with a pointer target is covered.
func detailPages() []Page {
	return append(testPages(), Page{Title: "More", Entries: []Entry{
		{Key: "explorer.exclude", Type: List, Title: "Exclude", Scope: config.UserScope},
	}})
}

// clickBody presses row bodyRow of the editor body in the detail column.
func clickBody(m *Model, bodyRow int) { m.Click(m.detailX()+2, bodyTop+m.detailBodyTop+bodyRow) }

// TestDetailBoolClickPicks guards #1325: clicking a radio row of the boolean
// editor selects it and stages the value.
func TestDetailBoolClickPicks(t *testing.T) {
	m := detailModel(t, 0, 0) // ui.menu_bar, a Bool
	if m.value("ui.menu_bar") != "true" {
		t.Fatalf("precondition: ui.menu_bar = %q, want true", m.value("ui.menu_bar"))
	}
	clickBody(m, 1) // the "off" row
	if m.focus != detailColumn {
		t.Fatalf("focus = %v, want the detail column", m.focus)
	}
	if v, staged := m.change("ui.menu_bar"); !staged || v.shown != "false" {
		t.Fatalf("clicking the off row must stage false, got %+v staged=%v", v, staged)
	}
	clickBody(m, 0) // back to "on"
	if v, staged := m.change("ui.menu_bar"); staged && v.shown == "false" {
		t.Fatal("clicking the on row must take the value back to true")
	}
}

// TestDetailEnumClickPicksOption guards the option list: a press picks the
// option under the pointer, and the wheel moves the selection.
func TestDetailEnumClickPicksOption(t *testing.T) {
	m := detailModel(t, 1, 0) // theme.name, an Enum
	clickBody(m, 2)           // head row + the second option
	if v, staged := m.change("theme.name"); !staged || v.shown != "tokyo-night" {
		t.Fatalf("clicking the second option must stage it, got %+v staged=%v", v, staged)
	}
	n, ok := m.editor.(*enumEditor)
	if !ok {
		t.Fatalf("editor = %T, want an enum editor", m.editor)
	}
	n.idx = 0
	m.Wheel(m.detailX()+2, bodyTop, 1)
	if n.idx != 1 {
		t.Fatalf("the wheel must move the option selection: idx = %d", n.idx)
	}
}

// TestDetailIntClickSteps guards the stepper: the ‹ and › glyphs step.
func TestDetailIntClickSteps(t *testing.T) {
	m := detailModel(t, 0, 1) // editor.tab_width, an Int
	before := m.value("editor.tab_width")
	m.Click(m.detailX()+1, bodyTop+m.detailBodyTop) // the ‹ glyph
	v, staged := m.change("editor.tab_width")
	if !staged {
		t.Fatal("clicking ‹ must step the value")
	}
	if v.shown == before {
		t.Fatalf("value unchanged: %q", v.shown)
	}
	down := v.shown
	n := m.editor.(*intEditor)
	m.Click(m.detailX()+widthOf(" ‹ "+n.tf.View()+" "), bodyTop+m.detailBodyTop) // the › glyph
	if v, _ := m.change("editor.tab_width"); v.shown == down {
		t.Fatalf("clicking › must step back up, still %q", v.shown)
	}
}

// TestDetailListClickSelectsThenEdits guards the select-then-activate rule:
// a press on another row only selects it, a press on the selected row opens
// the inline input.
func TestDetailListClickSelectsThenEdits(t *testing.T) {
	m := detailModel(t, 2, 0) // explorer.exclude, a List
	l, ok := m.editor.(*listEditor)
	if !ok {
		t.Fatalf("editor = %T, want a list editor", m.editor)
	}
	if len(l.items) < 2 {
		t.Skipf("fixture list too short: %v", l.items)
	}
	clickBody(m, 1)
	if l.idx != 1 || l.editing {
		t.Fatalf("first press must only select: idx=%d editing=%v", l.idx, l.editing)
	}
	clickBody(m, 1)
	if !l.editing {
		t.Fatal("a press on the selected row must start editing it")
	}
}

// TestDetailHeadClickOnlyFocuses guards that the documentation above the
// editor is not a control: pressing it focuses the column and nothing else.
func TestDetailHeadClickOnlyFocuses(t *testing.T) {
	m := detailModel(t, 0, 0)
	m.Click(m.detailX()+2, bodyTop) // the title line of the detail column
	if m.focus != detailColumn {
		t.Fatalf("focus = %v, want the detail column", m.focus)
	}
	if _, staged := m.change("ui.menu_bar"); staged {
		t.Fatal("a press on the description must not change the value")
	}
}

// TestDetailClickInStackedBand guards the narrow-panel fallback, where the
// detail sits under the settings list instead of beside it.
func TestDetailClickInStackedBand(t *testing.T) {
	restoreConfig(t)
	m := New(detailPages(), testOpts(t))
	m.SetSize(60, 24) // too narrow for three columns
	m.Open()
	m.focus = formColumn
	m.syncEditor()
	g := m.gridFor()
	if g.side {
		t.Skip("panel wide enough for the three-column grid")
	}
	_ = m.View()
	m.Click(formX()+2, bodyTop+g.listH+1+m.detailBodyTop+1) // the "off" row of the band
	if m.focus != detailColumn {
		t.Fatalf("focus = %v, want the detail column", m.focus)
	}
	if _, staged := m.change("ui.menu_bar"); !staged {
		t.Fatal("the stacked band's rows must be clickable too")
	}
}

// widthOf is the display width of a rendered fragment.
func widthOf(s string) int { return lipgloss.Width(s) }
