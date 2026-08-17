package lang

import (
	"reflect"
	"testing"
)

// tstSpec mirrors the Go plugin's declaration shape without importing the
// plugin (that would cycle): named groups, kind templates, exclusion.
func tstSpec() *TestSpec {
	return &TestSpec{
		FilePattern: `_test\.tst$`,
		Pattern:     `^func (?P<name>(?P<kind>Test|Benchmark)(?:[^a-z\s(][0-9A-Za-z_]*)?)\s*\(`,
		Kinds: map[string][]string{
			"Test":      {"{interpreter}", "test", "-run", "^{name}$"},
			"Benchmark": {"{interpreter}", "test", "-bench", "^{name}$", "-run", "^$"},
		},
		FileArgv: []string{"{interpreter}", "test"},
		Tool:     "tsttool",
		Exclude:  []string{"TestMain"},
	}
}

func registerTst(t *testing.T) {
	t.Helper()
	Register(Language{ID: "tstlang", Extensions: []string{"tst"}, Test: tstSpec()})
}

func TestTestsInFileDetection(t *testing.T) {
	registerTst(t)
	lines := []string{
		"package x",
		"func TestAlpha(t *testing.T) {",   // 1
		"func helper() {",                  // no
		"func Testify(t *testing.T) {",     // lowercase after prefix: no
		"func BenchmarkBeta(b *testing.B)", // 4
		"func TestMain(m *testing.M) {",    // excluded
		"func Test(t *testing.T) {",        // bare prefix: yes (6)
		"  func TestIndented(t *testing.T) {", // not line-anchored: no
	}
	got := TestsInFile("pkg/x_test.tst", lines)
	want := []TestMatch{
		{Line: 1, Name: "TestAlpha", Kind: "Test"},
		{Line: 4, Name: "BenchmarkBeta", Kind: "Benchmark"},
		{Line: 6, Name: "Test", Kind: "Test"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TestsInFile = %+v, want %+v", got, want)
	}
}

func TestTestsInFileRespectsFilePattern(t *testing.T) {
	registerTst(t)
	lines := []string{"func TestAlpha(t *testing.T) {"}
	if got := TestsInFile("pkg/x.tst", lines); got != nil {
		t.Fatalf("non-test file must yield no matches, got %+v", got)
	}
	if HasTests("pkg/x.tst") {
		t.Fatal("HasTests must be false for a non-test file")
	}
	if !HasTests("pkg/x_test.tst") {
		t.Fatal("HasTests must be true for a test file")
	}
}

func TestTestArgvTemplating(t *testing.T) {
	registerTst(t)
	argv, ok := TestArgv(".", "pkg/x_test.tst", TestMatch{Name: "TestAlpha", Kind: "Test"}, "")
	if !ok {
		t.Fatal("TestArgv must synthesize for a declared kind")
	}
	// No interpreter resolves for tstlang, so the Tool fallback substitutes.
	want := []string{"tsttool", "test", "-run", "^TestAlpha$"}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	argv, ok = TestArgv(".", "pkg/x_test.tst", TestMatch{Name: "BenchmarkBeta", Kind: "Benchmark"}, "")
	if !ok || !reflect.DeepEqual(argv, []string{"tsttool", "test", "-bench", "^BenchmarkBeta$", "-run", "^$"}) {
		t.Fatalf("benchmark argv = %v, ok=%v", argv, ok)
	}
	if _, ok := TestArgv(".", "pkg/x_test.tst", TestMatch{Name: "FuzzX", Kind: "Fuzz"}, ""); ok {
		t.Fatal("an undeclared kind must not synthesize")
	}
}

func TestTestArgvExplicitInterpreter(t *testing.T) {
	registerTst(t)
	argv, ok := TestArgv(".", "pkg/x_test.tst", TestMatch{Name: "TestAlpha", Kind: "Test"}, "/opt/custom/tst")
	if !ok || argv[0] != "/opt/custom/tst" {
		t.Fatalf("explicit interpreter must win: %v, ok=%v", argv, ok)
	}
}

func TestTestFileArgv(t *testing.T) {
	registerTst(t)
	argv, ok := TestFileArgv(".", "pkg/x_test.tst", "")
	if !ok || !reflect.DeepEqual(argv, []string{"tsttool", "test"}) {
		t.Fatalf("file argv = %v, ok=%v", argv, ok)
	}
	if _, ok := TestFileArgv(".", "pkg/x.tst", ""); ok {
		t.Fatal("non-test file must not synthesize a file argv")
	}
}

// structSpec is tstSpec plus the structured-output seam (#1911), the Go
// plugin's declaration shape.
func structSpec() *TestSpec {
	s := tstSpec()
	s.FilePattern = `_test\.[a-z]+$` // matches the .stt/.pyt registrations below
	s.StructuredArgs = []string{"-json"}
	s.ParseOutput = func(out string) []TestResult {
		return []TestResult{{Group: "pkg", Name: "TestAlpha", Status: TestPass}}
	}
	s.FailedArgv = []string{"{interpreter}", "test", "-run", "^({names})$"}
	s.NamesJoin = "|"
	return s
}

func TestStructuredArgvAppendsArgs(t *testing.T) {
	Register(Language{ID: "structlang", Extensions: []string{"stt"}, Test: structSpec()})
	argv, ok := TestStructuredArgv(".", "pkg/x_test.stt", nil, "")
	if !ok || !reflect.DeepEqual(argv, []string{"tsttool", "test", "-json"}) {
		t.Fatalf("file-scope structured argv = %v, ok=%v", argv, ok)
	}
	argv, ok = TestStructuredArgv(".", "pkg/x_test.stt", &TestMatch{Name: "TestAlpha", Kind: "Test"}, "")
	if !ok || !reflect.DeepEqual(argv, []string{"tsttool", "test", "-run", "^TestAlpha$", "-json"}) {
		t.Fatalf("single-test structured argv = %v, ok=%v", argv, ok)
	}
	// A language without a parser never synthesizes a structured argv — the
	// caller falls back to the raw run-terminal path.
	registerTst(t)
	if _, ok := TestStructuredArgv(".", "pkg/x_test.tst", nil, ""); ok {
		t.Fatal("no parser must mean no structured argv")
	}
}

func TestFailedArgvJoinsNames(t *testing.T) {
	Register(Language{ID: "structlang", Extensions: []string{"stt"}, Test: structSpec()})
	argv, ok := TestFailedArgv(".", "pkg/x_test.stt", []string{"TestA", "TestB"}, "")
	want := []string{"tsttool", "test", "-run", "^(TestA|TestB)$", "-json"}
	if !ok || !reflect.DeepEqual(argv, want) {
		t.Fatalf("failed argv = %v, want %v", argv, want)
	}
	if _, ok := TestFailedArgv(".", "pkg/x_test.stt", nil, ""); ok {
		t.Fatal("no ids must synthesize nothing")
	}
}

func TestFailedArgvWholeElementExpansion(t *testing.T) {
	s := structSpec()
	// The pytest shape: {names} as a whole element expands to one element
	// per id, {file} to the test file's base name.
	s.FailedArgv = []string{"{interpreter}", "-m", "pytest", "-k", "{names}", "{file}"}
	s.NamesJoin = " or "
	Register(Language{ID: "pylike", Extensions: []string{"pyt"}, Test: s})
	argv, ok := TestFailedArgv(".", "pkg/x_test.pyt", []string{"test_a", "test_b"}, "")
	want := []string{"tsttool", "-m", "pytest", "-k", "test_a or test_b", "x_test.pyt", "-json"}
	if !ok || !reflect.DeepEqual(argv, want) {
		t.Fatalf("failed argv = %v, want %v", argv, want)
	}
	// An empty NamesJoin selects the element-per-id expansion (node ids).
	s.FailedArgv = []string{"{interpreter}", "-m", "pytest", "{names}"}
	s.NamesJoin = ""
	Register(Language{ID: "pylike", Extensions: []string{"pyt"}, Test: s})
	argv, ok = TestFailedArgv(".", "pkg/x_test.pyt", []string{"a::b", "a::c"}, "")
	want = []string{"tsttool", "-m", "pytest", "a::b", "a::c", "-json"}
	if !ok || !reflect.DeepEqual(argv, want) {
		t.Fatalf("per-id argv = %v, want %v", argv, want)
	}
}

func TestTestParserLookup(t *testing.T) {
	Register(Language{ID: "structlang", Extensions: []string{"stt"}, Test: structSpec()})
	p, ok := TestParser("structlang")
	if !ok || p == nil {
		t.Fatal("declared parser must resolve")
	}
	if res := p("x"); len(res) != 1 || res[0].Name != "TestAlpha" {
		t.Fatalf("parser result = %+v", res)
	}
	if _, ok := TestParser("tstlang"); ok {
		t.Fatal("a language without ParseOutput must report no parser")
	}
	if _, ok := TestParser("nosuch"); ok {
		t.Fatal("an unknown language must report no parser")
	}
}

func TestTestSeamAbsentLanguage(t *testing.T) {
	Register(Language{ID: "notests", Extensions: []string{"nts"}})
	if TestsInFile("a_test.nts", []string{"func TestX() {"}) != nil {
		t.Fatal("a language without a TestSpec must detect nothing")
	}
	if _, ok := TestArgv(".", "a_test.nts", TestMatch{Name: "TestX"}, ""); ok {
		t.Fatal("TestArgv must fail without a TestSpec")
	}
}
