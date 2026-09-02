package problems

// matchstep_test.go covers #2410 for the Problems pane. Its search is the
// shared filter row, so its "matches" are the findings the filter left
// standing: cmd+g walks them while the row keeps the keyboard and the
// expression stays editable, skipping the file headers.

import (
	"strings"
	"testing"
)

func TestProblemsMatchStepWalksTheFindings(t *testing.T) {
	m := filterModel(t)
	m.filter.Focus()
	m.cursor = 0
	hits := len(messages(m))
	if hits != 3 {
		t.Fatalf("the fixture must hold three findings, got %d", hits)
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

func TestProblemsMatchStepKeepsTheQueryAndWraps(t *testing.T) {
	m := filterModel(t)
	setFilter(t, m, "sev:error")
	m.filter.Focus()
	if got := len(messages(m)); got != 2 {
		t.Fatalf("the filter must leave two findings, got %d", got)
	}
	if st := m.NextMatch(); st.Index != 2 || st.Wrapped {
		t.Fatalf("the first step = %+v, want 2/2", st)
	}
	st := m.NextMatch() // off the end
	if !st.Wrapped || st.Index != 1 || st.Total != 2 {
		t.Fatalf("the wrap = %+v", st)
	}
	if m.filter.Text() != "sev:error" {
		t.Fatalf("the query must survive the walk, got %q", m.filter.Text())
	}
	if got := m.filter.View(120, nil); !strings.Contains(got, "(wrapped)") {
		t.Fatalf("the filter row must mark the wrap: %q", got)
	}
}

func TestProblemsMatchStepWithoutTheFilterRow(t *testing.T) {
	m := filterModel(t)
	if st := m.NextMatch(); st.Handled {
		t.Fatalf("NextMatch with the row unfocused = %+v, want not handled", st)
	}
}

func TestProblemsMatchStepWithNoMatches(t *testing.T) {
	m := filterModel(t)
	setFilter(t, m, "nothing-matches-this")
	m.filter.Focus()
	if st := m.NextMatch(); !st.Handled || st.Total != 0 {
		t.Fatalf("NextMatch over nothing = %+v", st)
	}
}
