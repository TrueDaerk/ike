package editor

import (
	"reflect"
	"testing"

	"ike/internal/complete/mru"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

// completion_tiers_test.go guards #2651: the popup ranks by match tier —
// exact label, case-exact prefix, prefix under the case rule, hump match —
// and only orders within a tier by MRU, locality, length, source priority
// and the server's sortText.

// openTiers opens a popup fed by a server batch and a words batch so the
// tiers are exercised across sources.
func openTiers(t *testing.T, lspItems, wordItems []ilsp.CompletionItem) Model {
	t.Helper()
	m, _ := loaded(t, "x.\n")
	m = insertModeAt(m, 0, 2)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 2, Source: ilsp.SourceLSP, SourcePriority: ilsp.PriorityLSP, Items: lspItems})
	if len(wordItems) > 0 {
		m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 2, Source: "words", SourcePriority: ilsp.PriorityWords, Items: wordItems})
	}
	if !m.CompletionOpen() {
		t.Fatal("completion popup should be open")
	}
	return m
}

func item(label string, extra ...func(*ilsp.CompletionItem)) ilsp.CompletionItem {
	it := ilsp.CompletionItem{Label: label, InsertText: label}
	for _, f := range extra {
		f(&it)
	}
	return it
}

func typed(m Model, s string) []string {
	for _, r := range s {
		m = send(m, key(r))
	}
	return labels(m.filteredCompletion())
}

// TestCompletionTierPrefixBeatsHump: an exact prefix ranks above a scattered
// CamelCase hit whose fuzzy score is higher, an exact label above a prefix
// match, and a case-exact prefix above a folded one.
func TestCompletionTierPrefixBeatsHump(t *testing.T) {
	m := openTiers(t,
		[]ilsp.CompletionItem{item("DumpAllTablesAgain", withSort("0001")), item("DataAccessObject", withSort("0002"))},
		[]ilsp.CompletionItem{item("dao"), item("database")})
	if got := typed(m, "data"); !reflect.DeepEqual(got, []string{"database", "DataAccessObject", "DumpAllTablesAgain"}) {
		t.Fatalf("data → %v, want prefix tiers before the hump match", got)
	}
	if got := typed(m, "dao"); !reflect.DeepEqual(got, []string{"dao", "DataAccessObject"}) {
		t.Fatalf("dao → %v, want the exact label first", got)
	}

	m = openTiers(t, []ilsp.CompletionItem{item("MY_CONSTANT", withSort("0001")), item("mycelium", withSort("0002"))}, nil)
	if got := typed(m, "my"); !reflect.DeepEqual(got, []string{"mycelium", "MY_CONSTANT"}) {
		t.Fatalf("my → %v, want the case-exact prefix first", got)
	}
	m = openTiers(t, []ilsp.CompletionItem{item("Data", withSort("0001")), item("data", withSort("0002"))}, nil)
	if got := typed(m, "data"); !reflect.DeepEqual(got, []string{"data", "Data"}) {
		t.Fatalf("data → %v, want the case-exact label first", got)
	}
}

// TestCompletionTierKeepsServerOrder: two server items in one tier list in
// sortText order even when their fuzzy scores would reverse it, and the
// shorter label comes first before sortText is consulted.
func TestCompletionTierKeepsServerOrder(t *testing.T) {
	m := openTiers(t, []ilsp.CompletionItem{
		item("gxVOxxxxxxx", withSort("0002")), // hump then consecutive hit: higher fuzzy score
		item("getValueOfX", withSort("0001")), // scattered humps: fuzzy 54, same length
	}, nil)
	if got := typed(m, "gVO"); !reflect.DeepEqual(got, []string{"getValueOfX", "gxVOxxxxxxx"}) {
		t.Fatalf("gVO → %v, want sortText order inside the hump tier", got)
	}
	m = openTiers(t, []ilsp.CompletionItem{
		item("fooBarBaz", withSort("0001")),
		item("fooBar", withSort("0002")),
		item("fooBarQux", withSort("0003")),
	}, nil)
	if got := typed(m, "foo"); !reflect.DeepEqual(got, []string{"fooBar", "fooBarBaz", "fooBarQux"}) {
		t.Fatalf("foo → %v, want shorter first, then sortText", got)
	}
}

// TestCompletionTierMRUWithinTier: a recently accepted item tops its own tier
// but cannot climb into a better one.
func TestCompletionTierMRUWithinTier(t *testing.T) {
	store := mru.Load("")
	open := func() Model {
		m := openTiers(t, []ilsp.CompletionItem{
			item("fooAlpha", withSort("0001")),
			item("fooOmega", withSort("0002")),
			item("fetchOrOpen", withSort("0003")), // hump match for "foo"
		}, nil)
		m.SetCompletionMRU(store)
		return m
	}
	store.Bump(open().mruScope(), "fetchOrOpen")
	store.Bump(open().mruScope(), "fooOmega")
	if got := typed(open(), "foo"); !reflect.DeepEqual(got, []string{"fooOmega", "fooAlpha", "fetchOrOpen"}) {
		t.Fatalf("foo → %v, want MRU first inside the prefix tier, hump tier last", got)
	}
}

// TestCompletionTierCaseRuleAll: under completion.case_sensitivity "all" the
// folded tiers never arise; a case-mismatched prefix is simply not offered.
func TestCompletionTierCaseRuleAll(t *testing.T) {
	m, _ := loaded(t, "x.\n")
	m.Configure(host.MapConfig{"completion.case_sensitivity": "all"})
	m = insertModeAt(m, 0, 2)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 2, Items: []ilsp.CompletionItem{item("Data"), item("data")}})
	if got := typed(m, "data"); !reflect.DeepEqual(got, []string{"data"}) {
		t.Fatalf("all: data → %v, want [data]", got)
	}
}

func withSort(s string) func(*ilsp.CompletionItem) {
	return func(it *ilsp.CompletionItem) { it.SortText = s }
}
