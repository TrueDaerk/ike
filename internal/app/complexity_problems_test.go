package app

// complexity_problems_test.go covers the complexity Problems feed (#2920):
// the diagnostic builder's thresholds and message text, and the app wiring —
// every generation replaces the source instead of appending, a threshold
// reload adds or drops rows, and switching the feed off clears only its own
// rows.

import (
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/complexity"
	"ike/internal/config"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/problems"
	"ike/internal/registry"
)

func TestComplexityDiagnosticsBuilder(t *testing.T) {
	v := complexity.View{Files: map[string][]complexity.Function{
		"/p/a.go": {
			{Name: "low", Line: 2, CCN: 3, Cognitive: 2},
			{Name: "ccnOnly", Line: 10, CCN: 40, Cognitive: 4},
			{Name: "cogOnly", Line: 20, CCN: 5, Cognitive: 25},
			{Name: "both", Line: 30, CCN: 15, Cognitive: 15},
		},
		"/p/b.go": {{Name: "fine", Line: 1, CCN: 14, Cognitive: 14}},
	}}
	got := complexityDiagnostics(v, 15, 15)
	if _, ok := got["/p/b.go"]; ok || len(got) != 1 {
		t.Fatalf("only a.go may hold findings: %v", got)
	}
	want := []struct {
		line int
		msg  string
		code string
	}{
		{10, "function ccnOnly: CCN 40 ≥ 15", "ccn"},
		{20, "function cogOnly: cognitive 25 ≥ 15", "cognitive"},
		{30, "function both: CCN 15 ≥ 15, cognitive 15 ≥ 15", "ccn"},
	}
	ds := got["/p/a.go"]
	if len(ds) != len(want) {
		t.Fatalf("got %d diagnostics, want %d: %+v", len(ds), len(want), ds)
	}
	for i, w := range want {
		d := ds[i]
		if d.Message != w.msg || d.Code != w.code || d.Source != "complexity" || d.Severity != 2 ||
			d.Range.Start.Line != w.line || d.Range.End.Line != w.line {
			t.Errorf("diag %d = %+v, want line %d %q code %q", i, d, w.line, w.msg, w.code)
		}
	}
	// A threshold is inclusive per metric: at 41 / 26 nothing is over.
	if got := complexityDiagnostics(v, 41, 26); len(got) != 0 {
		t.Fatalf("raised thresholds still flag %v", got)
	}
}

// complexityRows lists the store's complexity-sourced messages per path.
func complexityRows(s *problems.Store) []string {
	var out []string
	for _, p := range s.Paths() {
		for _, d := range s.Get(p) {
			if d.Source == "complexity" {
				out = append(out, filepath.Base(p)+": "+d.Message)
			}
		}
	}
	return out
}

func TestComplexityProblemsFeedWiring(t *testing.T) {
	proj := t.TempDir()
	if r, err := filepath.EvalSymlinks(proj); err == nil {
		proj = r
	}
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	t.Chdir(proj)
	writeComplexityFixture(t, proj)

	cfg, _ := config.Load(config.Options{})
	cfg.Complexity.CCNWarn = 2
	config.Set(cfg)
	t.Cleanup(func() { c, _ := config.Load(config.Options{}); config.Set(c) })
	m := NewWith(registry.New(), host.FromConfig(cfg))
	x := m.ComplexityIndex()
	if !x.Available() {
		t.Skip("no Go grammar in this build (cgo off)")
	}
	m.Init()
	a := filepath.Join(proj, "src", "a.go")
	waitComplexity(t, x, "the walk", func(v complexity.View) bool { return len(v.Files[a]) == 1 })

	// Unrelated findings the feed must never touch.
	lsp := ilsp.Diagnostic{Severity: 1, Message: "undefined: y", Source: "gopls"}
	m.probStore.Set(a, []ilsp.Diagnostic{lsp})
	m.probStore.SetTaskSource("deps", map[string][]ilsp.Diagnostic{a: {{Severity: 2, Message: "vuln", Source: "deps"}}})

	gen := x.Snapshot().Gen
	for i := 1; i <= 2; i++ {
		tm, _ := m.Update(ComplexityChangedMsg{Gen: gen + i})
		m = tm.(Model)
	}
	if rows := complexityRows(m.probStore); len(rows) != 1 || rows[0] != "a.go: function A: CCN 2 ≥ 2" {
		t.Fatalf("two generations must replace, not append: %v", rows)
	}
	if d := m.probStore.Get(a)[1]; d.Source != "complexity" || d.Range.Start.Line != 2 {
		t.Fatalf("the row must sit at the header line: %+v", d)
	}

	// Raising the threshold on reload drops the row, lowering it adds it.
	reload := func(mut func(c *config.Config)) {
		c, _ := config.Load(config.Options{})
		c.Complexity.CCNWarn = 2
		mut(c)
		config.Set(c)
		tm, _ := m.Update(config.ConfigReloadedMsg{Config: c})
		m = tm.(Model)
	}
	reload(func(c *config.Config) { c.Complexity.CCNWarn = 3 })
	if rows := complexityRows(m.probStore); len(rows) != 0 {
		t.Fatalf("a raised threshold must drop the row: %v", rows)
	}
	reload(func(c *config.Config) {})
	if rows := complexityRows(m.probStore); len(rows) != 1 {
		t.Fatalf("a lowered threshold must add the row: %v", rows)
	}

	// The feed switch off clears its rows and nothing else.
	reload(func(c *config.Config) { c.Complexity.Problems = false })
	if rows := complexityRows(m.probStore); len(rows) != 0 {
		t.Fatalf("complexity.problems=false must clear the rows: %v", rows)
	}
	var kept []string
	for _, d := range m.probStore.Get(a) {
		kept = append(kept, d.Source)
	}
	if strings.Join(kept, ",") != "gopls,deps" {
		t.Fatalf("LSP and task findings must stay: %v", kept)
	}
	// A generation while off publishes nothing.
	tm, _ := m.Update(ComplexityChangedMsg{Gen: gen + 9})
	m = tm.(Model)
	if rows := complexityRows(m.probStore); len(rows) != 0 {
		t.Fatalf("the feed republished while off: %v", rows)
	}

	// Back on, then the index master switch off clears them too.
	reload(func(c *config.Config) {})
	if rows := complexityRows(m.probStore); len(rows) != 1 {
		t.Fatalf("re-enabling the feed must republish: %v", rows)
	}
	reload(func(c *config.Config) { c.Complexity.Enabled = false })
	if rows := complexityRows(m.probStore); len(rows) != 0 {
		t.Fatalf("complexity.enabled=false must clear the rows: %v", rows)
	}
}

// TestComplexityProblemsSourceFilter: the pane's source:complexity filter
// keeps exactly the feed's rows.
func TestComplexityProblemsSourceFilter(t *testing.T) {
	s := problems.NewStore()
	s.Set("/p/a.go", []ilsp.Diagnostic{{Severity: 1, Message: "undefined: y", Source: "gopls"}})
	s.SetTaskSource("complexity", complexityDiagnostics(complexity.View{Files: map[string][]complexity.Function{
		"/p/a.go": {{Name: "A", Line: 4, CCN: 20}},
	}}, 15, 15))
	p := problems.New(nil)
	p.SetSize(120, 12)
	p.SetStore(s)
	p.Filter().SetText("source:complexity")
	p.Refresh()
	if view := p.View(); !strings.Contains(view, "function A: CCN 20 ≥ 15") || strings.Contains(view, "undefined: y") {
		t.Fatalf("source:complexity must keep only the feed's rows:\n%s", view)
	}
}
