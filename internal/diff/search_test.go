package diff

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// search_test.go covers #2409: the diff viewer's own in-pane search — "/" and
// the shared find chord open the prompt, n/N walk the matches, and hunk
// stepping keeps n/N while no search is open.

// typeQuery feeds the runes of s to the open prompt.
func typeQuery(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// numbered builds a document whose lines carry their index, so a query can
// name exactly one of them.
func numbered(prefix string, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(prefix)
		b.WriteByte('0' + byte(i))
		b.WriteByte('\n')
	}
	return b.String()
}

// TestSlashOpensTheSearchPrompt: "/" starts the prompt and it owns the keys.
func TestSlashOpensTheSearchPrompt(t *testing.T) {
	m := testModel(t, numbered("line", 8), numbered("line", 8))
	if m.Searching() {
		t.Fatal("the prompt starts closed")
	}
	m.Update(key("/"))
	if !m.Searching() {
		t.Fatal(`"/" must open the search prompt`)
	}
	typeQuery(m, "line3")
	if m.SearchQuery() != "line3" {
		t.Fatalf("query = %q", m.SearchQuery())
	}
	if got := plainView(m); !strings.Contains(got, "/line3") {
		t.Fatalf("the prompt row is not rendered:\n%s", got)
	}
}

// TestFindChordOpensTheSearchPrompt: ctrl+f does what "/" does (#2409).
func TestFindChordOpensTheSearchPrompt(t *testing.T) {
	m := testModel(t, "a\n", "b\n")
	m.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if !m.Searching() {
		t.Fatal("ctrl+f must open the search prompt")
	}
}

// TestOpenSearchCapability: the pane.Searchable entry point opens the prompt.
func TestOpenSearchCapability(t *testing.T) {
	m := testModel(t, "a\n", "b\n")
	if !m.OpenSearch() {
		t.Fatal("OpenSearch must report the prompt opened")
	}
	if !m.Searching() {
		t.Fatal("OpenSearch did not open the prompt")
	}
}

// TestSearchScrollsToMatchAndCounts: enter applies the query, the counter
// reports the match set, and the matching row is brought into view.
func TestSearchScrollsToMatchAndCounts(t *testing.T) {
	doc := numbered("line", 9)
	m := testModel(t, doc, doc)
	m.OpenSearch()
	typeQuery(m, "line8")
	if m.SearchMatches() != 1 {
		t.Fatalf("matches = %d, want 1", m.SearchMatches())
	}
	m.Update(key("enter"))
	if m.Searching() {
		t.Fatal("enter must leave the prompt")
	}
	if got := plainView(m); !strings.Contains(got, "line8") {
		t.Fatalf("the match was not scrolled into view:\n%s", got)
	}
}

// TestSearchStepsMatchesWithNAndN: with a search applied n/N walk its matches
// instead of the hunks, wrapping at both ends.
func TestSearchStepsMatchesWithNAndN(t *testing.T) {
	doc := "alpha\nbeta\nalpha\ngamma\n"
	m := testModel(t, doc, doc)
	m.OpenSearch()
	typeQuery(m, "alpha")
	m.Update(key("enter"))
	if m.SearchMatches() != 2 {
		t.Fatalf("matches = %d, want 2", m.SearchMatches())
	}
	first := m.search.Cur
	m.Update(key("n"))
	if m.search.Cur == first {
		t.Fatal("n must step to the next match")
	}
	m.Update(key("n"))
	if m.search.Cur != first {
		t.Fatal("n must wrap back to the first match")
	}
	m.Update(key("N"))
	if m.search.Cur == first {
		t.Fatal("N must step back")
	}
}

// TestNoSearchKeepsHunkStepping: without a query n/N still navigate changes,
// the diff pane's whole point.
func TestNoSearchKeepsHunkStepping(t *testing.T) {
	m := testModel(t, "a\nb\nc\n", "a\nB\nc\n")
	before := m.cur
	m.Update(key("n"))
	if m.cur == before {
		t.Fatal("without a search n must step the hunks")
	}
}

// TestEscClosesTheSearch: esc abandons the search and n/N return to hunks.
func TestEscClosesTheSearch(t *testing.T) {
	doc := "alpha\nbeta\n"
	m := testModel(t, doc, doc)
	m.OpenSearch()
	typeQuery(m, "alpha")
	m.Update(key("enter"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.search != nil {
		t.Fatal("esc must close the search")
	}
	if m.SearchQuery() != "" {
		t.Fatalf("query survived esc: %q", m.SearchQuery())
	}
}

// TestSearchPromptCostsOneRow: the prompt takes a line from the diff body
// while it is open, and gives it back when it closes.
func TestSearchPromptCostsOneRow(t *testing.T) {
	doc := numbered("line", 40)
	m := testModel(t, doc, doc)
	full := m.viewHeight()
	m.OpenSearch()
	if got := m.viewHeight(); got != full-1 {
		t.Fatalf("viewHeight with the prompt open = %d, want %d", got, full-1)
	}
	m.closeSearch()
	if got := m.viewHeight(); got != full {
		t.Fatalf("viewHeight after closing = %d, want %d", got, full)
	}
}

// TestSearchIsSmartcase: an all-lowercase query folds case, an uppercase rune
// makes it exact — the editor's "/" rule (#257).
func TestSearchIsSmartcase(t *testing.T) {
	doc := "Alpha\nalpha\n"
	m := testModel(t, doc, doc)
	m.OpenSearch()
	typeQuery(m, "alpha")
	if m.SearchMatches() != 2 {
		t.Fatalf("lowercase query matched %d rows, want both", m.SearchMatches())
	}
	m.closeSearch()
	m.OpenSearch()
	typeQuery(m, "Alpha")
	if m.SearchMatches() != 1 {
		t.Fatalf("cased query matched %d rows, want 1", m.SearchMatches())
	}
}
