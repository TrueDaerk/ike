package editor

import (
	"reflect"
	"testing"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

// completion_humps_test.go guards #2650: the popup filters with the
// JetBrains-style hump matcher, so a typed prefix only keeps candidates whose
// runes continue the previous match or start a word segment, and the case
// rule follows completion.case_sensitivity.

func humpItems() []ilsp.CompletionItem {
	var items []ilsp.CompletionItem
	for _, l := range []string{"mycelium", "MY_CONSTANT", "empty", "summary", "GotoURLResolver", "dialogBox", "catalogOf", "logger", "DataAccessObject", "database"} {
		items = append(items, ilsp.CompletionItem{Label: l, InsertText: l})
	}
	return items
}

func openHumps(t *testing.T, cfg host.Config) Model {
	t.Helper()
	m, _ := loaded(t, "x.\n")
	if cfg != nil {
		m.Configure(cfg)
	}
	m = insertModeAt(m, 0, 2)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 2, Items: humpItems()})
	if !m.CompletionOpen() {
		t.Fatal("completion popup should be open")
	}
	return m
}

func TestCompletionHumpFilterDropsMidWordHits(t *testing.T) {
	m := openHumps(t, nil)
	m = send(m, key('m'), key('y'))
	got := labels(m.filteredCompletion())
	sortStrings(got)
	if want := []string{"MY_CONSTANT", "mycelium"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("my → %v, want %v (no empty, no summary)", got, want)
	}

	m = openHumps(t, nil)
	m = send(m, key('l'), key('o'), key('g'))
	if got := labels(m.filteredCompletion()); !reflect.DeepEqual(got, []string{"logger"}) {
		t.Fatalf("log → %v, want [logger] (no dialogBox, no catalogOf)", got)
	}

	m = openHumps(t, nil)
	m = send(m, key('g'), key('u'), key('r'))
	if got := labels(m.filteredCompletion()); !reflect.DeepEqual(got, []string{"GotoURLResolver"}) {
		t.Fatalf("gur → %v, want [GotoURLResolver]", got)
	}
}

// TestCompletionHumpCaseRule: under the default first_letter rule an
// uppercase typed rune only matches an uppercase label rune; "none" folds
// it, "all" also holds lowercase runes to their case.
func TestCompletionHumpCaseRule(t *testing.T) {
	typeDatab := func(m Model) []string {
		m = send(m, key('D'), key('a'), key('t'), key('a'), key('b'))
		return labels(m.filteredCompletion())
	}
	if got := typeDatab(openHumps(t, nil)); len(got) != 0 {
		t.Fatalf("first_letter: Datab → %v, want none (uppercase D cannot match database)", got)
	}
	if got := typeDatab(openHumps(t, host.MapConfig{"completion.case_sensitivity": "none"})); !reflect.DeepEqual(got, []string{"database"}) {
		t.Fatalf("none: Datab → %v, want [database]", got)
	}

	m := openHumps(t, host.MapConfig{"completion.case_sensitivity": "all"})
	m = send(m, key('m'), key('y'))
	if got := labels(m.filteredCompletion()); !reflect.DeepEqual(got, []string{"mycelium"}) {
		t.Fatalf("all: my → %v, want [mycelium] (MY_CONSTANT is case-mismatched)", got)
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
