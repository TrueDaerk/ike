package settings

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
)

func keyRune(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

// searchModel builds a panel with schema pages plus a searchable toolchain
// page.
func searchModel(t *testing.T) (*Model, *ToolchainPage) {
	t.Helper()
	restoreConfig(t)
	tp := NewToolchainPage(config.Options{}, t.TempDir(), nil)
	tp.look = func(name string) string {
		if name == "uv" {
			return "/bin/uv"
		}
		return ""
	}
	tp.run = func(string, ...string) string { return "" }
	m := New(append(testPages(), Page{Title: "Toolchain", Custom: tp}), testOpts(t))
	m.SetSize(100, 24)
	m.Open()
	return m, tp
}

// TestFilterFindsCustomPageItems guards #886: "/python" surfaces the
// Toolchain rows and enter navigates there.
func TestFilterFindsCustomPageItems(t *testing.T) {
	m, tp := searchModel(t)
	m.Update(key("/"))
	for _, r := range "python" {
		m.Update(keyRune(r))
	}
	m.Update(key("enter")) // leave filter typing
	rows := m.rows()
	found := -1
	for i, r := range rows {
		if r.kind == rowItem && strings.Contains(r.label, "python") {
			found = i
		}
	}
	if found < 0 {
		t.Fatalf("filter must surface the toolchain python item, rows=%d", len(rows))
	}
	m.sel, m.focus = found, formColumn
	m.Update(key("enter"))
	if m.filter != "" {
		t.Fatal("activating a result must clear the filter")
	}
	if m.pages[m.cat].Custom != tp {
		t.Fatal("activation must land on the toolchain page")
	}
	if tp.rows()[tp.sel].lang.ID != "python" {
		t.Fatalf("activation must select the python row, sel=%d", tp.sel)
	}
}

// TestFilterFindsPages guards #886: a category title matches as a jump row.
func TestFilterFindsPages(t *testing.T) {
	m, tp := searchModel(t)
	m.Update(key("/"))
	for _, r := range "toolch" {
		m.Update(keyRune(r))
	}
	rows := m.rows()
	if len(rows) == 0 || rows[0].kind != rowPage {
		t.Fatalf("category title must match as a jump row, rows=%+v", rows)
	}
	m.filtering = false
	m.sel, m.focus = 0, formColumn
	m.Update(key("enter"))
	if m.pages[m.cat].Custom != tp || m.filter != "" {
		t.Fatal("the jump row must navigate to the page")
	}
}

// TestFilterRailStaysAlive guards #886: a rail click while filtering clears
// the filter and selects the page.
func TestFilterRailStaysAlive(t *testing.T) {
	m, _ := searchModel(t)
	m.Update(key("/"))
	for _, r := range "menu" {
		m.Update(keyRune(r))
	}
	m.Click(2, 3) // second rail row
	if m.filter != "" || m.cat != 1 {
		t.Fatalf("rail click must clear the filter and select, filter=%q cat=%d", m.filter, m.cat)
	}
}

// TestFilterNoteListsOnlyUnsearchablePages: pages exporting SearchItems drop
// out of the "not searched" note.
func TestFilterNoteListsOnlyUnsearchablePages(t *testing.T) {
	m, _ := searchModel(t)
	if note := m.customPagesNote(); strings.Contains(note, "Toolchain") {
		t.Fatalf("searchable pages must not be listed, note=%q", note)
	}
}
