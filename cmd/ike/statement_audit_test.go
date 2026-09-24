package main

import (
	"sort"
	"testing"

	"ike/internal/lang"
)

// statementCompleters is the shipped table of languages implementing the
// Complete Current Statement seam (#2726, lang.StatementCompleter) — the
// list wiki/architecture/editor.md § "Complete statement" prints. A language
// gaining or losing the completer updates both; every other registered
// language deliberately reports "not supported" in the editor.
var statementCompleters = []string{"go", "php", "python", "shell", "typescript"}

func TestStatementCompleterTableIsCurrent(t *testing.T) {
	var got []string
	for _, l := range lang.All() {
		if lang.SupportsStatementCompletion(l.ID) {
			got = append(got, l.ID)
		}
	}
	sort.Strings(got)
	want := append([]string{}, statementCompleters...)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("languages with a StatementCompleter = %v, ledger = %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("languages with a StatementCompleter = %v, ledger = %v", got, want)
		}
	}
}
