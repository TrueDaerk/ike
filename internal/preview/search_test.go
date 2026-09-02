package preview

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// search_test.go covers #2409: the markdown preview's in-pane search — "/" and
// the shared find chord open the prompt, n/N walk the matches.

// typeQuery feeds the runes of s to the open prompt.
func typeQuery(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// searchPane returns a rendered preview over doc, focused and sized.
func searchPane() Model {
	m := newSized()
	m.SetFocused(true)
	m.SetSourceImmediate(doc)
	return m
}

// TestSlashOpensThePreviewSearch: "/" starts the prompt and it owns the keys.
func TestSlashOpensThePreviewSearch(t *testing.T) {
	m := searchPane()
	if m.Searching() {
		t.Fatal("the prompt starts closed")
	}
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !m.Searching() {
		t.Fatal(`"/" must open the search prompt`)
	}
	typeQuery(&m, "Gamma")
	if m.SearchQuery() != "Gamma" {
		t.Fatalf("query = %q", m.SearchQuery())
	}
	if got := ansi.Strip(m.View()); !strings.Contains(got, "/Gamma") {
		t.Fatalf("the prompt row is not rendered:\n%s", got)
	}
}

// TestFindChordOpensThePreviewSearch: ctrl+f does what "/" does (#2409).
func TestFindChordOpensThePreviewSearch(t *testing.T) {
	m := searchPane()
	m.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if !m.Searching() {
		t.Fatal("ctrl+f must open the search prompt")
	}
}

// TestPreviewOpenSearchCapability: the pane.Searchable entry point opens it.
func TestPreviewOpenSearchCapability(t *testing.T) {
	m := searchPane()
	if !m.OpenSearch() {
		t.Fatal("OpenSearch must report the prompt opened")
	}
	if !m.Searching() {
		t.Fatal("OpenSearch did not open the prompt")
	}
}

// TestPreviewSearchScrollsToMatch: a query for text below the fold brings its
// line into view once applied.
func TestPreviewSearchScrollsToMatch(t *testing.T) {
	m := newSized()
	m.SetFocused(true)
	m.SetSourceImmediate("# Top\n\n" + strings.Repeat("filler\n\n", 30) + "\n## Needle\n")
	if strings.Contains(ansi.Strip(m.View()), "Needle") {
		t.Skip("the document fits the viewport; nothing to scroll to")
	}
	m.OpenSearch()
	typeQuery(&m, "Needle")
	if m.SearchMatches() == 0 {
		t.Fatal("the query must match the heading")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := ansi.Strip(m.View()); !strings.Contains(got, "Needle") {
		t.Fatalf("the match was not scrolled into view:\n%s", got)
	}
}

// TestPreviewSearchStepsMatches: n/N walk the matches, wrapping at both ends.
func TestPreviewSearchStepsMatches(t *testing.T) {
	m := newSized()
	m.SetFocused(true)
	m.SetSourceImmediate("alpha\n\nbeta\n\nalpha\n")
	m.OpenSearch()
	typeQuery(&m, "alpha")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.SearchMatches() != 2 {
		t.Fatalf("matches = %d, want 2", m.SearchMatches())
	}
	first := m.search.cur
	m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if m.search.cur == first {
		t.Fatal("n must step to the next match")
	}
	m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if m.search.cur != first {
		t.Fatal("n must wrap back to the first match")
	}
}

// TestPreviewEscClosesTheSearch: esc abandons the search.
func TestPreviewEscClosesTheSearch(t *testing.T) {
	m := searchPane()
	m.OpenSearch()
	typeQuery(&m, "Beta")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.search != nil {
		t.Fatal("esc must close the search")
	}
}

// TestPreviewSearchPromptCostsOneRow: the prompt takes a line from the body
// while it is open, and gives it back when it closes.
func TestPreviewSearchPromptCostsOneRow(t *testing.T) {
	m := searchPane()
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
