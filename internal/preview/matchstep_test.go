package preview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// matchstep_test.go covers #2410 for the markdown preview: cmd+g steps the
// matches while the "/" prompt keeps the keyboard and the query survives.

func TestPreviewNextMatchKeepsTheInputFocused(t *testing.T) {
	m := searchPane()
	m.OpenSearch()
	typeQuery(&m, "text")
	if m.SearchMatches() < 2 {
		t.Fatalf("the fixture needs several matches, got %d", m.SearchMatches())
	}
	before := m.search.Cur
	st := m.NextMatch()
	if !st.Handled || m.search.Cur == before {
		t.Fatalf("NextMatch = %+v, cur %d → %d", st, before, m.search.Cur)
	}
	if !m.Searching() || m.SearchQuery() != "text" {
		t.Fatalf("the prompt must survive the step: searching=%v query=%q",
			m.Searching(), m.SearchQuery())
	}
}

func TestPreviewPrevMatchWrapsAndSaysSo(t *testing.T) {
	m := searchPane()
	m.OpenSearch()
	typeQuery(&m, "text")
	m.search.Cur = 0
	st := m.PrevMatch()
	if !st.Wrapped || st.Index != st.Total {
		t.Fatalf("the backward wrap = %+v", st)
	}
	if got := ansi.Strip(m.View()); !strings.Contains(got, "(wrapped)") {
		t.Fatalf("the prompt row must mark the wrap:\n%s", got)
	}
}

func TestPreviewMatchStepWithoutASearch(t *testing.T) {
	m := searchPane()
	if st := m.NextMatch(); st.Handled {
		t.Fatalf("NextMatch without a prompt = %+v, want not handled", st)
	}
}

func TestPreviewMatchStepWithNoMatches(t *testing.T) {
	m := searchPane()
	m.OpenSearch()
	typeQuery(&m, "zzzz")
	if st := m.NextMatch(); !st.Handled || st.Total != 0 {
		t.Fatalf("NextMatch over nothing = %+v", st)
	}
}
