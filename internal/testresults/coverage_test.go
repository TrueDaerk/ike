package testresults

import (
	"strings"
	"testing"
)

// coverage_test.go covers the run summary's coverage percentage (#2081).

// TestCoverageInHeader: a stamped percentage renders in the summary line.
func TestCoverageInHeader(t *testing.T) {
	m := filled(t)
	m.SetCoverage(73.25)
	if v := m.View(); !strings.Contains(v, "73.2% coverage") {
		t.Fatalf("header must carry the coverage percentage:\n%s", v)
	}
}

// TestCoverageResetsOnNextRun: a plain run after a coverage run shows no
// percentage — StartRun clears the stamp.
func TestCoverageResetsOnNextRun(t *testing.T) {
	m := filled(t)
	m.SetCoverage(50)
	m.StartRun("tests: pkg", "/proj/pkg")
	m.FinishRun(sampleResults(), "raw\n")
	if strings.Contains(m.View(), "coverage") {
		t.Fatal("a plain run must not show a stale coverage percentage")
	}
}
