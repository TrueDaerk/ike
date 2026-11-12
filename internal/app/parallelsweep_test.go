package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// parallelsweep_test.go keeps the package's tests running in parallel (#3135).
//
// Every top-level test opens with one of two lines:
//
//	t.Parallel()
//	// serial: <why>
//
// A test is parallel unless it touches state the whole process shares: the
// environment (t.Setenv — which is also how a test takes a real state store,
// withStore), the working directory, a package-level seam or switch, the
// language or plugin registry, the process-wide configuration, the wall clock
// (a poll with a deadline is load-sensitive), or a counter every model bumps.
// Go runs the serial tests first, one after the other, and then the parallel
// ones together — so a serial test may still change such state for itself,
// and a parallel test may rely on nobody changing it under its feet.
//
// What makes the parallel default possible is that a test model has no state
// store unless the test asks for one (TestMain): nothing a model persists can
// reach another model.

var testFuncRE = regexp.MustCompile(`(?m)^func (Test\w*)\(t \*testing\.T\) \{\n([^\n]*)\n`)

// TestEveryTestIsParallelOrSaysWhy is the sweep guard: a new test that is
// neither marked parallel nor carries its reason fails here, so the suite does
// not drift back to one test at a time.
func TestEveryTestIsParallelOrSaysWhy(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no test files found: the guard must run in the package directory")
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range testFuncRE.FindAllStringSubmatch(string(data), -1) {
			name, first := m[1], m[2]
			switch {
			case first == "\tt.Parallel()":
			case strings.HasPrefix(first, "\t// serial: ") && len(first) > len("\t// serial: "):
			default:
				t.Errorf("%s: %s must open with t.Parallel() or with a \"// serial: <why>\" line naming the process-wide state it touches", f, name)
			}
		}
	}
}

// TestSerialReasonsAreNotStale is the companion: a test marked serial for
// t.Setenv or a store that no longer uses either should run in parallel
// again. Only the two reasons a line-level check can verify are covered.
func TestSerialReasonsAreNotStale(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	bodyRE := regexp.MustCompile(`(?ms)^func (Test\w*)\(t \*testing\.T\) \{\n\t// serial: (t\.Setenv|withStore: t\.Setenv)\n(.*?)^\}\n`)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range bodyRE.FindAllStringSubmatch(string(data), -1) {
			name, reason, body := m[1], m[2], m[3]
			want := "t.Setenv("
			if reason == "withStore: t.Setenv" {
				want = "withStore(t)"
			}
			if !strings.Contains(body, want) {
				t.Errorf("%s: %s says %q but never calls %s — fix the reason or make the test parallel", f, name, "serial: "+reason, want)
			}
		}
	}
}
