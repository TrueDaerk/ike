package lsp

import (
	"reflect"
	"testing"
)

// TestSeverityRulesApply guards the remap semantics (#1503): first match
// wins, `off` drops, codeless diagnostics (syntax errors) never remap, and
// the untouched fast path returns the input slice unchanged.
func TestSeverityRulesApply(t *testing.T) {
	rules := CompileSeverityRules([]string{
		"reportArgumentType warning",
		"reportUnusedImport off", // before the wildcard — first match wins
		"source=Pyright code=report* info",
		"msg=*deprecated* hint",
	})
	diags := []Diagnostic{
		{Severity: 1, Source: "Pyright", Code: "reportArgumentType", Message: "arg"},
		{Severity: 1, Source: "Pyright", Code: "reportGeneralTypeIssues", Message: "x"},
		{Severity: 2, Source: "Pyright", Code: "reportUnusedImport", Message: "unused"},
		{Severity: 1, Source: "Pyright", Code: "", Message: "syntax error"},
		{Severity: 2, Source: "gopls", Code: "SA1019", Message: "x is deprecated"},
		{Severity: 2, Source: "gopls", Code: "unusedvar", Message: "declared"},
	}
	got := rules.Apply(diags)
	want := []int{2, 3, 1, 4, 2} // remapped severities in order, unused import dropped
	if len(got) != len(want) {
		t.Fatalf("Apply kept %d diagnostics, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Severity != w {
			t.Errorf("diag %d (%s): severity = %d, want %d", i, got[i].Code, got[i].Severity, w)
		}
	}
	if got[2].Code != "" || got[2].Severity != 1 {
		t.Errorf("codeless syntax error must stay error, got %+v", got[2])
	}

	// No rules / no hits: the input slice comes back unchanged.
	if out := (SeverityRules)(nil).Apply(diags); !reflect.DeepEqual(out, diags) {
		t.Error("nil rules must return the input unchanged")
	}
	miss := CompileSeverityRules([]string{"nomatch warning"})
	if out := miss.Apply(diags[:1]); &out[0] != &diags[0] {
		t.Error("no hit must return the input slice, not a copy")
	}
}

// TestSeverityRuleParsing: malformed entries — no severity keyword, no
// condition, unknown keyword — are dropped instead of remapping everything.
func TestSeverityRuleParsing(t *testing.T) {
	for _, raw := range []string{"", "  ", "warning", "reportFoo loud", "off"} {
		if rs := CompileSeverityRules([]string{raw}); len(rs) != 0 {
			t.Errorf("rule %q must be dropped, compiled to %+v", raw, rs)
		}
	}
	rs := CompileSeverityRules([]string{"source=pyright code=reportFoo msg=some message off"})
	if len(rs) != 1 || rs[0].to != 0 || rs[0].cond.msg != "some message" {
		t.Fatalf("full-grammar rule parsed to %+v", rs)
	}
}

// TestNativeSeverityOverrides guards the pass-through extraction: only rules
// whose sole condition is an exact code qualify, hint stays client-side, and
// first match wins over a duplicate.
func TestNativeSeverityOverrides(t *testing.T) {
	got := NativeSeverityOverrides([]string{
		"reportArgumentType warning",
		"reportUnusedImport off",
		"reportGeneralTypeIssues info",
		"reportSomething hint",             // no native vocabulary — skipped
		"code=report* error",               // wildcard — skipped
		"source=Pyright reportOther error", // extra condition — skipped
		"msg=whatever error",               // message rule — skipped
		"reportArgumentType error",         // duplicate — first wins
	})
	want := map[string]any{
		"reportArgumentType":      "warning",
		"reportUnusedImport":      "none",
		"reportGeneralTypeIssues": "information",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NativeSeverityOverrides = %#v, want %#v", got, want)
	}
	if NativeSeverityOverrides(nil) != nil {
		t.Error("no rules must yield nil")
	}
}
