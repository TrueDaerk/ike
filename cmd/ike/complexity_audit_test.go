//go:build !ike_complexity_only

package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"ike/internal/highlight"
	"ike/internal/highlight/treetest"
	"ike/internal/lang"
)

// complexity_audit_test.go is the standing ledger of the language complexity
// audit (Roadmap 0550, #2913, #2914), the sibling of the span-family ledger
// in spanfamily_audit_test.go (#2337) and the unbound-command audit in
// keybind_audit_test.go (#2305). The complexity engine measures a language
// only through its lang.ComplexitySpec — the node kinds that start a
// function, branch, nest, and the boolean operator tokens — and nothing else
// says which language *should* be measured: a language added without a spec
// would silently read as "no functions" in every metric.
//
// This test closes that hole. Every registered language either declares a
// spec that measures a per-language probe buffer to its expected values, or
// carries a row here saying why it does not, with a reason from a small named
// set. A newly registered language therefore fails the build until someone
// decides — spec or justification. The ledger is kept honest in both
// directions: a row for a language that has since gained a spec, that claims
// no grammar while one is registered, or that no longer exists, is stale and
// fails too.
//
// "Measures" is decided behaviourally: each spec'd language carries a probe
// buffer, and the spec must find the named function in it at the expected
// cyclomatic count (1 + branches + boolean operators, nested units excluded
// — the reference count treetest implements). The per-language unit tests
// (plugins/languages/<lang>/complexity_test.go) go further and check every
// kind the spec names against a parse; the audit only asks that the spec
// works end to end on the grammar the language actually registers.

// The reasons a language may record for not being measured.
const (
	// The format has no functions: data, markup, configuration, logs,
	// patches, dependency manifests. There is nothing a function-level
	// metric could attach to.
	reasonNoFunctions = "data or markup format: no functions to measure"
	// The language has no Tree-sitter grammar, so there is no tree to walk;
	// measuring it would need LSP symbols or regex heuristics, which the
	// epic rules out. Verified: a row with this reason fails once the
	// language registers a grammar.
	reasonNoGrammar = "no Tree-sitter grammar: nothing to walk"
)

// reasonSpecGap (the span ledger owns the reasonGap name) records a real
// gap surfaced by filling in this ledger — tracked in an issue, not excused.
// When the gap closes, the row goes and a probe takes its place.
func reasonSpecGap(issue int) string {
	return fmt.Sprintf("genuine gap, spec tracked in #%d", issue)
}

// complexityProbe is the per-language probe: a buffer with one function the
// spec must find by name at the expected cyclomatic count.
type complexityProbe struct {
	lines   []string
	wantFn  string
	wantCCN int
}

// complexityProbes carries one probe per language that declares a spec. A
// listed language whose spec is gone, or whose probe stops measuring as
// expected, fails — so a removed wiring (or a rotted probe) cannot hide.
var complexityProbes = map[string]complexityProbe{
	// if + && = 3.
	"go": {[]string{
		"package p",
		"func f(a, b int) bool {",
		"	if a > 0 && b > 0 {",
		"		return true",
		"	}",
		"	return false",
		"}",
	}, "f", 3},
	// if + or = 3.
	"python": {[]string{
		"def f(a, b):",
		"    if a or b:",
		"        return True",
		"    return False",
	}, "f", 3},
	// if + ?? = 3.
	"php": {[]string{
		"<?php",
		"function f($a, $b) {",
		"    if ($a ?? $b) {",
		"        return true;",
		"    }",
		"    return false;",
		"}",
	}, "f", 3},
	// if + || = 3.
	"typescript": {[]string{
		"function f(a: number, b: number): boolean {",
		"  if (a || b) {",
		"    return true;",
		"  }",
		"  return false;",
		"}",
	}, "f", 3},
	// if + && = 3.
	"shell": {[]string{
		"f() {",
		"  if [ -n \"$1\" ] && [ -n \"$2\" ]; then",
		"    return 0",
		"  fi",
		"  return 1",
		"}",
	}, "f", 3},
}

// unmeasuredLanguages records why a language declares no complexity spec.
// Every row is verified: a registered id, no spec, and — for reasonNoGrammar
// — no grammar either.
var unmeasuredLanguages = map[string]string{
	"ansible":         reasonNoFunctions,
	"crontab":         reasonNoFunctions,
	"css":             reasonNoFunctions,
	"csv":             reasonNoFunctions,
	"diff":            reasonNoFunctions,
	"dockerfile":      reasonNoFunctions,
	"dotenv":          reasonNoFunctions,
	"go.mod":          reasonNoFunctions,
	"go.sum":          reasonNoFunctions,
	"go.work":         reasonNoFunctions,
	"html":            reasonNoFunctions, // scripts inside it are typescript's
	"http":            reasonNoFunctions,
	"ini":             reasonNoFunctions,
	"json":            reasonNoFunctions,
	"log":             reasonNoFunctions,
	"make":            reasonNoFunctions, // rules are not functions
	"markdown":        reasonNoFunctions,
	"markdown_inline": reasonNoFunctions, // injection helper grammar
	"ndjson":          reasonNoFunctions,
	"psv":             reasonNoFunctions,
	"sql":             reasonNoFunctions, // statements; stored routines are a dialect niche
	"systemd":         reasonNoFunctions,
	"toml":            reasonNoFunctions,
	"tsv":             reasonNoFunctions,
	"xml":             reasonNoFunctions,
	"yaml":            reasonNoFunctions,
}

