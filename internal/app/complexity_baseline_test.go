package app

// complexity_baseline_test.go covers the app half of the accepted-debt
// baseline (#2958): the project file applies to the Problems feed and the
// Complexity window, complexity.baseline off ignores it, a changed file on
// disk re-applies, 'i' toggles one function and saves, the bulk accept asks
// first, and a malformed file is reported once.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/registry"
	"ike/internal/watch"
)

// baselineApp builds a model over the complexity fixture with ccn_warn 2
// (A, CCN 2, is over) and the density check off, walked to completion.
func baselineApp(t *testing.T) (Model, string) {
	t.Helper()
	withProjectWalks(t) // the tests below assert on the Init walk of proj
	proj := t.TempDir()
	if r, err := filepath.EvalSymlinks(proj); err == nil {
		proj = r
	}
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	t.Chdir(proj)
	writeComplexityFixture(t, proj)
	cfg, _ := config.Load(config.Options{})
	cfg.Complexity.CCNWarn = 2
	cfg.Complexity.CommentDensityWarn = 0
	config.Set(cfg)
	t.Cleanup(func() { c, _ := config.Load(config.Options{}); config.Set(c) })
	m := NewWith(registry.New(), host.FromConfig(cfg))
	if !m.ComplexityIndex().Available() {
		t.Skip("no Go grammar in this build (cgo off)")
	}
	m.Init()
	out, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = out.(Model)
	a := filepath.Join(proj, "src", "a.go")
	// The session base (#2959) is recorded only once the walk has settled,
	// so wait for the walk's end as well as a.go's measure: the measure
	// lands first, and a test asserting on the base right after it raced
	// the walk's last steps on a loaded runner (#3006).
	ix := m.ComplexityIndex()
	waitComplexity(t, ix, "the walk", func(v complexity.View) bool {
		return len(v.Files[a]) == 1 && !ix.Stats().Scanning
	})
	return dismissOnboarding(m), proj
}

// baselineReload re-applies a mutated config through the reload path.
func baselineReload(t *testing.T, m Model, mut func(c *config.Config)) Model {
	t.Helper()
	c, _ := config.Load(config.Options{})
	c.Complexity.CCNWarn = 2
	c.Complexity.CommentDensityWarn = 0
	mut(c)
	config.Set(c)
	tm, _ := m.Update(config.ConfigReloadedMsg{Config: c})
	return tm.(Model)
}

func baselineGen(t *testing.T, m Model) Model {
	t.Helper()
	tm, _ := m.Update(ComplexityChangedMsg{Gen: m.ComplexityIndex().Snapshot().Gen + 1})
	return tm.(Model)
}

