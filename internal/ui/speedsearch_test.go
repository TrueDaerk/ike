package ui

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// runeKey builds the key press a printable rune arrives as.
func runeKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// typeInto feeds a whole string to a search, rune by rune.
func typeInto(s *SpeedSearch, text string) {
	for _, r := range text {
		s.Key(runeKey(r))
	}
}

func TestSpeedSearchIdleMatchesEverything(t *testing.T) {
	var s SpeedSearch
	if s.Active() {
		t.Fatal("the zero value must be idle")
	}
	rows := []string{"bug", "feature", "docs"}
	if got := NarrowStrings(&s, rows); !reflect.DeepEqual(got, rows) {
		t.Fatalf("an idle search must keep every row, got %v", got)
	}
	if got := s.Filter(rows); !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Fatalf("an idle search must return every index, got %v", got)
	}
	if s.Hint() != "" {
		t.Fatalf("an idle search must render no hint, got %q", s.Hint())
	}
}

func TestSpeedSearchNarrowsAndKeepsRowOrder(t *testing.T) {
	var s SpeedSearch
	rows := []string{"area/editor", "bug", "documentation", "enhancement", "good first issue"}
	typeInto(&s, "e")
	got := NarrowStrings(&s, rows)
	want := []string{"area/editor", "documentation", "enhancement", "good first issue"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("typing 'e' must narrow to %v in row order, got %v", want, got)
	}
	typeInto(&s, "nh")
	if got := NarrowStrings(&s, rows); !reflect.DeepEqual(got, []string{"enhancement"}) {
		t.Fatalf("'enh' must narrow to enhancement alone, got %v", got)
	}
	if s.Query() != "enh" {
		t.Fatalf("query = %q, want enh", s.Query())
	}
}

func TestSpeedSearchIsCaseInsensitive(t *testing.T) {
	var s SpeedSearch
	typeInto(&s, "BU")
	if got := NarrowStrings(&s, []string{"bug", "docs"}); !reflect.DeepEqual(got, []string{"bug"}) {
		t.Fatalf("the query must fold case, got %v", got)
	}
}

func TestSpeedSearchNeverTakesSpace(t *testing.T) {
	var s SpeedSearch
	typeInto(&s, "bu")
	for _, msg := range []tea.KeyPressMsg{
		{Code: tea.KeySpace, Text: " "},
		{Code: ' ', Text: " "},
	} {
		if handled, _ := s.Key(msg); handled {
			t.Fatalf("space belongs to the picker's toggle, %v was consumed", msg)
		}
	}
	if s.Query() != "bu" {
		t.Fatalf("space must leave the query alone, got %q", s.Query())
	}
}

func TestSpeedSearchIgnoresChords(t *testing.T) {
	var s SpeedSearch
	for _, msg := range []tea.KeyPressMsg{
		{Code: 'n', Text: "n", Mod: tea.ModCtrl},
		{Code: 'd', Text: "d", Mod: tea.ModAlt},
		{Code: tea.KeyEnter},
		{Code: tea.KeyDown},
		{Code: tea.KeyEscape},
	} {
		if handled, _ := s.Key(msg); handled {
			t.Fatalf("%v must stay with the picker", msg)
		}
	}
	if s.Active() {
		t.Fatalf("no chord may start a query, got %q", s.Query())
	}
}

func TestSpeedSearchBackspacePeelsThenFallsThrough(t *testing.T) {
	var s SpeedSearch
	typeInto(&s, "bug")
	bs := tea.KeyPressMsg{Code: tea.KeyBackspace}
	for _, want := range []string{"bu", "b", ""} {
		handled, changed := s.Key(bs)
		if !handled || !changed {
			t.Fatalf("backspace must peel a running query (handled=%v changed=%v)", handled, changed)
		}
		if s.Query() != want {
			t.Fatalf("query = %q, want %q", s.Query(), want)
		}
	}
	// With the query empty backspace is the picker's again — that is how
	// "clear the whole selection" stays reachable.
	if handled, _ := s.Key(bs); handled {
		t.Fatal("backspace on an empty query must fall through to the picker")
	}
}

