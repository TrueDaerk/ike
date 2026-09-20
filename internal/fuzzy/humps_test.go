package fuzzy

// humps_test.go covers the JetBrains-style hump matcher (#2650): a matched
// rune either continues the previous match or starts a word segment, and
// the case rule follows completion.case_sensitivity.

import (
	"reflect"
	"testing"
)

func TestMatchHumpsTable(t *testing.T) {
	cases := []struct {
		pattern, text string
		want          bool
		positions     []int // nil = don't check
	}{
		// Positive examples from the issue.
		{"my", "mycelium", true, []int{0, 1}},
		{"my", "MY_CONSTANT", true, []int{0, 1}},
		{"gur", "GotoURLResolver", true, nil},
		{"dao", "DataAccessObject", true, []int{0, 4, 10}},
		{"dacco", "DataAccessObject", true, []int{0, 4, 5, 6, 10}},
		{"DataA", "DataAccessObject", true, []int{0, 1, 2, 3, 4}},
		// Negative examples: mid-word hits that plain Match accepts.
		{"my", "empty", false, nil},
		{"my", "summary", false, nil},
		{"log", "dialogBox", false, nil},
		{"log", "catalogOf", false, nil},
		// Uppercase pattern runes only match uppercase text runes.
		{"DataA", "database", false, nil},
		{"Da", "database", false, nil},
		{"da", "Database", true, []int{0, 1}},
		// Separator boundaries: "_" and "-" and ".".
		{"mc", "MY_CONSTANT", true, []int{0, 3}},
		{"fb", "foo-bar", true, []int{0, 4}},
		{"fb", "foo.bar", true, []int{0, 4}},
		{"ob", "foo-bar", false, nil}, // "o" is mid-word
		// Letter/digit change is a boundary in both directions.
		{"h2", "html2canvas", true, []int{0, 4}},
		{"hc", "html2canvas", true, []int{0, 5}},
		{"v2", "version2", true, []int{0, 7}},
		// Acronym end: the R of "URLResolver" starts a segment.
		{"ur", "URLResolver", true, []int{0, 3}},
		// A segment start may be continued mid-word.
		{"getcn", "getClassName", true, []int{0, 1, 2, 3, 8}},
		{"gCN", "getClassName", true, []int{0, 3, 8}},
		{"gCN", "getCount", false, nil}, // n mid-word, not consecutive
		// Pattern longer than text never matches.
		{"abcd", "abc", false, nil},
	}
	for _, c := range cases {
		r, ok := MatchHumps(c.pattern, c.text)
		if ok != c.want {
			t.Errorf("MatchHumps(%q, %q) ok = %v, want %v (positions %v)", c.pattern, c.text, ok, c.want, r.Positions)
			continue
		}
		if ok && c.positions != nil && !reflect.DeepEqual(r.Positions, c.positions) {
			t.Errorf("MatchHumps(%q, %q) positions = %v, want %v", c.pattern, c.text, r.Positions, c.positions)
		}
	}
}

func TestMatchHumpsEmptyPattern(t *testing.T) {
	r, ok := MatchHumps("", "anything")
	if !ok || r.Score != 0 || r.Positions != nil || !r.Prefix {
		t.Fatalf("empty pattern: want ok, zero score, nil positions, prefix; got %+v %v", r, ok)
	}
}

// TestMatchHumpsPrefixFlag: Prefix is set exactly when the match covers the
// leading runes, so the ranking follow-up can tier prefix over hump matches.
func TestMatchHumpsPrefixFlag(t *testing.T) {
	if r, _ := MatchHumps("data", "DataAccessObject"); !r.Prefix {
		t.Errorf("data → DataAccessObject should be a prefix match: %+v", r)
	}
	if r, _ := MatchHumps("dao", "DataAccessObject"); r.Prefix {
		t.Errorf("dao → DataAccessObject is a hump match, not a prefix: %+v", r)
	}
	if r, _ := Match("apg", "internal/app/app.go"); r.Prefix {
		t.Errorf("scattered Match must not report a prefix: %+v", r)
	}
	if r, _ := Match("int", "internal"); !r.Prefix {
		t.Errorf("leading Match should report a prefix: %+v", r)
	}
}

func TestMatchHumpsCaseModes(t *testing.T) {
	cases := []struct {
		mode          Case
		pattern, text string
		want          bool
	}{
		{CaseFirstLetter, "DataA", "database", false},
		{CaseFirstLetter, "da", "Database", true},
		{CaseNone, "DA", "database", true},
		{CaseNone, "MY", "mycelium", true},
		{CaseAll, "da", "Database", false},
		{CaseAll, "Da", "Database", true},
		{CaseAll, "my", "MY_CONSTANT", false},
		{CaseAll, "MC", "MY_CONSTANT", true},
	}
	for _, c := range cases {
		if _, ok := MatchHumpsCase(c.pattern, c.text, c.mode); ok != c.want {
			t.Errorf("MatchHumpsCase(%q, %q, %v) = %v, want %v", c.pattern, c.text, c.mode, ok, c.want)
		}
	}
}

func TestParseCase(t *testing.T) {
	for s, want := range map[string]Case{"none": CaseNone, "first_letter": CaseFirstLetter, "all": CaseAll, "bogus": CaseFirstLetter, "": CaseFirstLetter} {
		if got := ParseCase(s); got != want {
			t.Errorf("ParseCase(%q) = %v, want %v", s, got, want)
		}
	}
}

// TestMatchUnchangedByHumps: the permissive matcher still accepts the mid-word
// hits the hump matcher rejects, so pickers keep their behaviour.
func TestMatchUnchangedByHumps(t *testing.T) {
	for _, c := range [][2]string{{"my", "empty"}, {"my", "summary"}, {"log", "dialogBox"}, {"DataA", "database"}} {
		if _, ok := Match(c[0], c[1]); !ok {
			t.Errorf("Match(%q, %q) must still match", c[0], c[1])
		}
	}
}

func BenchmarkMatchHumps(b *testing.B) {
	for i := 0; i < b.N; i++ {
		MatchHumps("dacco", "DataAccessObjectFactoryProvider")
	}
}
