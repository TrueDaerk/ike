package main

import (
	"sort"
	"strings"
	"testing"

	"ike/internal/lang"
)

// declkeyword_audit_test.go is the standing ledger of the completion-context
// audit (#2654), the sibling of spanfamily_audit_test.go (#2337). The
// completion auto-popup stays shut right after a keyword that declares a
// new name — `func na`, `def na` — which only works for a language whose
// plugin lists its keywords in lang.Language.DeclKeywords. Nothing used to
// say which language *should*: a language registered without them silently
// keeps popping up over every fresh name.
//
// This test closes that hole. Every registered language either carries
// DeclKeywords or an entry here saying why it has none. A newly registered
// language fails the build until someone decides — keywords or
// justification. The ledger stays honest in both directions: an entry for a
// language that has since gained keywords, or that no longer exists, is
// stale and fails too.

// The reasons a language may record for carrying no declaration keywords.
const (
	// The language introduces names by position (a key before a colon, a
	// column header, a section bracket), never by keyword.
	reasonNoDeclKeyword = "names are introduced by position, never by a keyword"
	// The buffer's content is foreign data (CSV cells, patch hunks, log
	// output, checksum lines) — nothing in it is declared.
	reasonNoDeclarations = "the buffer holds foreign data; nothing in it is declared"
	// Prose: a name is just a word.
	reasonProse = "prose; no name is ever declared"
	// An injection helper grammar: no buffer is ever of this language.
	reasonDeclInjection = "injection helper grammar; no buffer is ever of this language"
	// The keyword that follows a declaration head also follows a reference
	// (`CREATE TABLE t` vs `ALTER TABLE t`, `DROP TABLE t`): gating on it
	// would hide the very names the user wants completed.
	reasonAmbiguousKeyword = "its declaring keywords also precede references to existing names"
	// A directive whose operand is an existing path or module, not a new
	// name (`use ./dir`, `replace a => b`).
	reasonReferencesOnly = "its directives reference existing names or paths only"
)

// noDeclKeywords records why a language lists no declaration keywords.
var noDeclKeywords = map[string]string{
	"ansible":         reasonNoDeclKeyword,
	"crontab":         reasonNoDeclarations,
	"csv":             reasonNoDeclarations,
	"diff":            reasonNoDeclarations,
	"dotenv":          reasonNoDeclKeyword,
	"go.sum":          reasonNoDeclarations,
	"go.work":         reasonReferencesOnly,
	"html":            reasonNoDeclKeyword,
	"http":            reasonNoDeclKeyword,
	"ini":             reasonNoDeclKeyword,
	"json":            reasonNoDeclKeyword,
	"log":             reasonNoDeclarations,
	"markdown":        reasonProse,
	"markdown_inline": reasonDeclInjection,
	"ndjson":          reasonNoDeclKeyword,
	"psv":             reasonNoDeclarations,
	"sql":             reasonAmbiguousKeyword,
	"toml":            reasonNoDeclKeyword,
	"tsv":             reasonNoDeclarations,
	"xml":             reasonNoDeclKeyword,
	"yaml":            reasonNoDeclKeyword,
}

// TestEveryLanguageDeclaresOrJustifiesDeclKeywords is the audit's guardrail.
func TestEveryLanguageDeclaresOrJustifiesDeclKeywords(t *testing.T) {
	seen := map[string]bool{}
	for _, l := range lang.All() {
		seen[l.ID] = true
		reason := noDeclKeywords[l.ID]
		switch {
		case len(l.DeclKeywords) > 0 && reason != "":
			t.Errorf("language %q lists DeclKeywords %v but the ledger still excuses it (%q): drop the ledger entry", l.ID, l.DeclKeywords, reason)
		case len(l.DeclKeywords) == 0 && reason == "":
			t.Errorf("language %q has no DeclKeywords and no ledger entry: list the keywords that introduce a new name, or record why there are none in cmd/ike/declkeyword_audit_test.go", l.ID)
		}
		for i, k := range l.DeclKeywords {
			if k == "" || strings.ContainsAny(k, " \t") {
				t.Errorf("language %q DeclKeywords[%d] = %q: a keyword is one identifier", l.ID, i, k)
			}
			for _, o := range l.DeclKeywords[:i] {
				if strings.EqualFold(o, k) {
					t.Errorf("language %q lists DeclKeyword %q twice", l.ID, k)
				}
			}
		}
	}
	var stale []string
	for id := range noDeclKeywords {
		if !seen[id] {
			stale = append(stale, id)
		}
	}
	sort.Strings(stale)
	for _, id := range stale {
		t.Errorf("ledger entry for %q names a language that is not registered", id)
	}
}
