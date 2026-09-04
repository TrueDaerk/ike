package diff

import (
	"strings"
	"testing"
)

// matchstep_test.go covers #2410 for the diff viewer: cmd+g steps the matches
// while the "/" prompt still holds the keyboard, the query survives, and the
// step that comes back around says so.

// TestNextMatchStepsWithTheInputStillFocused is the issue's headline case.
func TestNextMatchStepsWithTheInputStillFocused(t *testing.T) {
	doc := numbered("line", 6)
	m := testModel(t, doc, doc)
	m.OpenSearch()
	typeQuery(m, "line")
	if !m.Searching() {
		t.Fatal("the prompt must still own the keyboard")
	}
	first := m.search.Cur
	st := m.NextMatch()
	if !st.Handled || st.Total != m.SearchMatches() {
		t.Fatalf("NextMatch = %+v, matches = %d", st, m.SearchMatches())
	}
	if m.search.Cur == first {
		t.Fatal("NextMatch did not move the current match")
	}
	if !m.Searching() {
		t.Fatal("the prompt must keep the keyboard across a step")
	}
	if m.SearchQuery() != "line" {
		t.Fatalf("the query must survive the step, got %q", m.SearchQuery())
	}
}

// TestPrevMatchWrapsAndSaysSo: stepping back off the first match lands on the
// last one, and the prompt row marks the wrap.
func TestPrevMatchWrapsAndSaysSo(t *testing.T) {
	doc := numbered("line", 4)
	m := testModel(t, doc, doc)
	m.OpenSearch()
	typeQuery(m, "line")
	m.search.Cur = 0
	st := m.PrevMatch()
	if !st.Wrapped {
		t.Fatalf("stepping back off the first match must wrap, got %+v", st)
	}
	if st.Index != st.Total {
		t.Fatalf("the wrap must land on the last match, got %+v", st)
	}
	if got := plainView(m); !strings.Contains(got, "(wrapped)") {
		t.Fatalf("the prompt row must mark the wrap:\n%s", got)
	}
}

// TestMatchStepWithoutASearchIsNotOurs: with no prompt open the chord keeps
// its older meaning, so the pane reports it did not handle it.
func TestMatchStepWithoutASearchIsNotOurs(t *testing.T) {
	m := testModel(t, "a\n", "b\n")
	if st := m.NextMatch(); st.Handled {
		t.Fatalf("NextMatch without a search = %+v, want not handled", st)
	}
}

// TestMatchStepWithNoMatchesIsAHandledNoOp: the pane owns the chord while the
// prompt is open, and a query that matches nothing moves nothing.
func TestMatchStepWithNoMatchesIsAHandledNoOp(t *testing.T) {
	m := testModel(t, "a\n", "b\n")
	m.OpenSearch()
	typeQuery(m, "zzz")
	st := m.NextMatch()
	if !st.Handled || st.Total != 0 {
		t.Fatalf("NextMatch over nothing = %+v, want handled with total 0", st)
	}
}

// TestEditingTheQueryDropsTheWrapMarker: the marker describes one step, not
// the search.
func TestEditingTheQueryDropsTheWrapMarker(t *testing.T) {
	doc := numbered("line", 4)
	m := testModel(t, doc, doc)
	m.OpenSearch()
	typeQuery(m, "line")
	m.search.Cur = 0
	m.PrevMatch()
	typeQuery(m, "1")
	if m.search.Wrapped {
		t.Fatal("an edited query must start a fresh walk")
	}
}
