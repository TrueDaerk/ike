package langphp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/lang"
)

// fixture reads a captured PHPUnit run from testdata.
func fixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(raw)
}

// find returns the result with the given "/"-nested name.
func find(t *testing.T, results []lang.TestResult, name string) lang.TestResult {
	t.Helper()
	for _, r := range results {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no result named %q in %d results", name, len(results))
	return lang.TestResult{}
}

func TestParsePHPUnit9(t *testing.T) {
	results := parsePHPUnit(fixture(t, "phpunit9.teamcity.txt"))
	if len(results) != 5 {
		t.Fatalf("got %d results, want 5: %+v", len(results), results)
	}
	for _, r := range results {
		if r.Group != `App\Tests\CalculatorTest` {
			t.Errorf("%s: group %q, want the test class", r.Name, r.Group)
		}
	}

	pass := find(t, results, "testAdd")
	if pass.Status != lang.TestPass {
		t.Errorf("testAdd status = %q, want pass", pass.Status)
	}
	if pass.Elapsed != 0.007 {
		t.Errorf("testAdd elapsed = %v, want 0.007", pass.Elapsed)
	}
	if pass.RerunID != "testAdd" {
		t.Errorf("testAdd rerun id = %q", pass.RerunID)
	}

	fail := find(t, results, "testDivideByZero")
	if fail.Status != lang.TestFail {
		t.Errorf("testDivideByZero status = %q, want fail", fail.Status)
	}
	if fail.File != "/srv/app/tests/CalculatorTest.php" || fail.Line != 34 {
		t.Errorf("testDivideByZero location = %s:%d, want …/CalculatorTest.php:34", fail.File, fail.Line)
	}
	// The message's escaped quotes are decoded and the comparison attached.
	if !strings.Contains(fail.Output, `Failed asserting that '0' matches expected '1'.`) {
		t.Errorf("testDivideByZero output missing the unescaped message:\n%s", fail.Output)
	}
	for _, want := range []string{"--- Expected", "1", "+++ Actual", "0"} {
		if !strings.Contains(fail.Output, want) {
			t.Errorf("testDivideByZero output missing %q:\n%s", want, fail.Output)
		}
	}

	skip := find(t, results, "testLegacyMode")
	if skip.Status != lang.TestSkip {
		t.Errorf("testLegacyMode status = %q, want skip", skip.Status)
	}
	if !strings.Contains(skip.Output, "Legacy mode was removed in 8.0") {
		t.Errorf("testLegacyMode output = %q", skip.Output)
	}
}

// Data-provider cases nest as subtests under their method and re-run by the
// bare method name — `--filter` cannot address a single data set.
func TestParsePHPUnitDataProvider(t *testing.T) {
	results := parsePHPUnit(fixture(t, "phpunit9.teamcity.txt"))

	first := find(t, results, "testSum/#0")
	if first.Status != lang.TestPass || first.RerunID != "testSum" {
		t.Errorf("testSum/#0 = %+v, want a passing subtest re-running as testSum", first)
	}

	named := find(t, results, `testSum/"negatives"`)
	if named.Status != lang.TestFail || named.RerunID != "testSum" {
		t.Errorf(`testSum/"negatives" = %+v, want a failing subtest`, named)
	}
	// The trace's first frame is inside PHPUnit itself; the frame in the
	// test's own file is the one Enter should jump to.
	if named.File != "/srv/app/tests/CalculatorTest.php" || named.Line != 52 {
		t.Errorf("data set failure location = %s:%d, want …/CalculatorTest.php:52", named.File, named.Line)
	}
}

// PHPUnit 10 reports the qualified Class::method as the test name; the parser
// normalizes it to the method so the tree does not repeat the group.
func TestParsePHPUnit10(t *testing.T) {
	results := parsePHPUnit(fixture(t, "phpunit10.teamcity.txt"))
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2: %+v", len(results), results)
	}
	pass := find(t, results, "testTrim")
	if pass.Group != `App\Tests\StringTest` || pass.Status != lang.TestPass || pass.Elapsed != 0.012 {
		t.Errorf("testTrim = %+v", pass)
	}
	fail := find(t, results, "testUpper")
	if fail.Status != lang.TestFail || fail.File != "/srv/app/tests/StringTest.php" || fail.Line != 27 {
		t.Errorf("testUpper = %+v", fail)
	}
	if fail.RerunID != "testUpper" {
		t.Errorf("testUpper rerun id = %q", fail.RerunID)
	}
}

// Without a locationHint the enclosing suite names the group.
func TestParsePHPUnitSuiteFallback(t *testing.T) {
	out := strings.Join([]string{
		`##teamcity[testSuiteStarted name='LegacyTest' flowId='1']`,
		`##teamcity[testStarted name='testOld' flowId='1']`,
		`##teamcity[testFinished name='testOld' duration='0' flowId='1']`,
		`##teamcity[testSuiteFinished name='LegacyTest' flowId='1']`,
	}, "\n")
	results := parsePHPUnit(out)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Group != "LegacyTest" || results[0].Name != "testOld" {
		t.Errorf("result = %+v, want group LegacyTest / name testOld", results[0])
	}
	if results[0].Elapsed != 0 {
		t.Errorf("elapsed = %v, want 0", results[0].Elapsed)
	}
}

// Output without service messages (a bootstrap failure, a plain run) leaves
// the raw-output fallback in place.
func TestParsePHPUnitNoMessages(t *testing.T) {
	out := "PHP Fatal error:  Uncaught Error: Class \"PHPUnit\\Framework\\TestCase\" not found\n"
	if results := parsePHPUnit(out); results != nil {
		t.Errorf("got %+v, want nil for output without service messages", results)
	}
}

func TestTeamCityAttrs(t *testing.T) {
	attrs := teamcityAttrs(`name='a|'b' details='x|ny||z' flowId='7'`)
	want := map[string]string{"name": "a'b", "details": "x\ny|z", "flowId": "7"}
	for k, v := range want {
		if attrs[k] != v {
			t.Errorf("attr %s = %q, want %q", k, attrs[k], v)
		}
	}
	if len(attrs) != len(want) {
		t.Errorf("got %d attrs, want %d: %v", len(attrs), len(want), attrs)
	}
}
