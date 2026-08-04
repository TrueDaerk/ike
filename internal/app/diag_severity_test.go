package app

import (
	"testing"

	"ike/internal/config"
	ilsp "ike/internal/lsp"
)

// reloadWithSeverity pushes a config reload whose lsp.diagnostics_severity is
// set to rules, and restores the process-wide config afterwards.
func reloadWithSeverity(t *testing.T, m Model, rules []string) Model {
	t.Helper()
	orig := config.Get()
	t.Cleanup(func() { config.Set(orig) })
	c, _ := config.Load(config.Options{})
	c.LSP.DiagnosticsSeverity = rules
	out, _ := m.Update(config.ConfigReloadedMsg{Config: c})
	return out.(Model)
}

// TestSeverityRulesRemapPublishes: a matching publish reaches the Problems
// store with the remapped severity; off drops, syntax errors stay put (#1503).
func TestSeverityRulesRemapPublishes(t *testing.T) {
	m := problemsApp(t)
	m = reloadWithSeverity(t, m, []string{
		"reportArgumentType warning",
		"reportUnusedImport off",
	})

	arg := ignoreDiag("Pyright", "reportArgumentType", "type mismatch")
	arg.Severity = 1
	unused := ignoreDiag("Pyright", "reportUnusedImport", "unused import")
	unused.Severity = 2
	syntax := ignoreDiag("Pyright", "", "expected expression")
	syntax.Severity = 1
	out, _ := m.Update(ilsp.DiagnosticsMsg{Path: "/a.py", Diagnostics: []ilsp.Diagnostic{arg, unused, syntax}})
	m = out.(Model)

	got := m.probStore.Get("/a.py")
	if len(got) != 2 {
		t.Fatalf("store = %+v, want the remapped and the syntax entry", got)
	}
	if got[0].Code != "reportArgumentType" || got[0].Severity != 2 {
		t.Fatalf("reportArgumentType = %+v, want severity 2", got[0])
	}
	if got[1].Code != "" || got[1].Severity != 1 {
		t.Fatalf("syntax error = %+v, must stay severity 1", got[1])
	}
}

// TestSeverityRuleEditRemapsLive: a rule edit re-applies to the raw cache —
// no republish needed — and removing it restores the published severity.
func TestSeverityRuleEditRemapsLive(t *testing.T) {
	m := problemsApp(t)
	d := ignoreDiag("Pyright", "reportArgumentType", "type mismatch")
	d.Severity = 1
	out, _ := m.Update(ilsp.DiagnosticsMsg{Path: "/a.py", Diagnostics: []ilsp.Diagnostic{d}})
	m = out.(Model)
	if got := m.probStore.Get("/a.py"); len(got) != 1 || got[0].Severity != 1 {
		t.Fatalf("store = %+v, want the published error", got)
	}

	m = reloadWithSeverity(t, m, []string{"reportArgumentType info"})
	if got := m.probStore.Get("/a.py"); len(got) != 1 || got[0].Severity != 3 {
		t.Fatalf("after the rule edit store = %+v, want severity 3", got)
	}

	m = reloadWithSeverity(t, m, nil)
	if got := m.probStore.Get("/a.py"); len(got) != 1 || got[0].Severity != 1 {
		t.Fatalf("after removing the rule store = %+v, want severity 1 back", got)
	}
}
