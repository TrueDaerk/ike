package testresults

import (
	"strings"
	"testing"
)

// coverage_test.go covers the run summary's coverage percentage (#2081) and
// the per-file coverage listing in the detail column (#2246).

// covFiles is the per-file breakdown a coverage run hands in, worst first as
// the store orders it.
func covFiles() []CoverageFile {
	return []CoverageFile{
		{Path: "/proj/pkg/thin.go", Percent: 12.5},
		{Path: "/proj/pkg/sub/mid.go", Percent: 60},
		{Path: "/elsewhere/vendored.go", Percent: 100},
	}
}

// TestCoverageInHeader: a stamped percentage renders in the summary line.
func TestCoverageInHeader(t *testing.T) {
	m := filled(t)
	m.SetCoverage(73.25, nil)
	if v := m.View(); !strings.Contains(v, "73.2% coverage") {
		t.Fatalf("header must carry the coverage percentage:\n%s", v)
	}
}

// TestCoverageResetsOnNextRun: a plain run after a coverage run shows no
// percentage — StartRun clears the stamp.
func TestCoverageResetsOnNextRun(t *testing.T) {
	m := filled(t)
	m.SetCoverage(50, covFiles())
	m.StartRun("tests: pkg", "/proj/pkg")
	m.FinishRun(sampleResults(), "raw\n")
	if strings.Contains(m.View(), "coverage") {
		t.Fatal("a plain run must not show a stale coverage percentage")
	}
	if m.Update(key("c")); m.coverMode {
		t.Fatal("a plain run must not have a coverage listing to open")
	}
}

// TestCoverageListing: 'c' swaps the detail column for the per-file
// breakdown — run-wide figure first, then one line per file in the order it
// was handed in, paths relative to the run directory.
func TestCoverageListing(t *testing.T) {
	m := filled(t)
	// A wide panel so the footer hints are not clipped away.
	m.SetSize(140, 12)
	m.SetCoverage(57.5, covFiles())
	if strings.Contains(m.View(), "12.5%") {
		t.Fatal("the listing must stay closed until c is pressed")
	}
	m.Update(key("c"))
	lines := m.coverageLines()
	want := []string{
		"Coverage — 57.5% overall",
		"",
		" 12.5%  thin.go",
		" 60.0%  sub/mid.go",
		"100.0%  /elsewhere/vendored.go",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
	v := m.View()
	if !strings.Contains(v, "12.5%") || !strings.Contains(v, "sub/mid.go") {
		t.Fatalf("the detail column must render the listing:\n%s", v)
	}
	if !strings.Contains(v, "c detail") {
		t.Fatalf("the footer must offer the way back:\n%s", v)
	}
	m.Update(key("c"))
	if m.coverMode {
		t.Fatal("c must toggle the listing back off")
	}
	if !strings.Contains(m.View(), "c coverage") {
		t.Fatal("the footer must advertise the listing while coverage is loaded")
	}
}

// TestCoverageKeyInertWithoutData: without a coverage run 'c' does nothing
// and the footer stays silent about it.
func TestCoverageKeyInertWithoutData(t *testing.T) {
	m := filled(t)
	m.Update(key("c"))
	if m.coverMode {
		t.Fatal("c must not open an empty listing")
	}
	if strings.Contains(m.View(), "c coverage") {
		t.Fatal("the footer must not advertise a listing without coverage data")
	}
}
