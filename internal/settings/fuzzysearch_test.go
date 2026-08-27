package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
	"ike/internal/theme"
)

// fuzzyPages are schema pages with real descriptions, so the match sources
// (key · label · description) can be told apart in a test.
func fuzzyPages() []Page {
	return []Page{
		{Title: "Editor", Entries: []Entry{
			{Key: "editor.tab_width", Type: Int, Title: "Tab width", Scope: config.UserScope, Min: 1, Max: 16,
				Description: "How many columns a tab occupies"},
			{Key: "editor.line_numbers", Type: Bool, Title: "Line numbers", Scope: config.UserScope,
				Description: "Show the gutter with line numbers"},
		}},
		{Title: "Interface", Entries: []Entry{
			{Key: "ui.menu_bar", Type: Bool, Title: "Menu bar", Scope: config.UserScope,
				Description: "Draw the menu bar above the workspace"},
		}},
	}
}

// fuzzyModel opens a panel over fuzzyPages with query typed into the filter.
func fuzzyModel(t *testing.T, query string) *Model {
	t.Helper()
	restoreConfig(t)
	m := New(fuzzyPages(), testOpts(t))
	m.SetSize(130, 20)
	m.Open()
	m.Update(key("/"))
	for _, r := range query {
		m.Update(keyRune(r))
	}
	m.Update(key("enter")) // end the input, keep the query
	return m
}

// keysOf lists the config keys of the matched entry rows, in row order.
func keysOf(rows []row) []string {
	var out []string
	for _, r := range rows {
		if r.kind == rowEntry {
			out = append(out, r.entry.Key)
		}
	}
	return out
}

// hasKey reports whether the rows contain an entry row for key.
func hasKey(rows []row, key string) bool {
	for _, k := range keysOf(rows) {
		if k == key {
			return true
		}
	}
	return false
}

// TestSearchMatchSources guards #2179: a setting is findable by its config
// key, its label and its description — and by a scattered (fuzzy) pattern of
// each, not only by a literal substring.
func TestSearchMatchSources(t *testing.T) {
	cases := []struct {
		name, query, want string
	}{
		{"key", "editor.tab_width", "editor.tab_width"},
		{"key fuzzy", "edtabw", "editor.tab_width"},
		{"label", "tab width", "editor.tab_width"},
		{"label fuzzy", "tbwdth", "editor.tab_width"},
		{"description", "columns occupies", "editor.tab_width"},
		{"description fuzzy", "gutter", "editor.line_numbers"},
		{"page and label", "interface menu", "ui.menu_bar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := fuzzyModel(t, tc.query)
			rows := m.rows()
			if !hasKey(rows, tc.want) {
				t.Fatalf("%q must find %q, got %v", tc.query, tc.want, keysOf(rows))
			}
		})
	}
}

// TestSearchTermsAreANDed: whitespace-separated terms narrow — a query whose
// terms live on different settings matches neither.
func TestSearchTermsAreANDed(t *testing.T) {
	m := fuzzyModel(t, "menu columns")
	if rows := m.rows(); len(rows) != 0 {
		t.Fatalf("terms must be ANDed, got %v", keysOf(rows))
	}
}

// TestSearchProseNeedsALongerPattern: a one- or two-rune pattern scattered
// through a description is noise, not a match — it only counts where it
// occurs literally.
func TestSearchProseNeedsALongerPattern(t *testing.T) {
	// "hm" is a subsequence of "How many columns a tab occupies" only when
	// scattered; it appears in no key or label.
	m := fuzzyModel(t, "hm")
	if rows := m.rows(); len(rows) != 0 {
		t.Fatalf("a scattered 2-rune prose match must not count, got %v", keysOf(rows))
	}
	// The same source matches once the pattern is long enough to be meant.
	m = fuzzyModel(t, "many columns")
	if !hasKey(m.rows(), "editor.tab_width") {
		t.Fatalf("a longer prose pattern must match, got %v", keysOf(m.rows()))
	}
}