func writeBaselineFile(t *testing.T, proj string, ccn, cog int) string {
	t.Helper()
	b := complexity.NewBaseline(2, 15)
	b.Accept("src/a.go", complexity.Function{Name: "A", CCN: ccn, Cognitive: cog}, 0)
	path := complexity.BaselinePath(proj)
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestComplexityBaselineAppliesToProblemsAndPanel(t *testing.T) {
	withProjectWalks(t)
	proj := t.TempDir()
	if r, err := filepath.EvalSymlinks(proj); err == nil {
		proj = r
	}
	// A baseline written before the model exists applies from the start.
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	t.Chdir(proj)
	writeComplexityFixture(t, proj)
	path := writeBaselineFile(t, proj, 2, 1)
	cfg, _ := config.Load(config.Options{})
	cfg.Complexity.CCNWarn = 2
	cfg.Complexity.CommentDensityWarn = 0
	config.Set(cfg)
	t.Cleanup(func() { c, _ := config.Load(config.Options{}); config.Set(c) })
	m := NewWith(registry.New(), host.FromConfig(cfg))
	if !m.ComplexityIndex().Available() {
		t.Skip("no Go grammar in this build (cgo off)")
	}
	m.Init()
	out, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = out.(Model)
	a := filepath.Join(proj, "src", "a.go")
	waitComplexity(t, m.ComplexityIndex(), "the walk", func(v complexity.View) bool { return len(v.Files[a]) == 1 })
	m = dismissOnboarding(m)

	m = baselineGen(t, m)
	if rows := complexityRows(m.probStore); len(rows) != 0 {
		t.Fatalf("an accepted function must have no Problems row: %v", rows)
	}
	out, _ = m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	p := m.complexityPanel()
	if p == nil || p.Baseline() == nil {
		t.Fatal("the panel must be seeded with the baseline")
	}
	if s := p.Summary(); s.OverThreshold != 0 || s.Accepted != 1 {
		t.Fatalf("panel summary over=%d accepted=%d", s.OverThreshold, s.Accepted)
	}
	if v := p.View(); !strings.Contains(v, "A ✓") {
		t.Fatalf("panel must mark the accepted row:\n%s", v)
	}

	// complexity.baseline off ignores the file everywhere; on re-applies.
	m = baselineReload(t, m, func(c *config.Config) { c.Complexity.Baseline = false })
	if rows := complexityRows(m.probStore); len(rows) != 1 || !strings.HasSuffix(rows[0], "function A: CCN 2 ≥ 2") {
		t.Fatalf("baseline off must list the function: %v", rows)
	}
	if p.Baseline() != nil || strings.Contains(p.View(), "✓") {
		t.Fatal("baseline off must reach the panel")
	}
	m = baselineReload(t, m, func(c *config.Config) {})
	if rows := complexityRows(m.probStore); len(rows) != 0 || p.Baseline() == nil {
		t.Fatalf("baseline on must re-apply: %v", rows)
	}

	// A changed file on disk (a checkout) re-applies: the recorded CCN
	// drops to 1, so A is worsened.
	writeBaselineFile(t, proj, 1, 1)
	m.routeWatchEvent(watch.EventMsg{Kind: watch.BaselineChanged, Path: path})
	rows := complexityRows(m.probStore)
	if len(rows) != 1 || !strings.Contains(rows[0], "function A: CCN 2 ≥ 2 (worse than the baseline)") {
		t.Fatalf("a worsened function must be listed and say so: %v", rows)
	}
	if s := p.Summary(); s.OverThreshold != 1 || s.Worsened != 1 || s.Accepted != 0 {
		t.Fatalf("panel summary after the worsening: over=%d worsened=%d", s.OverThreshold, s.Worsened)
	}
	// A removed file drops the baseline.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	m.routeWatchEvent(watch.EventMsg{Kind: watch.BaselineChanged, Path: path})
	if rows := complexityRows(m.probStore); len(rows) != 1 || strings.Contains(rows[0], "baseline") || p.Baseline() != nil {
		t.Fatalf("a removed file must drop the baseline: %v", rows)
	}
	// A malformed file is reported once and ignored.
	if err := os.WriteFile(path, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.routeWatchEvent(watch.EventMsg{Kind: watch.BaselineChanged, Path: path})
	m.routeWatchEvent(watch.EventMsg{Kind: watch.BaselineChanged, Path: path})
	m = baselineGen(t, m)
	if n := strings.Count(cxNotices(m), "baseline ignored"); n != 1 {
		t.Fatalf("a malformed baseline must be reported exactly once, got %d:\n%s", n, cxNotices(m))
	}
	if rows := complexityRows(m.probStore); len(rows) != 1 || p.Baseline() != nil {
		t.Fatalf("a malformed file is ignored: %v", rows)
	}
}

func TestComplexityBaselineToggleAndAcceptAll(t *testing.T) {
	m, proj := baselineApp(t)
	path := complexity.BaselinePath(proj)
	m = baselineGen(t, m)
	if rows := complexityRows(m.probStore); len(rows) != 1 {
		t.Fatalf("without a baseline A is listed: %v", rows)
	}
	out, _ := m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	p := m.complexityPanel()
	if r, ok := p.Selected(); !ok || r.Fn.Name != "A" {
		t.Fatalf("selected row = %+v", r)
	}

	// 'i' accepts the selected function, saves the file and re-applies.
	out, cmd := m.Update(ComplexityBaselineMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("complexity.baseline must yield the toggle message")
	}
	out, _ = m.Update(cmd())
	m = out.(Model)
	b, err := complexity.LoadBaseline(path)
	if err != nil || b == nil || b.Len() != 1 || b.Accepted[0] != (complexity.Entry{File: "src/a.go", Function: "A", Ordinal: 0, CCN: 2, Cognitive: 1}) {
		t.Fatalf("saved baseline = %+v, %v", b, err)
	}
	if rows := complexityRows(m.probStore); len(rows) != 0 {
		t.Fatalf("the accepted function must leave Problems: %v", rows)
	}
	if !strings.Contains(cxNotices(m), "accepted A") {
		t.Fatalf("the accept must be reported: %q", cxNotices(m))
	}
	if r, _ := p.Selected(); r.Fn.Baseline != complexity.BaselineAccepted {
		t.Fatalf("the panel row must be accepted now: %+v", r.Fn.Baseline)
	}
	// 'i' again drops it.
	out, cmd = m.Update(ComplexityBaselineMsg{})
	m = out.(Model)
	out, _ = m.Update(cmd())
	m = out.(Model)
	if b, _ := complexity.LoadBaseline(path); b == nil || b.Len() != 0 {
		t.Fatalf("the second toggle must drop the entry: %+v", b)
	}
	if rows := complexityRows(m.probStore); len(rows) != 1 {
		t.Fatalf("the dropped function is listed again: %v", rows)
	}

	// The bulk accept asks first: esc cancels, enter writes.
	out, _ = m.Update(ComplexityBaselineAllMsg{})
	m = out.(Model)
	if !m.complexityBaselineDialogOpen() {
		t.Fatal("complexity.baselineAll must open the confirmation dialog")
	}
	if v := plainView(m); !strings.Contains(v, "Record 1 function at or above the thresholds") || !strings.Contains(v, "complexity-baseline.json") {
		t.Fatalf("dialog body:\n%s", v)
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.complexityBaselineDialogOpen() {
		t.Fatal("esc must close the dialog")
	}
	if b, _ := complexity.LoadBaseline(path); b.Len() != 0 {
		t.Fatalf("esc must not write: %+v", b)
	}
	out, _ = m.Update(ComplexityBaselineAllMsg{})
	m = out.(Model)
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if m.complexityBaselineDialogOpen() {
		t.Fatal("enter must close the dialog")
	}
	if b, _ := complexity.LoadBaseline(path); b == nil || b.Len() != 1 || b.CCNWarn != 2 {
		t.Fatalf("enter must write every over-threshold function: %+v", b)
	}
	if rows := complexityRows(m.probStore); len(rows) != 0 {
		t.Fatalf("after the bulk accept nothing is listed: %v", rows)
	}
	if !strings.Contains(cxNotices(m), "accepted 1 function over threshold") {
		t.Fatalf("the bulk accept must be reported: %q", cxNotices(m))
	}
	// A second bulk accept re-seeds (the function is still numerically
	// over threshold), so the dialog opens again; esc leaves the file.
	out, _ = m.Update(ComplexityBaselineAllMsg{})
	m = out.(Model)
	if !m.complexityBaselineDialogOpen() {
		t.Fatal("a re-seed must ask again")
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m = out.(Model)
	// With a raised threshold nothing is over, so there is nothing to ask.
	m = baselineReload(t, m, func(c *config.Config) { c.Complexity.CCNWarn = 50 })
	out, _ = m.Update(ComplexityBaselineAllMsg{})
	m = out.(Model)
	if m.complexityBaselineDialogOpen() || !strings.Contains(cxNotices(m), "nothing to accept") {
		t.Fatalf("nothing over threshold must not open the dialog: %q", cxNotices(m))
	}
	m = baselineReload(t, m, func(c *config.Config) {})

	// With the setting off both actions refuse.
	m = baselineReload(t, m, func(c *config.Config) { c.Complexity.Baseline = false })
	out, _ = m.Update(ComplexityBaselineAllMsg{})
	m = out.(Model)
	if m.complexityBaselineDialogOpen() || !strings.Contains(cxNotices(m), "baseline is off") {
		t.Fatalf("baseline off must refuse the bulk accept: %q", cxNotices(m))
	}
	out, _ = m.Update(complexitypanel.BaselineToggleMsg{Items: []complexitypanel.BaselineItem{{Rel: "src/a.go", Fn: complexity.Function{Name: "A"}}}, Remove: true})
	m = out.(Model)
	if b, _ := complexity.LoadBaseline(path); b.Len() != 1 {
		t.Fatalf("baseline off must not edit the file: %+v", b)
	}
}
