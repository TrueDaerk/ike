package app

// complexity_problems_test.go covers the complexity Problems feed (#2920):
// the diagnostic builder's thresholds and message text, and the app wiring —
// every generation replaces the source instead of appending, a threshold
// reload adds or drops rows, and switching the feed off clears only its own
// rows.

import (
	"os"
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
	got := complexityDiagnostics(v, complexity.Uniform(complexity.Thresholds{CCN: 15, Cog: 15, Cmt: 0}), nil, "")
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
	if got := complexityDiagnostics(v, complexity.Uniform(complexity.Thresholds{CCN: 41, Cog: 26, Cmt: 0}), nil, ""); len(got) != 0 {
		t.Fatalf("raised thresholds still flag %v", got)
	}
}

// TestComplexityDiagnosticsCommentDensity: a body at or above
// complexity.comment_density_warn (#2934) is its own warning with Code
// "comments", beside any CCN / cognitive row; 0 turns it off.
func TestComplexityDiagnosticsCommentDensity(t *testing.T) {
	v := complexity.View{Files: map[string][]complexity.Function{
		"/p/a.go": {
			{Name: "foo", Line: 3, CCN: 2, NLOC: 9, CommentLines: 11, CommentDensity: 0.55},
			{Name: "below", Line: 9, CCN: 2, NLOC: 7, CommentLines: 3, CommentDensity: 0.3},
			{Name: "both", Line: 20, CCN: 30, NLOC: 6, CommentLines: 4, CommentDensity: 0.4},
		},
	}}
	ds := complexityDiagnostics(v, complexity.Uniform(complexity.Thresholds{CCN: 15, Cog: 15, Cmt: 40}), nil, "")["/p/a.go"]
	want := []struct {
		line      int
		msg, code string
	}{
		{3, "function foo: comment density 55% ≥ 40%", "comments"},
		{20, "function both: comment density 40% ≥ 40%", "comments"},
		{20, "function both: CCN 30 ≥ 15", "ccn"},
	}
	if len(ds) != len(want) {
		t.Fatalf("got %d diagnostics, want %d: %+v", len(ds), len(want), ds)
	}
	for i, w := range want {
		if d := ds[i]; d.Message != w.msg || d.Code != w.code || d.Severity != 2 || d.Range.Start.Line != w.line {
			t.Errorf("diag %d = %+v, want line %d %q code %q", i, d, w.line, w.msg, w.code)
		}
	}
	for _, d := range complexityDiagnostics(v, complexity.Uniform(complexity.Thresholds{CCN: 15, Cog: 15, Cmt: 0}), nil, "")["/p/a.go"] {
		if d.Code == "comments" {
			t.Errorf("threshold 0 must disable the density row: %+v", d)
		}
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
	withProjectWalks(t)
	proj := t.TempDir()
	if r, err := filepath.EvalSymlinks(proj); err == nil {
		proj = r
	}
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	t.Chdir(proj)
	writeComplexityFixture(t, proj)
	// A comment-heavy unit (#2934): 2 code lines, 3 comment lines (60 %),
	// CCN 1 — only the comment-density threshold can flag it.
	chatty := filepath.Join(proj, "src", "c.go")
	if err := os.WriteFile(chatty, []byte("package a\n\nfunc C() {\n\t// one\n\t// two\n\t// three\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _ := config.Load(config.Options{})
	cfg.Complexity.CCNWarn = 2
	cfg.Complexity.CommentDensityWarn = 0
	config.Set(cfg)
	t.Cleanup(func() { c, _ := config.Load(config.Options{}); config.Set(c) })
	m := NewWith(registry.New(), host.FromConfig(cfg))
	x := m.ComplexityIndex()
	if !x.Available() {
		t.Skip("no Go grammar in this build (cgo off)")
	}
	m.Init()
	a := filepath.Join(proj, "src", "a.go")
	waitComplexity(t, x, "the walk", func(v complexity.View) bool { return len(v.Files[a]) == 1 && len(v.Files[chatty]) == 1 })

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
		c.Complexity.CommentDensityWarn = 0
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
	// The comment-density threshold re-evaluates on reload too: on, the
	// chatty unit joins; back at 0, it leaves.
	reload(func(c *config.Config) { c.Complexity.CommentDensityWarn = 40 })
	if rows := complexityRows(m.probStore); len(rows) != 2 || rows[1] != "c.go: function C: comment density 60% ≥ 40%" {
		t.Fatalf("comment_density_warn=40 must add the density row: %v", rows)
	}
	reload(func(c *config.Config) {})
	if rows := complexityRows(m.probStore); len(rows) != 1 {
		t.Fatalf("comment_density_warn=0 must drop the density row: %v", rows)
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
	}}, complexity.Uniform(complexity.Thresholds{CCN: 15, Cog: 15, Cmt: 40}), nil, ""))
	p := problems.New(nil)
	p.SetSize(120, 12)
	p.SetStore(s)
	p.Filter().SetText("source:complexity")
	p.Refresh()
	if view := p.View(); !strings.Contains(view, "function A: CCN 20 ≥ 15") || strings.Contains(view, "undefined: y") {
		t.Fatalf("source:complexity must keep only the feed's rows:\n%s", view)
	}
}
