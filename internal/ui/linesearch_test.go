package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// linesearch_test.go covers the shared in-pane search (#2461): the prompt's
// open/accept/cancel life cycle, live narrowing that keeps the current match
// put, the #2410 fresh-walk rule, enter's landing rule, stepping both ways
// with wrap, the position-anchored variants the terminal and the explorer
// use, the rendered prompt row, and the smartcase rule.

func lsType(s *LineSearch, text string) {
	for _, r := range text {
		s.Key(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestLineSearchStartKeepsTheLastQuery(t *testing.T) {
	var s LineSearch
	if s.Active() {
		t.Fatal("the zero value must be idle")
	}
	s.Start()
	lsType(&s, "abc")
	s.Key(tea.KeyPressMsg{Code: tea.KeyLeft})
	if _, _, action := s.Key(tea.KeyPressMsg{Code: tea.KeyEnter}); action != SearchAccept {
		t.Fatalf("enter must report accept, got %v", action)
	}
	if s.Open || s.Text != "abc" || !s.Active() {
		t.Fatalf("enter must close and keep the query: open=%v text=%q", s.Open, s.Text)
	}
	s.Start()
	if !s.Open || s.Text != "abc" || s.Field.Cur != 3 {
		t.Fatalf("reopen must keep the query with the caret at its end: %+v", s)
	}
}

func TestLineSearchEscResets(t *testing.T) {
	var s LineSearch
	s.Start()
	lsType(&s, "abc")
	s.SetMatches([]int{1, 2})
	handled, _, action := s.Key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !handled || action != SearchCancel {
		t.Fatalf("esc must report cancel, got handled=%v action=%v", handled, action)
	}
	if s.Open || s.Text != "" || len(s.Matches) != 0 || s.Active() {
		t.Fatalf("esc must drop the search: %+v", s)
	}
}

func TestLineSearchKeyEditsThroughTheField(t *testing.T) {
	var s LineSearch
	s.Start()
	handled, changed, action := s.Key(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if !handled || !changed || action != SearchNone {
		t.Fatalf("typing = handled %v changed %v action %v", handled, changed, action)
	}
	handled, changed, _ = s.Key(tea.KeyPressMsg{Code: tea.KeyLeft})
	if !handled || changed {
		t.Fatalf("a motion is handled but not a change: %v %v", handled, changed)
	}
	if handled, _, _ := s.Key(tea.KeyPressMsg{Code: 'g', Mod: tea.ModSuper, Text: "g"}); handled {
		t.Fatal("a chord the field does not bind is the caller's")
	}
	s.Wrapped = true
	s.Key(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if s.Wrapped {
		t.Fatal("an edited query must start a fresh walk (#2410)")
	}
	s.Wrapped = true
	if !s.Paste("xy") || s.Wrapped || s.Text != "bxya" {
		t.Fatalf("paste must insert and drop the wrap marker: %+v", s)
	}
}

func TestLineSearchSetMatchesKeepsTheNearestSurvivingMatch(t *testing.T) {
	s := LineSearch{Field: NewField("q")}
	s.SetMatches([]int{2, 5, 9})
	if s.Cur != 0 {
		t.Fatalf("the first install lands on the first match, got %d", s.Cur)
	}
	s.Cur = 1 // on 5
	s.Wrapped = true
	s.SetMatches([]int{2, 7, 9})
	if p, _ := s.Current(); p != 7 || s.Wrapped {
		t.Fatalf("narrowing must land on the nearest surviving position at or after 5: %d (wrapped %v)", p, s.Wrapped)
	}
	s.SetMatches([]int{1})
	if s.Cur != 0 {
		t.Fatalf("with nothing at or after, the first match: %d", s.Cur)
	}
	s.SetMatches(nil)
	if s.Cur != -1 || !s.Miss() {
		t.Fatalf("no matches: cur %d miss %v", s.Cur, s.Miss())
	}
	s.Field.Clear()
	if s.Miss() {
		t.Fatal("an empty query is not a miss")
	}
}

func TestLineSearchRecompute(t *testing.T) {
	rows := []string{"alpha", "Beta", "gamma", "beta"}
	var s LineSearch
	s.Recompute(len(rows), func(i int) bool { return SmartCaseContains(s.Text, rows[i]) })
	if len(s.Matches) != 0 {
		t.Fatal("an empty query matches nothing")
	}
	s.Set("beta")
	s.Recompute(len(rows), func(i int) bool { return SmartCaseContains(s.Text, rows[i]) })
	if len(s.Matches) != 2 || s.Matches[0] != 1 || s.Matches[1] != 3 {
		t.Fatalf("matches = %v, want [1 3]", s.Matches)
	}
}

func TestLineSearchApplyLandsAtOrAfter(t *testing.T) {
	var s LineSearch
	if s.Apply(0) {
		t.Fatal("nothing to apply without matches")
	}
	s.SetMatches([]int{2, 5, 9})
	s.Apply(5)
	if s.Cur != 1 {
		t.Fatalf("at 5 lands on 5, got index %d", s.Cur)
	}
	s.Apply(6)
	if s.Cur != 2 {
		t.Fatalf("after 5 lands on 9, got index %d", s.Cur)
	}
	s.Apply(10)
	if s.Cur != 0 {
		t.Fatalf("past the last match wraps to the first, got index %d", s.Cur)
	}
}

func TestLineSearchStepWraps(t *testing.T) {
	var s LineSearch
	if st := s.Step(1); !st.Handled || st.Total != 0 {
		t.Fatalf("a step over nothing is a handled no-op, got %+v", st)
	}
	s.SetMatches([]int{2, 5, 9})
	st := s.Step(1)
	if st.Index != 2 || st.Wrapped {
		t.Fatalf("step = %+v", st)
	}
	s.Step(1)
	st = s.Step(1)
	if st.Index != 1 || !st.Wrapped || !s.Wrapped {
		t.Fatalf("stepping off the end must wrap: %+v", st)
	}
	st = s.Step(-1)
	if st.Index != 3 || !st.Wrapped {
		t.Fatalf("stepping back off the start must wrap: %+v", st)
	}
	s.Cur = -1
	if st := s.Step(-1); st.Index != 3 || st.Wrapped {
		t.Fatalf("from no current match backward lands on the last without a wrap: %+v", st)
	}
}

func TestLineSearchStepFromAndLocate(t *testing.T) {
	var s LineSearch
	if _, st := s.StepFrom(0, 1); !st.Handled || st.Total != 0 {
		t.Fatalf("StepFrom over nothing = %+v", st)
	}
	s.SetMatches([]int{2, 5, 9})
	val, st := s.StepFrom(3, 1)
	if val != 5 || st.Index != 2 || st.Wrapped {
		t.Fatalf("from 3 forward = %d %+v", val, st)
	}
	val, st = s.StepFrom(9, 1)
	if val != 2 || !st.Wrapped || s.Cur != 0 {
		t.Fatalf("from the last forward wraps = %d %+v cur %d", val, st, s.Cur)
	}
	val, st = s.StepFrom(2, -1)
	if val != 9 || !st.Wrapped {
		t.Fatalf("from the first backward wraps = %d %+v", val, st)
	}
	if !s.Locate(5) || s.Cur != 1 {
		t.Fatalf("Locate(5): cur %d", s.Cur)
	}
	if s.Locate(6) || s.Cur != -1 {
		t.Fatalf("Locate off a match: cur %d", s.Cur)
	}
	if st := s.Stat(); st.Index != 1 || st.Total != 3 {
		t.Fatalf("Stat with no current match reports the first: %+v", st)
	}
}

func TestLineSearchLine(t *testing.T) {
	var s LineSearch
	s.Start()
	if got := ansi.Strip(s.Line()); got != "/ " {
		t.Fatalf("an empty open prompt is the slash and the caret cell, got %q", got)
	}
	lsType(&s, "ab")
	if got := ansi.Strip(s.Line()); got != "/ab   no matches" {
		t.Fatalf("a miss reads %q", got)
	}
	s.SetMatches([]int{1, 4})
	s.Step(1)
	if got := ansi.Strip(s.Line()); got != "/ab   2/2" {
		t.Fatalf("the counter reads %q", got)
	}
	s.Step(1)
	if got := ansi.Strip(s.Line()); !strings.HasSuffix(got, "1/2 (wrapped)") {
		t.Fatalf("the wrap marker reads %q", got)
	}
	s.Close()
	if got := ansi.Strip(s.Line()); got != "/ab  1/2 (wrapped)" {
		t.Fatalf("the closed prompt drops the caret: %q", got)
	}
	if s.Counter() != "1/2 (wrapped)" {
		t.Fatalf("Counter = %q", s.Counter())
	}
	s.Reset()
	if s.Counter() != "" {
		t.Fatalf("an empty query has no counter, got %q", s.Counter())
	}
}

func TestSmartCaseContains(t *testing.T) {
	cases := []struct {
		pattern, text string
		want          bool
	}{
		{"", "anything", false},
		{"abc", "xxABCxx", true},
		{"abc", "xxabcxx", true},
		{"Abc", "xxabcxx", false},
		{"Abc", "xxAbcxx", true},
		{"ß", "STRASSE", false},
	}
	for _, c := range cases {
		if got := SmartCaseContains(c.pattern, c.text); got != c.want {
			t.Errorf("SmartCaseContains(%q, %q) = %v, want %v", c.pattern, c.text, got, c.want)
		}
	}
	if p, tx := SmartCaseFold("ab", "xAB"); p != "ab" || tx != "xab" {
		t.Fatalf("Fold lowercases text under a lowercase pattern: %q %q", p, tx)
	}
	if p, tx := SmartCaseFold("Ab", "xAB"); p != "Ab" || tx != "xAB" {
		t.Fatalf("Fold keeps both exact under an uppercase pattern: %q %q", p, tx)
	}
}
