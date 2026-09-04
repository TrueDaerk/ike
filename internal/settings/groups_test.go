package settings

import (
	"strings"
	"testing"
)

// customPageTitles are the pages the app and its plugins add beside
// BasePages (app.go, plugins/lsp, plugins/format, commands.go). The group
// table has to know every one of them; a new custom page extends this list
// and the table together.
var customPageTitles = []string{
	"Syntax Colors", "File Associations", "Keymap", "Tools", "PHP Debug Mappings",
	"Toolchain", "Plugins", "Marketplace", "Language Servers", "Formatters",
	"Elasticsearch",
}

// TestEveryPageHasAGroup: no page may fall into "Other" — a new page is placed
// in the rail deliberately.
func TestEveryPageHasAGroup(t *testing.T) {
	titles := titlesOf(BasePages([]string{"default"}, nil, nil))
	titles = append(titles, customPageTitles...)
	for _, title := range titles {
		if GroupOf(title) == otherGroup {
			t.Errorf("page %q is not in the group table (groups.go)", title)
		}
	}
	// And the table names no page that no longer exists.
	known := map[string]bool{}
	for _, title := range titles {
		known[title] = true
	}
	for _, g := range pageGroups {
		for _, title := range g.Titles {
			if !known[title] {
				t.Errorf("group %q lists unknown page %q", g.Name, title)
			}
		}
	}
}

// TestRegroupOrdersAndSections: pages come out in table order, the first page
// of each group carries the Section and the rest join it, unknown pages trail
// under "Other" in their original order.
func TestRegroupOrdersAndSections(t *testing.T) {
	in := []Page{
		{Title: "Zzz custom"},
		{Title: "Terminal"},
		{Title: "Appearance", Section: "STALE"},
		{Title: "Editor"},
		{Title: "Notifications"},
		{Title: "Yyy custom"},
	}
	out := Regroup(in)
	got := titlesOf(out)
	want := []string{"Editor", "Appearance", "Notifications", "Terminal", "Zzz custom", "Yyy custom"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
	sections := make([]string, len(out))
	for i, p := range out {
		sections[i] = p.Section
	}
	wantSec := []string{"Editing", "Interface", "", "Tools & Integrations", "Other", ""}
	if strings.Join(sections, ",") != strings.Join(wantSec, ",") {
		t.Fatalf("sections = %q, want %q", sections, wantSec)
	}
	if in[2].Section != "STALE" {
		t.Fatal("Regroup must not modify its input")
	}
}

// TestRailHeaderNotRepeated: two consecutive pages naming the same section
// share one header (the old rail printed CORE twice).
func TestRailHeaderNotRepeated(t *testing.T) {
	restoreConfig(t)
	pages := []Page{
		{Section: "CORE", Title: "Editor", Entries: []Entry{{Key: "ui.menu_bar", Title: "Menu bar", Type: Bool}}},
		{Section: "CORE", Title: "Appearance", Entries: []Entry{{Key: "theme.name", Title: "Theme", Type: Enum, Options: []string{"default"}}}},
	}
	m := New(pages, testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	if n := strings.Count(m.View(), "CORE"); n != 1 {
		t.Fatalf("CORE header rendered %d times, want 1:\n%s", n, m.View())
	}
}
