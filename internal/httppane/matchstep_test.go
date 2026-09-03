package httppane

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// matchstep_test.go covers #2410 for the response viewer: cmd+g steps the
// matches while the "/" prompt still holds the keyboard — n and N are query
// text there, which is why the chord exists — and n/N keep working after
// enter blurs the prompt, for the vim hands that learned them (#1265).

func TestHTTPNextMatchWithThePromptOpen(t *testing.T) {
	m := searchViewer(t)
	typeSearch(m, "token")
	cur, total := m.MatchPosition()
	if cur != 1 || total != 4 {
		t.Fatalf("match position = %d/%d, want 1/4", cur, total)
	}
	st := m.NextMatch()
	if !st.Handled || st.Index != 2 || st.Total != 4 || st.Wrapped {
		t.Fatalf("NextMatch = %+v", st)
	}
	if q, open := m.SearchQuery(); q != "token" || !open {
		t.Fatalf("the prompt must survive the step: %q open=%v", q, open)
	}
}

func TestHTTPPrevMatchWrapsAndSaysSo(t *testing.T) {
	m := searchViewer(t)
	typeSearch(m, "token")
	st := m.PrevMatch()
	if !st.Wrapped || st.Index != st.Total {
		t.Fatalf("stepping back off the first match = %+v", st)
	}
	if got := m.footerText(); !strings.Contains(got, "(wrapped)") {
		t.Fatalf("the footer must mark the wrap: %q", got)
	}
}

// TestHTTPMatchStepAfterEnter: the applied pattern is still the pane's
// search, so the chord keeps stepping it once the prompt has blurred.
func TestHTTPMatchStepAfterEnter(t *testing.T) {
	m := searchViewer(t)
	typeSearch(m, "token")
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, open := m.SearchQuery(); open {
		t.Fatal("enter must blur the prompt")
	}
	if st := m.NextMatch(); !st.Handled || st.Total != 4 {
		t.Fatalf("NextMatch after enter = %+v", st)
	}
	// n keeps its vim meaning there too.
	before := m.search.Cur
	m.handleKey(keyPress("n"))
	if m.search.Cur == before {
		t.Fatal("n must still step the match after the prompt blurred")
	}
}

func TestHTTPMatchStepWithoutASearch(t *testing.T) {
	m := searchViewer(t)
	if st := m.NextMatch(); st.Handled {
		t.Fatalf("NextMatch with no search = %+v, want not handled", st)
	}
}

func TestHTTPMatchStepWithNoMatches(t *testing.T) {
	m := searchViewer(t)
	typeSearch(m, "zzzz")
	if st := m.NextMatch(); !st.Handled || st.Total != 0 {
		t.Fatalf("NextMatch over nothing = %+v", st)
	}
	if got := m.footerText(); !strings.Contains(got, "no matches") {
		t.Fatalf("footer = %q", got)
	}
}

func TestHTTPEditDropsTheWrapMarker(t *testing.T) {
	m := searchViewer(t)
	typeSearch(m, "token")
	m.PrevMatch()
	m.handleKey(keyPress("-"))
	if m.search.Wrapped {
		t.Fatal("an edited query must start a fresh walk")
	}
}
