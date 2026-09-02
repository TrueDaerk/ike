package ghissues

// matchstep_test.go covers #2410 for the Issues pane. Its search is the
// filter overlay's match input, so its "matches" are the issues the pattern
// left standing: cmd+g walks them behind the open overlay while the input
// keeps the keyboard and the pattern stays editable.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// typeMatch opens the overlay on its match row and types the pattern.
func typeMatch(m *Model, pattern string) {
	m.OpenSearch()
	for _, r := range pattern {
		press(m, string(r))
	}
}

func TestIssuesMatchStepWalksTheNarrowedList(t *testing.T) {
	m := filled(t)
	typeMatch(m, "explorer")
	if !m.Filtering() {
		t.Fatal("the match input must hold the keyboard")
	}
	st := m.NextMatch()
	if !st.Handled || st.Total != 2 {
		t.Fatalf("NextMatch = %+v, want the two explorer issues", st)
	}
	if !m.Filtering() || m.fInput != "explorer" {
		t.Fatalf("the input must survive the step: filtering=%v input=%q",
			m.Filtering(), m.fInput)
	}
}

func TestIssuesMatchStepWrapsAndSaysSo(t *testing.T) {
	m := filled(t)
	typeMatch(m, "explorer")
	m.NextMatch()
	st := m.NextMatch() // off the end of the two hits
	if !st.Wrapped {
		t.Fatalf("the walk must wrap, got %+v", st)
	}
	if got := ansi.Strip(m.View()); !strings.Contains(got, "(wrapped)") {
		t.Fatalf("the match row must mark the wrap:\n%s", got)
	}
}

func TestIssuesMatchStepWithoutTheOverlay(t *testing.T) {
	m := filled(t)
	if st := m.NextMatch(); st.Handled {
		t.Fatalf("NextMatch with the overlay closed = %+v, want not handled", st)
	}
}

func TestIssuesMatchStepWithNoMatches(t *testing.T) {
	m := filled(t)
	typeMatch(m, "zzzznothing")
	if st := m.NextMatch(); !st.Handled || st.Total != 0 {
		t.Fatalf("NextMatch over nothing = %+v", st)
	}
}

func TestIssuesEditDropsTheWrapMarker(t *testing.T) {
	m := filled(t)
	typeMatch(m, "explorer")
	m.NextMatch()
	m.NextMatch() // wraps
	press(m, "x")
	if m.matchStatus != "" {
		t.Fatalf("an edited pattern must start a fresh walk, status = %q", m.matchStatus)
	}
}
