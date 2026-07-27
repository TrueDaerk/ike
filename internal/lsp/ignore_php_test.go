package lsp

import (
	"testing"

	"ike/internal/config"
)

// TestDefaultRulesSuppressIntelephenseByRefFalsePositive is the regression
// guard for #1260: intelephense cannot infer types written through
// by-reference parameters (bmewburn/vscode-intelephense#3504) and flags every
// `fill($info)` call with P1006 "Expected type '...'. Found 'null'." (or
// 'unset'). The default lsp.diagnostics_ignore rules must drop exactly those
// — and nothing else.
func TestDefaultRulesSuppressIntelephenseByRefFalsePositive(t *testing.T) {
	rs := CompileIgnoreRules(config.Get().LSP.DiagnosticsIgnore)

	suppressed := []Diagnostic{
		{Source: "intelephense", Code: "P1006", Message: "Expected type 'array'. Found 'null'."},
		{Source: "intelephense", Code: "P1006", Message: "Expected type 'string'. Found 'null'."},
		{Source: "intelephense", Code: "P1006", Message: "Expected type 'object'. Found 'unset'."},
	}
	for _, d := range suppressed {
		if !rs.Match(d) {
			t.Errorf("default rules must suppress %q", d.Message)
		}
	}

	kept := []Diagnostic{
		// Real P1006 type mismatches keep surfacing.
		{Source: "intelephense", Code: "P1006", Message: "Expected type 'int'. Found 'string'."},
		// Other intelephense codes are untouched.
		{Source: "intelephense", Code: "P1005", Message: "Expected 2 arguments. Found 1."},
		// Same shape from another server is untouched.
		{Source: "gopls", Code: "P1006", Message: "Expected type 'array'. Found 'null'."},
	}
	for _, d := range kept {
		if rs.Match(d) {
			t.Errorf("default rules must not suppress %s %s %q", d.Source, d.Code, d.Message)
		}
	}
}