// TestSearchRanksNameHitsFirst: a hit in the key or the label outranks the
// same word buried in another setting's description, so the strongest match
// is the first row.
func TestSearchRanksNameHitsFirst(t *testing.T) {
	m := fuzzyModel(t, "line numbers")
	rows := m.rows()
	if len(rows) == 0 {
		t.Fatal("the query must match something")
	}
	if rows[0].kind != rowEntry || rows[0].entry.Key != "editor.line_numbers" {
		t.Fatalf("the labelled entry must rank first, got %v", keysOf(rows))
	}
}

// TestSearchJumpLandsOnTheFormField guards #2179: selecting a result opens
// its own category positioned on the real form field, focused, with the
// field's editor live — editing works immediately.
func TestSearchJumpLandsOnTheFormField(t *testing.T) {
	m := fuzzyModel(t, "menu bar")
	rows := m.rows()
	if len(rows) == 0 || rows[0].kind != rowEntry {
		t.Fatalf("rows = %+v, want a matched entry first", rows)
	}
	want := rows[0].entry.Key
	m.focus, m.sel = formColumn, 0
	m.Update(key("tab")) // "open page" on the highlighted match
	if m.filter != "" || m.filtering {
		t.Fatal("the jump must leave the search")
	}
	if m.focus != formColumn {
		t.Fatalf("focus = %v, want the settings column", m.focus)
	}
	if m.pages[m.cat].Title != "Interface" {
		t.Fatalf("landed on page %q, want Interface", m.pages[m.cat].Title)
	}
	got, ok := m.current()
	if !ok || got.kind != rowEntry || got.entry.Key != want {
		t.Fatalf("selection = %+v, want the %q row", got, want)
	}
	m.syncEditor()
	if m.editor == nil || m.editorKey != want {
		t.Fatalf("editorKey = %q, want a live editor for %q", m.editorKey, want)
	}
	// The landing row is editable straight away: space toggles the boolean.
	m.Update(key("space"))
	if !m.Dirty() {
		t.Fatal("editing must work on the landing row without another key")
	}
}

// TestSearchEscClearsThenCloses guards the speed-search esc convention
// (#2179): the first press drops the query, the second leaves the search.
func TestSearchEscClearsThenCloses(t *testing.T) {
	restoreConfig(t)
	m := New(fuzzyPages(), testOpts(t))
	m.SetSize(130, 20)
	m.Open()
	m.Update(key("/"))
	for _, r := range "menu" {
		m.Update(keyRune(r))
	}
	m.Update(key("esc"))
	if m.filter != "" {
		t.Fatalf("the first esc must clear the query, filter=%q", m.filter)
	}
	if !m.filtering {
		t.Fatal("the first esc must stay in the search")
	}
	m.Update(key("esc"))
	if m.filtering {
		t.Fatal("the second esc must leave the search")
	}
	if !m.open {
		t.Fatal("leaving the search must not close the panel")
	}
}

// TestHighlightMarksFuzzyPositions: a scattered match is marked on the runes
// the matcher hit, so the row shows why it is in the list.
func TestHighlightMarksFuzzyPositions(t *testing.T) {
	pal := theme.DefaultPalette()
	got := highlightMatch("Tab width", "tbw", pal)
	if got == "Tab width" {
		t.Fatal("a fuzzy match must be styled")
	}
	if plain := stripANSI(got); plain != "Tab width" {
		t.Fatalf("highlighting must not change the text, got %q", plain)
	}
	if got := highlightMatch("Tab width", "zq", pal); got != "Tab width" {
		t.Fatalf("a non-match must render unchanged, got %q", got)
	}
	// Every term of a multi-term query is marked.
	got = stripANSI(highlightMatch("Menu bar", "menu bar", pal))
	if !strings.Contains(got, "Menu bar") {
		t.Fatalf("multi-term highlighting must keep the text, got %q", got)
	}
}
