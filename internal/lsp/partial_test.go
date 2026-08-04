package lsp

import (
	"slices"
	"testing"
)

// TestPartialTypeMatch guards the #1510 classifier: pyright-style
// assignability messages classify as partial exactly when the expected type's
// union members are a strict subset of the inferred union's.
func TestPartialTypeMatch(t *testing.T) {
	for _, tc := range []struct {
		msg  string
		want bool
	}{
		// The motivating case: expected is one branch of the inferred union.
		{`Argument of type "str | None" cannot be assigned to parameter "method" of type "str"`, true},
		// Reversed phrasing family (pyright ≥1.1.360): "is not assignable".
		{`Type "int | None" is not assignable to type "int"`, true},
		// Declared-type variant.
		{`Expression of type "str | None" cannot be assigned to declared type "str"`, true},
		// Expected union that is a strict subset of the inferred union.
		{`Type "str | int | None" is not assignable to type "str | int"`, true},
		// Nested unions stay inside their brackets: the top-level member
		// matches whole.
		{`Type "list[int | str] | None" is not assignable to type "list[int | str]"`, true},
		// Full mismatch: expected not a member.
		{`Argument of type "int" cannot be assigned to parameter "method" of type "str"`, false},
		// Disjoint unions.
		{`Type "int | None" is not assignable to type "str"`, false},
		// Expected equals inferred member set — not partial, plain mismatch
		// details live elsewhere.
		{`Type "str | None" is not assignable to type "str | None"`, false},
		// Expected union not fully covered.
		{`Type "str | None" is not assignable to type "str | int"`, false},
		// A nested union member is not a top-level member.
		{`Type "list[int | str]" is not assignable to type "int"`, false},
		// Not an assignability message at all, despite quotes and a pipe.
		{`Import "os | sys" could not be resolved`, false},
		// Fewer than two quoted segments.
		{`"str | None" is odd here`, false},
		{`Cannot assign here`, false},
	} {
		if got := partialTypeMatch(tc.msg); got != tc.want {
			t.Errorf("partialTypeMatch(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

// TestSplitUnion guards the top-level union splitter.
func TestSplitUnion(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"str | None", []string{"str", "None"}},
		{"str", []string{"str"}},
		{"list[int | str] | None", []string{"list[int | str]", "None"}},
		{"tuple[int, str] | dict[str, int | None]", []string{"tuple[int, str]", "dict[str, int | None]"}},
		{"  str  |  None  ", []string{"str", "None"}},
		{"", nil},
	} {
		if got := splitUnion(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("splitUnion(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestPartialSeverityRule guards the `partial` pseudo-condition end to end in
// the severity remap (#1510): the same code demotes only when the message
// classifies as partial.
func TestPartialSeverityRule(t *testing.T) {
	rs := CompileSeverityRules([]string{"source=pyright reportArgumentType partial warning"})
	partial := Diagnostic{Source: "pyright", Code: "reportArgumentType", Severity: 1,
		Message: `Argument of type "str | None" cannot be assigned to parameter "method" of type "str"`}
	full := Diagnostic{Source: "pyright", Code: "reportArgumentType", Severity: 1,
		Message: `Argument of type "int" cannot be assigned to parameter "method" of type "str"`}
	got := rs.Apply([]Diagnostic{partial, full})
	if len(got) != 2 || got[0].Severity != 2 || got[1].Severity != 1 {
		t.Errorf("Apply = %+v, want partial demoted to warning, full mismatch untouched", got)
	}
}

// TestPartialRuleNotNative guards that `partial` rules never pass through as
// native server overrides — the classifier only exists client-side.
func TestPartialRuleNotNative(t *testing.T) {
	if got := NativeSeverityOverrides([]string{"reportArgumentType partial warning"}); got != nil {
		t.Errorf("NativeSeverityOverrides = %v, want nil for a partial rule", got)
	}
}

// TestPartialIgnoreRule guards the pseudo-condition in the shared ignore-rule
// grammar: a partial-gated ignore rule drops only classifying diagnostics,
// and a rule whose sole condition is `partial` still counts as conditioned.
func TestPartialIgnoreRule(t *testing.T) {
	rs := CompileIgnoreRules([]string{"partial"})
	if len(rs) != 1 {
		t.Fatalf("CompileIgnoreRules = %v, want the bare partial rule kept", rs)
	}
	if rs.Match(Diagnostic{Message: `Argument of type "int" cannot be assigned to parameter "m" of type "str"`}) {
		t.Error("full mismatch matched a partial-only rule")
	}
	if !rs.Match(Diagnostic{Message: `Argument of type "str | None" cannot be assigned to parameter "m" of type "str"`}) {
		t.Error("union-partial mismatch did not match a partial-only rule")
	}
}
