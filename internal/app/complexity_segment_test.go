package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/config"
	"ike/internal/vcs"
)

// complexity_segment_test.go covers the "Δcx" status segment (#2921):
// rendering per verdict, the hidden states, the arming seam with the
// Changes tab closed and the click target.

// cxSegmentConfig sets complexity.status_segment / enabled for one test.
func cxSegmentConfig(t *testing.T, on, enabled bool) {
	t.Helper()
	orig := config.Get()
	t.Cleanup(func() { config.Set(orig) })
	c, _ := config.Load(config.Options{})
	c.Complexity.StatusSegment = on
	c.Complexity.Enabled = enabled
	config.Set(c)
}

// cxSegmentApp is an app inside a (fake) repository whose stored diff turns
// base into head.
func cxSegmentApp(t *testing.T, base, head map[string][]complexity.Function) Model {
	t.Helper()
	m, _ := timeApp(t)
	m.vcs.snap = vcs.NewSnapshot("/r", map[string]vcs.FileStatus{"a.go": vcs.StatusModified})
	m.cxDiff.result = complexitypanel.Changes{Ready: true, Diff: complexity.DiffProject(base, head, complexity.Uniform(complexity.Thresholds{CCN: 15, Cog: 15, Cmt: 40}))}
	return m
}

func TestComplexitySegmentRendering(t *testing.T) {
	cxSegmentConfig(t, true, true)
	f := func(ccn int) map[string][]complexity.Function {
		return map[string][]complexity.Function{"/r/a.go": {{Name: "f", CCN: ccn, Line: 1}}}
	}
	cases := []struct {
		name       string
		base, head map[string][]complexity.Function
		want       string
	}{
		{"positive", f(17), f(21), "Δcx +4"},
		{"negative", f(21), f(19), "Δcx -2"},
		{"added", nil, f(18), "Δcx +3"},
		// ΣCCN rose below the threshold: the excess held (#2945).
		{"below threshold", f(2), f(6), "Δcx ±0"},
	}
	for _, tc := range cases {
		m := cxSegmentApp(t, tc.base, tc.head)
		if got := m.complexitySegment(); got != tc.want {
			t.Errorf("%s: segment = %q, want %q", tc.name, got, tc.want)
		}
	}
	// Zero: a changed function whose excess held (cognitive moved below
	// the threshold).
	m := cxSegmentApp(t,
		map[string][]complexity.Function{"/r/a.go": {{Name: "f", CCN: 3, Cognitive: 1, Line: 1}}},
		map[string][]complexity.Function{"/r/a.go": {{Name: "f", CCN: 3, Cognitive: 4, Line: 1}}})
	if got := m.complexitySegment(); got != "Δcx ±0" {
		t.Fatalf("zero segment = %q", got)
	}
	if m.complexitySegmentColor("Δcx ±0") != m.pal().InlayHint ||
		m.complexitySegmentColor("Δcx +4") != m.pal().Warning ||
		m.complexitySegmentColor("Δcx -2") != m.pal().Success {
		t.Fatal("verdict colours")
	}
	// The segment is a left slot and is tinted in the finished bar.
	var found bool
	for _, p := range renderParts(m, nil, statusLeft) {
		found = found || (p.id == "complexity" && p.text == "Δcx ±0")
	}
	if !found {
		t.Fatal("the left slots lack the segment")
	}
	if tinted := m.tintComplexitySegment(" a │ Δcx ±0 "); tinted == " a │ Δcx ±0 " || !containsPlain(tinted, "Δcx ±0") {
		t.Fatalf("tint = %q", tinted)
	}
}

func containsPlain(s, sub string) bool { return strings.Contains(ansi.Strip(s), sub) }

func TestComplexitySegmentHidden(t *testing.T) {
	f := map[string][]complexity.Function{"/r/a.go": {{Name: "f", CCN: 3, Line: 1}}}
	cxSegmentConfig(t, false, true)
	if s := cxSegmentApp(t, nil, f).complexitySegment(); s != "" {
		t.Fatalf("off by default: %q", s)
	}
	cxSegmentConfig(t, true, false)
	if s := cxSegmentApp(t, nil, f).complexitySegment(); s != "" {
		t.Fatalf("complexity.enabled=false must hide it: %q", s)
	}
	cxSegmentConfig(t, true, true)
	if s := cxSegmentApp(t, f, f).complexitySegment(); s != "" {
		t.Fatalf("an empty diff must hide it: %q", s)
	}
	m := cxSegmentApp(t, nil, f)
	m.vcs.snap = nil
	if s := m.complexitySegment(); s != "" {
		t.Fatalf("outside git it must hide: %q", s)
	}
}

// TestComplexitySegmentArmsDiff: with the Changes tab closed, the segment
// alone keeps the diff armed on a VCS refresh; off, nothing is scheduled.
func TestComplexitySegmentArmsDiff(t *testing.T) {
	cxSegmentConfig(t, false, true)
	m, _ := timeApp(t)
	if m.complexityPanel() != nil {
		t.Fatal("the window must start closed")
	}
	snap := vcs.NewSnapshot("/r", map[string]vcs.FileStatus{"a.go": vcs.StatusModified})
	out, _ := m.Update(vcs.SnapshotMsg{Snap: snap})
	m = out.(Model)
	if m.cxDiff.tickArmed {
		t.Fatal("segment off and tab closed: nothing may arm")
	}
	cxSegmentConfig(t, true, true)
	out, cmd := m.Update(vcs.SnapshotMsg{Snap: vcs.NewSnapshot("/r", map[string]vcs.FileStatus{"b.go": vcs.StatusModified})})
	m = out.(Model)
	if cmd == nil || !m.cxDiff.tickArmed {
		t.Fatal("segment on: the refresh must arm the diff")
	}
	out, cmd = m.Update(complexityDiffTickMsg{})
	m = out.(Model)
	if cmd == nil || !m.cxDiff.computing {
		t.Fatal("the tick must launch the computation without the pane")
	}
}

// TestComplexitySegmentClickOpensChanges: the click opens the window on
// its Changes tab.
func TestComplexitySegmentClickOpensChanges(t *testing.T) {
	cxSegmentConfig(t, true, true)
	m, _ := timeApp(t)
	m.openComplexityChanges()
	p := m.complexityPanel()
	if p == nil || p.Tab() != complexitypanel.TabChanges {
		t.Fatal("the click must open the Changes tab")
	}
}
