package usages

// matchstep_test.go covers #2410 for the Usages pane. Its search is the
// shared filter row, so its "matches" are the hits the filter left standing:
// cmd+g walks them while the row keeps the keyboard and the expression stays
// editable, skipping the file headers.

import (
	"strings"
	"testing"
)

func TestUsagesMatchStepWalksTheHits(t *testing.T) {
	m := filterModel(t)
	m.filter.Focus()
	hits := len(previews(m))
	if hits != 4 {
		t.Fatalf("the fixture must hold four hits, got %d", hits)
	}
	st := m.NextMatch()
	if !st.Handled || st.Total != hits {
		t.Fatalf("NextMatch = %+v, want %d matches", st, hits)
	}
	if m.rows[m.cursor].header {
		t.Fatal("the walk must never land on a file header")
	}
	if !m.filter.Active() {
		t.Fatal("the filter row must keep the keyboard across a step")
	}
}

func TestUsagesMatchStepKeepsTheQueryAndWraps(t *testing.T) {
	m := filterModel(t)
	setFilter(t, m, "file:web")
	m.filter.Focus()
	if got := len(previews(m)); got != 1 {
		t.Fatalf("the filter must leave one hit, got %d", got)
	}
	st := m.NextMatch() // the only match: any step wraps onto it
	if !st.Wrapped || st.Index != 1 || st.Total != 1 {
		t.Fatalf("the wrap = %+v", st)
	}
	if m.filter.Text() != "file:web" {
		t.Fatalf("the query must survive the walk, got %q", m.filter.Text())
	}
	if got := m.filter.View(120, nil); !strings.Contains(got, "(wrapped)") {
		t.Fatalf("the filter row must mark the wrap: %q", got)
	}
}

func TestUsagesMatchStepWithoutTheFilterRow(t *testing.T) {
	m := filterModel(t)
	if st := m.NextMatch(); st.Handled {
		t.Fatalf("NextMatch with the row unfocused = %+v, want not handled", st)
	}
}

func TestUsagesMatchStepWithNoMatches(t *testing.T) {
	m := filterModel(t)
	setFilter(t, m, "nothing-matches-this")
	m.filter.Focus()
	if st := m.NextMatch(); !st.Handled || st.Total != 0 {
		t.Fatalf("NextMatch over nothing = %+v", st)
	}
}
