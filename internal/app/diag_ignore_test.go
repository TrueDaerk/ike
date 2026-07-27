package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	ilsp "ike/internal/lsp"
)

// ignoreDiag builds a diagnostic with an attribution, the ignore rules' match
// surface.
func ignoreDiag(source, code, msg string) ilsp.Diagnostic {
	d := testDiag(0, 1, msg)
	d.Source, d.Code = source, code
	return d
}

// reloadWithIgnore pushes a config reload whose lsp.diagnostics_ignore is set
// to rules, and restores the process-wide config afterwards.
func reloadWithIgnore(t *testing.T, m Model, rules []string) Model {
	t.Helper()
	orig := config.Get()
	t.Cleanup(func() { config.Set(orig) })
	c, _ := config.Load(config.Options{})
	c.LSP.DiagnosticsIgnore = rules
	out, _ := m.Update(config.ConfigReloadedMsg{Config: c})
	return out.(Model)
}

// runForReload executes cmd (unwrapping one level of tea.Batch — the Update
// pipeline appends the toast command) and returns the ConfigReloadedMsg.
func runForReload(t *testing.T, cmd tea.Cmd) config.ConfigReloadedMsg {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case config.ConfigReloadedMsg:
			return msg
		case tea.BatchMsg:
			queue = append(queue, msg...)
		}
	}
	t.Fatal("no ConfigReloadedMsg produced")
	return config.ConfigReloadedMsg{}
}

// TestIgnoreRulesFilterPublishes: a matching publish is dropped before the
// Problems store sees it (#1259).
func TestIgnoreRulesFilterPublishes(t *testing.T) {
	m := problemsApp(t)
	m = reloadWithIgnore(t, m, []string{"source=intelephense code=P1006"})

	out, _ := m.Update(ilsp.DiagnosticsMsg{Path: "/a.php", Diagnostics: []ilsp.Diagnostic{
		ignoreDiag("intelephense", "P1006", "Expected type 'array'. Found 'null'."),
		ignoreDiag("intelephense", "P1005", "too few arguments"),
	}})
	m = out.(Model)
	got := m.probStore.Get("/a.php")
	if len(got) != 1 || got[0].Code != "P1005" {
		t.Fatalf("store = %+v, want only the P1005 entry", got)
	}
}

// TestIgnoreRuleEditRefiltersLive: adding a rule re-filters what is already on
// screen from the raw cache — no republish needed (#1259).
func TestIgnoreRuleEditRefiltersLive(t *testing.T) {
	m := problemsApp(t)
	// A real type mismatch: no default rule (#1260) matches it.
	out, _ := m.Update(ilsp.DiagnosticsMsg{Path: "/a.php", Diagnostics: []ilsp.Diagnostic{
		ignoreDiag("intelephense", "P1006", "Expected type 'int'. Found 'string'."),
		ignoreDiag("intelephense", "P1005", "too few arguments"),
	}})
	m = out.(Model)
	if len(m.probStore.Get("/a.php")) != 2 {
		t.Fatal("both diagnostics must land before the rule exists")
	}

	m = reloadWithIgnore(t, m, []string{"code=P1006"})
	if got := m.probStore.Get("/a.php"); len(got) != 1 || got[0].Code != "P1005" {
		t.Fatalf("after the rule edit store = %+v, want only P1005", got)
	}

	// Removing the rule restores the suppressed diagnostic from the raw cache.
	m = reloadWithIgnore(t, m, nil)
	if got := m.probStore.Get("/a.php"); len(got) != 2 {
		t.Fatalf("after removing the rule store = %+v, want both back", got)
	}
}

// TestIgnoreDiagnosticMsgPersistsProjectRule: the editor's ignore action writes
// the rule to the project settings file and the reload suppresses it (#1259).
func TestIgnoreDiagnosticMsgPersistsProjectRule(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	m := problemsApp(t)
	orig := config.Get()
	t.Cleanup(func() { config.Set(orig) })

	out, _ := m.Update(ilsp.DiagnosticsMsg{Path: "/a.php", Diagnostics: []ilsp.Diagnostic{
		ignoreDiag("intelephense", "P1006", "Expected type 'array'. Found 'null'."),
	}})
	m = out.(Model)

	out, cmd := m.Update(ilsp.IgnoreDiagnosticMsg{Diagnostic: ignoreDiag("intelephense", "P1006", "x")})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("ignore must dispatch the config write")
	}
	reloaded := runForReload(t, cmd) // runs the write + reload
	raw, err := os.ReadFile(filepath.Join(dir, ".ike", "settings.toml"))
	if err != nil {
		t.Fatalf("project settings not written: %v", err)
	}
	if !strings.Contains(string(raw), "source=intelephense code=P1006") {
		t.Fatalf("settings.toml = %q, want the ignore rule", raw)
	}

	// The write's reload message re-filters the cached set.
	out, _ = m.Update(reloaded)
	m = out.(Model)
	if got := m.probStore.Get("/a.php"); len(got) != 0 {
		t.Fatalf("after ignore store = %+v, want empty", got)
	}
}
