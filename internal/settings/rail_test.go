package settings

import (
	"strings"
	"testing"
)

func sectionPages() []Page {
	return []Page{
		{Section: "CORE", Title: "Editor", Entries: []Entry{{Key: "ui.menu_bar", Title: "Menu bar", Type: Bool}}},
		{Title: "Appearance", Entries: []Entry{{Key: "theme.name", Title: "Theme", Type: Enum, Options: []string{"default"}}}},
		{Section: "TOOLS", Title: "Toolchain", Entries: []Entry{{Key: "editor.tab_width", Title: "Tab width", Type: Int}}},
	}
}

// TestRailSectionsRender guards #890: section headers appear in the rail and
// are not click targets.
func TestRailSectionsRender(t *testing.T) {
	restoreConfig(t)
	m := New(sectionPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	v := m.View()
	for _, want := range []string{"CORE", "TOOLS", "Editor", "Toolchain"} {
		if !strings.Contains(v, want) {
			t.Fatalf("rail missing %q:\n%s", want, v)
		}
	}
	// Clicking the CORE header row (body row 0) selects nothing new.
	before := m.cat
	m.Click(2, 2)
	if m.cat != before {
		t.Fatalf("header click must not select, cat=%d", m.cat)
	}
	// Clicking the Editor row (body row 1) selects page 0.
	m.cat = 1
	m.Click(2, 3)
	if m.cat != 0 {
		t.Fatalf("page click under a header must map correctly, cat=%d", m.cat)
	}
}

// TestRailLetterJump guards #890: a letter on the rail hops to the next page
// starting with it, cycling.
func TestRailLetterJump(t *testing.T) {
	restoreConfig(t)
	m := New(sectionPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.Update(keyRune('t'))
	if m.pages[m.cat].Title != "Toolchain" {
		t.Fatalf("t must jump to Toolchain, got %q", m.pages[m.cat].Title)
	}
	m.Update(keyRune('e'))
	if m.pages[m.cat].Title != "Editor" {
		t.Fatalf("e must cycle to Editor, got %q", m.pages[m.cat].Title)
	}
}

// TestPanelRemembersPage guards #890: reopening lands on the last page, and
// the choice persists through the state file.
func TestPanelRemembersPage(t *testing.T) {
	restoreConfig(t)
	m := New(sectionPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.cat = 2
	m.Close()
	m.Open()
	if m.cat != 2 {
		t.Fatalf("reopen must keep the page, cat=%d", m.cat)
	}
	// A fresh panel (same process/project) restores from the state file.
	m2 := New(sectionPages(), testOpts(t))
	m2.Open()
	if m2.pages[m2.cat].Title != "Toolchain" {
		t.Fatalf("fresh panel must restore the persisted page, got %q", m2.pages[m2.cat].Title)
	}
}

// TestHeaderShowsCurrentPage guards #890.
func TestHeaderShowsCurrentPage(t *testing.T) {
	restoreConfig(t)
	m := New(sectionPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.cat = 2
	if v := m.View(); !strings.Contains(v, "SETTINGS › Toolchain") {
		t.Fatal("the title row must carry the current page name")
	}
}

// TestRailScrollIndicators guards #890: overflowing rails mark more content.
func TestRailScrollIndicators(t *testing.T) {
	restoreConfig(t)
	pages := sectionPages()
	for _, ti := range []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8"} {
		pages = append(pages, Page{Title: ti, Entries: []Entry{{Key: "ui.menu_bar", Title: "X", Type: Bool}}})
	}
	m := New(pages, testOpts(t))
	m.SetSize(90, 10)
	m.Open()
	if v := m.View(); !strings.Contains(v, "▼ more") {
		t.Fatalf("overflowing rail must show the down indicator:\n%s", v)
	}
	m.Wheel(2, 3)
	if v := m.View(); !strings.Contains(v, "▲ more") {
		t.Fatal("a scrolled rail must show the up indicator")
	}
}
