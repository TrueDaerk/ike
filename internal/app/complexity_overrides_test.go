package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/config"
)

// complexity_overrides_test.go covers the per-file thresholds (#2960) on the
// app's surfaces: the hints, the Problems feed and their live reload.

func TestComplexityHintsForPerFileThresholds(t *testing.T) {
	fns := []complexity.Function{{Name: "f", Line: 3, CCN: 20, Cognitive: 4}}
	if h := complexityHintsFor(fns, complexity.Thresholds{CCN: 15, Cog: 15}, false, nil, false); len(h) != 1 || !h[0].Warn {
		t.Fatalf("CCN 20 under the global 15 must warn: %+v", h)
	}
	relaxed := complexity.Thresholds{CCN: 30, Cog: 30, Source: "legacy/**"}
	if h := complexityHintsFor(fns, relaxed, false, nil, false); len(h) != 1 || h[0].Warn {
		t.Fatalf("CCN 20 under ccn=30 must not warn: %+v", h)
	}
	if h := complexityHintsFor(fns, relaxed, true, nil, false); len(h) != 0 {
		t.Fatalf("warn mode drops the relaxed function's hint: %+v", h)
	}
}

func TestComplexityDiagnosticsPerFileThresholds(t *testing.T) {
	v := complexity.View{Files: map[string][]complexity.Function{
		"/p/legacy/old.go": {{Name: "Old", Line: 1, CCN: 20}},
		"/p/src/new.go":    {{Name: "New", Line: 1, CCN: 20}},
	}}
	c := config.Complexity{CCNWarn: 15, CognitiveWarn: 15, Overrides: []string{"legacy/** ccn=30"}}
	got := complexityDiagnostics(v, complexity.ResolverFor(c, "/p"), nil, nil, "/p")
	if len(got["/p/legacy/old.go"]) != 0 {
		t.Fatalf("legacy/** ccn=30 must keep Old off the feed: %+v", got)
	}
	if ds := got["/p/src/new.go"]; len(ds) != 1 || ds[0].Message != "function New: CCN 20 ≥ 15" {
		t.Fatalf("New stays on the feed under the globals: %+v", ds)
	}
	c.Overrides = []string{"legacy/** ccn=18"}
	ds := complexityDiagnostics(v, complexity.ResolverFor(c, "/p"), nil, nil, "/p")["/p/legacy/old.go"]
	if len(ds) != 1 || ds[0].Message != "function Old: CCN 20 ≥ 18" {
		t.Fatalf("the row must name the file's own threshold: %+v", ds)
	}
}

// TestComplexityOverridesLiveReload: a reloaded override re-feeds Problems
// and repaints the hints, whose key carries the resolved thresholds.
func TestComplexityOverridesLiveReload(t *testing.T) {
	m, src := complexityApp(t)
	out, cmd := m.openPathAt(src, 0, 0)
	m = out.(Model)
	m = settleHints(t, m, cmd)
	out, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	if k := m.cxHints.sent[src]; k.th.Overridden() || k.th.CCN != config.Get().Complexity.CCNWarn {
		t.Fatalf("sent key thresholds = %+v, want the globals", k.th)
	}
	t.Cleanup(func() { c, _ := config.Load(config.Options{}); config.Set(c) })
	reload := func(overrides ...string) {
		c, _ := config.Load(config.Options{})
		c.Complexity.CCNWarn, c.Complexity.CognitiveWarn = 1, 1
		c.Complexity.Overrides = overrides
		config.Set(c)
		out, _ := m.Update(config.ConfigReloadedMsg{Config: c})
		m = out.(Model)
	}
	canon := canonicalPath(src)
	rows := func() int { return len(m.probStore.Get(canon)) }
	reload()
	if rows() == 0 {
		t.Fatal("thresholds of 1 must put the file's functions on the feed")
	}
	ed := m.editorForPath(src)
	fns := m.ComplexityIndex().Snapshot().Files[canon]
	if h, ok := ed.ComplexityHintAt(fns[0].Line); !ok || !h.Warn {
		t.Fatalf("a hint under thresholds of 1 must warn: %+v %v", h, ok)
	}
	reload("complexity_panel.go ccn=200 cog=200 cmt=0")
	if n := rows(); n != 0 {
		t.Fatalf("the override must re-feed Problems without the file's rows, %d left", n)
	}
	if k := m.cxHints.sent[src]; k.th.Source != "complexity_panel.go" || k.th.CCN != 200 {
		t.Fatalf("sent key thresholds = %+v, want the override's", k.th)
	}
	ed = m.editorForPath(src)
	if h, ok := ed.ComplexityHintAt(fns[0].Line); !ok || h.Warn {
		t.Fatalf("the hints must repaint under the override: %+v %v", h, ok)
	}
}
