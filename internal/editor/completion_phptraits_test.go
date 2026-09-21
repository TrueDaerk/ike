package editor

import (
	"reflect"
	"sort"
	"testing"

	ilsp "ike/internal/lsp"
)

// completion_phptraits_test.go guards the merge side of #2668: the PHP
// trait-member source sits below the server, so a member both offer appears
// once — with the server's item — while the members only the trait source
// knows (the ones living on the trait's consumers) are added below.

// openTraitPopup feeds a server batch and a phptraits batch at the same
// position, the way the engine and the LSP bridge do.
func openTraitPopup(t *testing.T, lspItems, traitItems []ilsp.CompletionItem) Model {
	t.Helper()
	m, _ := loaded(t, "x.\n")
	m = insertModeAt(m, 0, 2)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 2, Source: ilsp.SourceLSP, SourcePriority: ilsp.PriorityLSP, Items: lspItems})
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 2, Source: "phptraits", SourcePriority: ilsp.PriorityPHPTraits, Items: traitItems})
	if !m.CompletionOpen() {
		t.Fatal("completion popup should be open")
	}
	return m
}

// TestTraitMembersDedupeAgainstTheServer: the label the server already knows
// keeps the server's item; the consumer-only members are added.
func TestTraitMembersDedupeAgainstTheServer(t *testing.T) {
	m := openTraitPopup(t,
		[]ilsp.CompletionItem{{Label: "run(", InsertText: "run(", Detail: "from the server"}},
		[]ilsp.CompletionItem{
			{Label: "run(", InsertText: "run(", FilterText: "run(", Detail: "run(): void  —  trait A", SortText: "0000"},
			{Label: "abc(", InsertText: "abc(", FilterText: "abc(", Detail: "abc(int $times = 1): string  —  class B", SortText: "0001"},
		})
	got := append([]string(nil), labels(m.filteredCompletion())...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"abc(", "run("}) {
		t.Fatalf("merged items = %v, want the server's run( once plus the trait source's abc(", got)
	}
	for _, it := range m.filteredCompletion() {
		if it.Label == "run(" && it.Detail != "from the server" {
			t.Errorf("run( detail = %q, want the server's item to win the duplicate", it.Detail)
		}
		if it.Label == "abc(" && it.Detail == "" {
			t.Error("abc( lost the trait source's detail")
		}
	}
}

// TestTraitMemberFiltersByTypedPrefix: the partial identifier the source was
// dispatched with keeps filtering client-side, and the `(` of `abc(` does not
// get in the way.
func TestTraitMemberFiltersByTypedPrefix(t *testing.T) {
	m := openTraitPopup(t, nil, []ilsp.CompletionItem{
		{Label: "abc(", InsertText: "abc(", FilterText: "abc(", SortText: "0000"},
		{Label: "fromC(", InsertText: "fromC(", FilterText: "fromC(", SortText: "0001"},
		{Label: "x", InsertText: "x", FilterText: "x", SortText: "0002"},
	})
	if got := typed(m, "ab"); !reflect.DeepEqual(got, []string{"abc("}) {
		t.Fatalf("ab → %v, want only abc(", got)
	}
}