// TestEveryLanguageHasComplexitySpecOrReason is the audit's guardrail: every
// registered language either declares a spec that measures its probe, or
// records why it does not.
func TestEveryLanguageHasComplexitySpecOrReason(t *testing.T) {
	// Without cgo no language has a grammar, so there is no tree to probe
	// and nothing a reasonNoGrammar row could be stale against; the ledger
	// shape is still checked.
	grammars := false
	for _, l := range lang.All() {
		if l.Grammar != nil {
			grammars = true
			break
		}
	}
	for _, l := range lang.All() {
		reason, justified := unmeasuredLanguages[l.ID]
		probe, probed := complexityProbes[l.ID]
		switch {
		case l.Complexity != nil && justified:
			t.Errorf("%s declares a complexity spec and carries a ledger reason: delete "+
				"its unmeasuredLanguages row (#2914)", l.ID)
		case l.Complexity != nil && !probed:
			t.Errorf("%s declares a complexity spec but has no probe: add a "+
				"complexityProbes entry so the spec is verified against the grammar "+
				"(#2914)", l.ID)
		case l.Complexity != nil && !grammars:
			// !cgo: nothing to parse.
		case l.Complexity != nil:
			verifyComplexityProbe(t, l, probe)
		case justified && reason == reasonNoGrammar && l.Grammar != nil:
			t.Errorf("stale ledger row for %s: it claims no grammar, but one is "+
				"registered — declare a lang.ComplexitySpec in plugins/languages/%s "+
				"or record another reason (#2914)", l.ID, l.ID)
		case justified:
			// recorded and verified.
		case probed:
			t.Errorf("%s has a probe but declares no complexity spec: the wiring is "+
				"gone — restore Complexity in plugins/languages/%s or move the "+
				"language to unmeasuredLanguages (#2914)", l.ID, l.ID)
		default:
			t.Errorf("%s has no complexity ledger row: declare a lang.ComplexitySpec "+
				"in plugins/languages/%s and add a complexityProbes entry, or record a "+
				"reason in unmeasuredLanguages (#2914)", l.ID, l.ID)
		}
	}
}

// verifyComplexityProbe parses l's probe with its registered grammar and
// checks the spec finds the named function at the expected cyclomatic count.
func verifyComplexityProbe(t *testing.T, l lang.Language, probe complexityProbe) {
	t.Helper()
	root := highlight.SyntaxTree(l.ID, probe.lines)
	if root == nil {
		t.Errorf("%s declares a complexity spec but its probe yields no tree: the language "+
			"registers no grammar, so there is nothing to walk (#2914)", l.ID)
		return
	}
	fns := treetest.Functions(root, strings.Join(probe.lines, "\n"), l.Complexity)
	for _, f := range fns {
		if f.Name != probe.wantFn {
			continue
		}
		if f.CCN != probe.wantCCN {
			t.Errorf("%s: probe function %s measures CCN %d, want %d — the spec's "+
				"Branch/BoolOps no longer match the grammar, or the probe rotted (#2914)",
				l.ID, probe.wantFn, f.CCN, probe.wantCCN)
		}
		return
	}
	t.Errorf("%s: the spec finds no function %q in the probe (found %+v): "+
		"FunctionNodes or its name field no longer match the grammar (#2914)",
		l.ID, probe.wantFn, fns)
}

// TestComplexityLedgerIsCurrent keeps the ledger itself honest: every row
// and every probe must name a registered language, so a renamed or removed
// language cannot leave a dead justification behind.
func TestComplexityLedgerIsCurrent(t *testing.T) {
	registered := map[string]bool{}
	for _, l := range lang.All() {
		registered[l.ID] = true
	}
	ids := make([]string, 0, len(unmeasuredLanguages)+len(complexityProbes))
	for id := range unmeasuredLanguages {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if !registered[id] {
			t.Errorf("stale unmeasuredLanguages row %q: no registered language has this id", id)
		}
		switch r := unmeasuredLanguages[id]; {
		case r == reasonNoFunctions, r == reasonNoGrammar, strings.HasPrefix(r, "genuine gap, spec tracked in #"):
		default:
			t.Errorf("unmeasuredLanguages[%q] carries an unknown reason %q: use "+
				"reasonNoFunctions, reasonNoGrammar or reasonSpecGap(issue)", id, r)
		}
	}
	ids = ids[:0]
	for id := range complexityProbes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if !registered[id] {
			t.Errorf("stale complexityProbes entry %q: no registered language has this id", id)
		}
	}
}

// TestSyntaxTreeOperatorPerLanguage pins the operator seam the specs rely on
// (#2914): highlight.SyntaxNode.Operator carries the boolean operator token
// on each language's operator node — the grammar's `operator` field where
// there is one, the infix token of shell's field-less list otherwise.
func TestSyntaxTreeOperatorPerLanguage(t *testing.T) {
	cases := []struct {
		lang, kind, op string
		lines          []string
	}{
		{"go", "binary_expression", "&&", []string{"package p", "var x = a && b"}},
		{"python", "boolean_operator", "and", []string{"x = a and b"}},
		{"php", "binary_expression", "||", []string{"<?php", "$x = $a || $b;"}},
		{"typescript", "binary_expression", "??", []string{"const x = a ?? b;"}},
		{"shell", "list", "&&", []string{"true && false"}},
	}
	for _, c := range cases {
		root := highlight.SyntaxTree(c.lang, c.lines)
		if root == nil {
			t.Skipf("%s: no grammar (CGO off)", c.lang)
		}
		found := false
		var visit func(n *highlight.SyntaxNode)
		visit = func(n *highlight.SyntaxNode) {
			if n.Kind == c.kind && n.Operator == c.op {
				found = true
			}
			for _, ch := range n.Children {
				visit(ch)
			}
		}
		visit(root)
		if !found {
			t.Errorf("%s: no %s node carries Operator %q in %q", c.lang, c.kind, c.op, c.lines)
		}
	}
}
