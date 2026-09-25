package htmlpreview

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func press(m *Model, keys ...string) {
	for _, k := range keys {
		switch k {
		case "enter":
			m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		case "esc":
			m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		default:
			for _, r := range k {
				m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
			}
		}
	}
}

func TestSearchFindsRenderedText(t *testing.T) {
	m := sized(t, longDoc(80))
	m.SetFocused(true)
	press(&m, "/")
	if !m.Searching() {
		t.Fatal("/ opens the search prompt")
	}
	press(&m, "para07")
	if got := m.SearchMatches(); got != 10 {
		t.Fatalf("para07 matches %d lines, want 10 (para070–para079)", got)
	}
	press(&m, "enter")
	if m.Searching() {
		t.Fatal("enter applies and releases the keyboard")
	}
	if !strings.Contains(plain(m), "para070") {
		t.Fatalf("applying scrolls to the first match:\n%s", plain(m))
	}
	press(&m, "n")
	if !strings.Contains(plain(m), "para071") {
		t.Fatalf("n steps to the next match:\n%s", plain(m))
	}
	if st := m.NextMatch(); !st.Handled {
		t.Fatal("cmd+g steps while a search is open")
	}
	press(&m, "esc")
	if m.SearchQuery() != "" || m.NextMatch().Handled {
		t.Fatal("esc drops the search")
	}
}

func TestSearchIgnoresMarkup(t *testing.T) {
	m := sized(t, "<p>a <b>bold</b> word</p><p>class attr</p>")
	m.OpenSearch()
	press(&m, "bold word")
	if m.SearchMatches() != 1 {
		t.Fatalf("styling must not split the match: %d", m.SearchMatches())
	}
	press(&m, "esc", "/")
	m.search.Text = "<b>"
	m.recomputeMatches()
	if m.SearchMatches() != 0 {
		t.Fatal("tags are not rendered text")
	}
}

func TestSearchPromptTakesLastRow(t *testing.T) {
	m := sized(t, longDoc(80))
	m.OpenSearch()
	rows := strings.Split(plain(m), "\n")
	if len(rows) != 10 || !strings.HasPrefix(rows[9], "/") {
		t.Fatalf("prompt must be the last of 10 rows: %q", rows)
	}
	if !m.LineInputOpen() {
		t.Fatal("the open prompt owns the caret chords")
	}
}
