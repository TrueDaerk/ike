package app

// complexity_cache_test.go pins #3085: the Complexity window's everyday
// actions — accepting / dropping a baseline entry, grouping, the Trend tab's
// recompute, opening and closing the window, a config reload that leaves
// [complexity] as it was — never cost a cold walk. A rescan after any of
// them (the pane's 'r') comes from the cache, as does the next session's
// walk; only complexity.refresh, the explicit rebuild, measures everything.

import (
	"sync"
	"testing"
	"time"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/registry"
)

// scanLog collects the complexity.scan reports of an index.
type scanLog struct {
	mu    sync.Mutex
	scans []complexity.Stats
}

func watchScans(x *complexity.Index) *scanLog {
	l := &scanLog{}
	x.SetOnScan(func(s complexity.Stats) {
		l.mu.Lock()
		l.scans = append(l.scans, s)
		l.mu.Unlock()
	})
	return l
}

func (l *scanLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.scans)
}

// next waits for the report after the n-th one.
func (l *scanLog) next(t *testing.T, n int, what string) complexity.Stats {
	t.Helper()
	for start := time.Now(); time.Since(start) < 3*time.Second; time.Sleep(5 * time.Millisecond) {
		l.mu.Lock()
		if len(l.scans) > n {
			s := l.scans[n]
			l.mu.Unlock()
			return s
		}
		l.mu.Unlock()
	}
	t.Fatalf("no complexity.scan after %s", what)
	return complexity.Stats{}
}

// cxRescan presses the pane's 'r' and returns the scan it reports, which
// must come entirely from the cache.
func cxRescan(t *testing.T, m Model, l *scanLog, files int, after string) Model {
	t.Helper()
	n := l.count()
	out, cmd := m.Update(complexitypanel.RefreshMsg{})
	m = drainCmd(out.(Model), cmd)
	s := l.next(t, n, after)
	if s.Files != files || s.Cached != s.Files {
		t.Fatalf("rescan after %s = %+v, want all %d files cached", after, s, files)
	}
	return m
}

func TestComplexityScansAfterWindowActionsComeFromCache(t *testing.T) {
	m, _ := baselineApp(t) // the cold first walk wrote the cache
	ix := m.ComplexityIndex()
	files := ix.Stats().Files
	if files == 0 {
		t.Fatal("the fixture walk measured nothing")
	}
	scans := watchScans(ix)

	// Open the window and accept A into the baseline, then drop it again.
	out, _ := m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	for _, what := range []string{"a baseline accept", "a baseline drop"} {
		out, cmd := m.Update(ComplexityBaselineMsg{})
		m = drainCmd(out.(Model), cmd)
		m = cxRescan(t, m, scans, files, what)
	}

	// Grouping, persisted through the layout, and an unrelated config
	// reload: neither starts a walk, and the rescan behind them is warm.
	n := scans.count()
	out, _ = m.Update(ComplexityGroupMsg{})
	m = out.(Model)
	m = baselineReload(t, m, func(c *config.Config) { c.Complexity.TrendCommits = 7 })
	time.Sleep(50 * time.Millisecond)
	if scans.count() != n {
		t.Fatalf("grouping / a config reload started %d walk(s)", scans.count()-n)
	}
	m = cxRescan(t, m, scans, files, "a grouping change")

	// The Trend tab's recompute is the same 'r'.
	m.complexityPanel().SetTab(complexitypanel.TabTrend)
	m = cxRescan(t, m, scans, files, "the trend recompute")

	// Close and reopen the window.
	for i := 0; i < 2; i++ {
		out, _ = m.Update(ComplexityToggleMsg{})
		m = out.(Model)
	}
	m = cxRescan(t, m, scans, files, "closing and reopening the window")

	// The next session starts warm too.
	next := NewWith(registry.New(), host.FromConfig(config.Get()))
	nscans := watchScans(next.ComplexityIndex())
	next.Init()
	if s := nscans.next(t, 0, "the next session's walk"); s.Files != files || s.Cached != files {
		t.Fatalf("next session's walk = %+v, want all %d files cached", s, files)
	}

	// complexity.refresh is the explicit rebuild: everything re-measured.
	n = scans.count()
	out, _ = m.Update(ComplexityRefreshMsg{})
	m = out.(Model)
	if s := scans.next(t, n, "complexity.refresh"); s.Files != files || s.Cached != 0 {
		t.Fatalf("rebuild = %+v, want a cold walk of %d files", s, files)
	}
}

// TestComplexityOptionsKeepBraceGlobs: an exclude glob with a {a,b}
// alternation survives the flat config read in one piece, so the index
// built at startup equals the one a reload configures (#3085).
func TestComplexityOptionsKeepBraceGlobs(t *testing.T) {
	opts := complexityOptionsFrom(host.MapConfig{"complexity.exclude": "**/*.{js,ts}, vendor/**,{a,{b,c}}/x"})
	want := []string{"**/*.{js,ts}", "vendor/**", "{a,{b,c}}/x"}
	if len(opts.Exclude) != len(want) {
		t.Fatalf("exclude = %q, want %q", opts.Exclude, want)
	}
	for i := range want {
		if opts.Exclude[i] != want[i] {
			t.Fatalf("exclude = %q, want %q", opts.Exclude, want)
		}
	}
}