func TestSpeedSearchEscClearsOnceThenReleases(t *testing.T) {
	var s SpeedSearch
	typeInto(&s, "doc")
	if !s.EscClears() {
		t.Fatal("the first esc must clear the query")
	}
	if s.Active() {
		t.Fatalf("the query must be gone, got %q", s.Query())
	}
	if s.EscClears() {
		t.Fatal("the second esc must fall through so the modal closes")
	}
}

func TestSpeedSearchResetDropsTheQuery(t *testing.T) {
	var s SpeedSearch
	typeInto(&s, "x")
	s.Reset()
	if s.Active() || s.Query() != "" {
		t.Fatalf("Reset must idle the search, got %q", s.Query())
	}
}

func TestSpeedSearchNarrowUsesTheLabelFunc(t *testing.T) {
	type row struct{ key, text string }
	rows := []row{{"o", "Open in browser"}, {"r", "Refresh the listing"}, {"s", "Start work"}}
	var s SpeedSearch
	typeInto(&s, "brow")
	got := Narrow(&s, rows, func(r row) string { return r.key + " " + r.text })
	if len(got) != 1 || got[0].key != "o" {
		t.Fatalf("the label func must decide the match, got %v", got)
	}
	// A query nothing matches yields an empty set, never the whole list.
	s.Reset()
	typeInto(&s, "zzz")
	if got := Narrow(&s, rows, func(r row) string { return r.text }); len(got) != 0 {
		t.Fatalf("a fruitless query must narrow to nothing, got %v", got)
	}
}

func TestSpeedSearchHintShowsTheQuery(t *testing.T) {
	var s SpeedSearch
	typeInto(&s, "bug")
	if got := s.Hint(); got != "/bug▏" {
		t.Fatalf("Hint() = %q, want /bug▏", got)
	}
}

func TestSpeedSearchFilterReturnsSourceIndices(t *testing.T) {
	var s SpeedSearch
	typeInto(&s, "d")
	got := s.Filter([]string{"bug", "docs", "feature", "duplicate"})
	if !reflect.DeepEqual(got, []int{1, 3}) {
		t.Fatalf("Filter must index the source rows, got %v", got)
	}
}

// The word-edit chords a running query owes the user (#2360): before it, the
// type-ahead rejected every modifier chord, so an alt+backspace typed inside
// a picker fell through to the keymap and was logged unbound.
func TestSpeedSearchRunningQueryEditsThroughEditKey(t *testing.T) {
	cases := []struct {
		name  string
		keys  []tea.KeyPressMsg
		query string
		cur   int
	}{
		{"alt+backspace", []tea.KeyPressMsg{{Code: tea.KeyBackspace, Mod: tea.ModAlt}}, "area/", 5},
		{"ctrl+w", []tea.KeyPressMsg{{Code: 'w', Mod: tea.ModCtrl}}, "area/", 5},
		{"alt+delete deletes the word right", []tea.KeyPressMsg{
			{Code: tea.KeyLeft, Mod: tea.ModAlt},
			{Code: tea.KeyDelete, Mod: tea.ModAlt}}, "area/", 5},
		{"super+backspace", []tea.KeyPressMsg{{Code: tea.KeyBackspace, Mod: tea.ModSuper}}, "", 0},
		{"alt+left", []tea.KeyPressMsg{{Code: tea.KeyLeft, Mod: tea.ModAlt}}, "area/editor", 5},
		{"ctrl+left", []tea.KeyPressMsg{{Code: tea.KeyLeft, Mod: tea.ModCtrl}}, "area/editor", 5},
		{"alt+right after two lefts", []tea.KeyPressMsg{
			{Code: tea.KeyLeft, Mod: tea.ModAlt}, {Code: tea.KeyLeft, Mod: tea.ModAlt},
			{Code: tea.KeyRight, Mod: tea.ModAlt}}, "area/editor", 4},
		{"ctrl+right after two lefts", []tea.KeyPressMsg{
			{Code: tea.KeyLeft, Mod: tea.ModAlt}, {Code: tea.KeyLeft, Mod: tea.ModAlt},
			{Code: tea.KeyRight, Mod: tea.ModCtrl}}, "area/editor", 4},
		{"left", []tea.KeyPressMsg{{Code: tea.KeyLeft}}, "area/editor", 10},
		{"right at the end", []tea.KeyPressMsg{{Code: tea.KeyRight}}, "area/editor", 11},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var s SpeedSearch
			typeInto(&s, "area/editor")
			for _, msg := range tc.keys {
				if handled, _ := s.Key(msg); !handled {
					t.Fatalf("%v must not leak past a running query", msg)
				}
			}
			if s.Query() != tc.query || s.Cursor() != tc.cur {
				t.Fatalf("query = %q cur = %d, want %q %d", s.Query(), s.Cursor(), tc.query, tc.cur)
			}
		})
	}
}

