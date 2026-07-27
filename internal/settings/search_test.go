package settings

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/theme"
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

// TestFilterRailListsHitPages guards #886/#1297: the rail is never dead while
// filtering — it lists the pages carrying matches, and a click jumps the match
// list to that page's first hit without leaving the search.
func TestFilterRailListsHitPages(t *testing.T) {
	m, _ := searchModel(t)
	m.Update(key("/"))
	for _, r := range "e" { // a broad query, so several pages match
		m.Update(keyRune(r))
	}
	pages := m.hitPages()
	if len(pages) < 2 {
		t.Skipf("query matched %d pages, need at least two", len(pages))
	}
	m.Click(2, 2+1+1) // header row + the second hit page
	if m.filter == "" {
		t.Fatal("a rail click must stay inside the search")
	}
	if m.hitSel != 1 || m.sel != pages[1].firstRow {
		t.Fatalf("rail click must jump to the page's first hit, hitSel=%d sel=%d", m.hitSel, m.sel)
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

// --- search in the grid (0460, #1297) ---

// gridSearch opens a filtered panel wide enough for three columns.
func gridSearch(t *testing.T, query string) *Model {
	t.Helper()
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(130, 20)
	m.Open()
	m.Update(key("/"))
	for _, r := range query {
		m.Update(keyRune(r))
	}
	m.Update(key("enter")) // end the filter input, keep the query
	return m
}

// TestSearchGroupsHitsByPage: the nav column answers "where are the matches",
// with counts.
func TestSearchGroupsHitsByPage(t *testing.T) {
	m := gridSearch(t, "e")
	pages := m.hitPages()
	if len(pages) == 0 {
		t.Fatal("the query must match something")
	}
	total := 0
	for _, p := range pages {
		total += p.count
	}
	if total != len(m.rows()) {
		t.Fatalf("hit counts = %d, want the %d matched rows", total, len(m.rows()))
	}
	v := m.View()
	if !strings.Contains(v, "pages with hits") {
		t.Fatalf("the nav column must switch to hit pages:\n%s", v)
	}
	if !strings.Contains(v, m.hitSummary()) {
		t.Fatalf("the header must summarise the matches (%q):\n%s", m.hitSummary(), v)
	}
}

// TestSearchRailWalksPages: moving in the nav column jumps the match list to
// the next page's first hit, and moving the match list walks the rail back.
func TestSearchRailWalksPages(t *testing.T) {
	m := gridSearch(t, "e")
	pages := m.hitPages()
	if len(pages) < 2 {
		t.Skipf("query matched %d pages, need at least two", len(pages))
	}
	m.focus = catColumn
	m.Update(key("down"))
	if m.hitSel != 1 || m.sel != pages[1].firstRow {
		t.Fatalf("rail move must jump the match list, hitSel=%d sel=%d", m.hitSel, m.sel)
	}
	m.focus = formColumn
	m.sel = pages[0].firstRow
	m.Update(key("down"))
	m.syncHitSel()
	if m.hitSel != 0 {
		t.Fatalf("the rail must follow the highlighted match, hitSel=%d", m.hitSel)
	}
}

// TestSearchSetsValueInPlace: enter on a matched entry opens its editor in the
// detail column — the value is settable without leaving the search.
func TestSearchSetsValueInPlace(t *testing.T) {
	m := gridSearch(t, "menu")
	rows := m.rows()
	if len(rows) == 0 || rows[0].kind != rowEntry {
		t.Fatalf("rows = %+v, want a matched entry first", rows)
	}
	m.focus, m.sel = formColumn, 0
	m.Update(key("enter")) // ui.menu_bar is a toggle: enter flips it in place
	if m.filter == "" {
		t.Fatal("setting a value must not clear the search")
	}
	if !m.Dirty() {
		t.Fatal("the edit must stage from the search result")
	}
	if m.editor == nil {
		t.Fatal("the detail column must carry the result's editor")
	}
}

// TestSearchTabOpensTheOwningPage: tab leaves the search on the match's page,
// positioned on that row.
func TestSearchTabOpensTheOwningPage(t *testing.T) {
	m := gridSearch(t, "theme")
	rows := m.rows()
	if len(rows) == 0 {
		t.Fatal("the query must match something")
	}
	want := rows[0]
	m.focus, m.sel = formColumn, 0
	m.Update(key("tab"))
	if m.filter != "" {
		t.Fatal("tab must leave the search")
	}
	if m.cat != want.page {
		t.Fatalf("cat = %d, want the match's page %d", m.cat, want.page)
	}
	if got := m.rows()[m.sel]; got.entry.Key != want.entry.Key {
		t.Fatalf("landed on %q, want %q", got.entry.Key, want.entry.Key)
	}
}

// stripANSI drops SGR sequences so a styled string can be compared by text.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// TestSearchHighlightsTheMatch: the matched substring is marked in the row.
func TestSearchHighlightsTheMatch(t *testing.T) {
	pal := theme.DefaultPalette()
	got := highlightMatch("Command palette key", "palette", pal)
	if got == "Command palette key" {
		t.Fatal("the matched substring must be styled")
	}
	if !strings.Contains(stripANSI(got), "Command palette key") {
		t.Fatalf("highlighting must not change the text, got %q", stripANSI(got))
	}
	if got := highlightMatch("Theme", "zzz", pal); got != "Theme" {
		t.Fatalf("a non-match must render unchanged, got %q", got)
	}
}