// An idle search still starts on printable runes only: every chord — and
// backspace itself — belongs to the picker while nothing is typed.
func TestSpeedSearchIdleLeavesTheChordsToThePicker(t *testing.T) {
	for _, msg := range []tea.KeyPressMsg{
		{Code: tea.KeyBackspace, Mod: tea.ModAlt},
		{Code: tea.KeyBackspace, Mod: tea.ModSuper},
		{Code: tea.KeyLeft, Mod: tea.ModCtrl},
		{Code: tea.KeyRight, Mod: tea.ModAlt},
		{Code: tea.KeyLeft},
	} {
		var s SpeedSearch
		if handled, _ := s.Key(msg); handled {
			t.Fatalf("%v must stay with the picker while the search is idle", msg)
		}
		if s.Active() {
			t.Fatalf("%v must not start a query", msg)
		}
	}
}

// The keys a host binds to list actions stay the host's even mid-query.
func TestSpeedSearchReservesTheHostKeys(t *testing.T) {
	for _, msg := range []tea.KeyPressMsg{
		{Code: tea.KeySpace, Text: " "},
		{Code: tea.KeyDelete},
		{Code: tea.KeyHome},
		{Code: tea.KeyEnd},
		{Code: tea.KeyLeft, Mod: tea.ModSuper},
		{Code: tea.KeyRight, Mod: tea.ModSuper},
		{Code: tea.KeyTab, Text: "\t"},
	} {
		var s SpeedSearch
		typeInto(&s, "bug")
		if handled, _ := s.Key(msg); handled {
			t.Fatalf("%v must stay with the picker, the query took it", msg)
		}
		if s.Query() != "bug" {
			t.Fatalf("%v changed the query to %q", msg, s.Query())
		}
	}
}

// Typing lands at the caret, not at the end.
func TestSpeedSearchTypesAtTheCaret(t *testing.T) {
	var s SpeedSearch
	typeInto(&s, "dc")
	if handled, _ := s.Key(tea.KeyPressMsg{Code: tea.KeyLeft}); !handled {
		t.Fatal("left must move the caret of a running query")
	}
	typeInto(&s, "o")
	if s.Query() != "doc" || s.Cursor() != 2 {
		t.Fatalf("query = %q cur = %d, want doc 2", s.Query(), s.Cursor())
	}
	if got := s.Hint(); got != "/do▏c" {
		t.Fatalf("Hint() = %q, want /do▏c", got)
	}
}

// Reset and EscClears take the caret with the query.
func TestSpeedSearchResetRewindsTheCaret(t *testing.T) {
	var s SpeedSearch
	typeInto(&s, "bug")
	s.Reset()
	if s.Cursor() != 0 {
		t.Fatalf("Reset must rewind the caret, got %d", s.Cursor())
	}
	typeInto(&s, "bug")
	s.EscClears()
	if s.Cursor() != 0 {
		t.Fatalf("EscClears must rewind the caret, got %d", s.Cursor())
	}
}
