package langgo

import (
	"reflect"
	"testing"

	"ike/internal/lang"
)

// The init() registration carries the Go TestSpec (#1150); these tests pin
// its detection and command synthesis end to end through the lang seam.

func TestGoTestDetection(t *testing.T) {
	lines := []string{
		"package x",
		"func TestParse(t *testing.T) {",
		"func testHelper() {",
		"func Testify(t *testing.T) {", // lowercase after prefix — not a test
		"func BenchmarkParse(b *testing.B) {",
		"func FuzzParse(f *testing.F) {",
		"func TestMain(m *testing.M) {", // orchestrator, excluded
		"func Test(t *testing.T) {",     // bare Test is runnable
	}
	got := lang.TestsInFile("pkg/parse_test.go", lines)
	want := []lang.TestMatch{
		{Line: 1, Name: "TestParse", Kind: "Test"},
		{Line: 4, Name: "BenchmarkParse", Kind: "Benchmark"},
		{Line: 5, Name: "FuzzParse", Kind: "Fuzz"},
		{Line: 7, Name: "Test", Kind: "Test"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TestsInFile = %+v, want %+v", got, want)
	}
	// Only _test.go files carry markers.
	if lang.TestsInFile("pkg/parse.go", lines) != nil {
		t.Fatal("a non-_test.go file must yield no matches")
	}
}

func TestGoTestArgv(t *testing.T) {
	cases := []struct {
		m    lang.TestMatch
		want []string
	}{
		{lang.TestMatch{Name: "TestParse", Kind: "Test"}, []string{"go", "test", "-run", "^TestParse$"}},
		{lang.TestMatch{Name: "BenchmarkParse", Kind: "Benchmark"}, []string{"go", "test", "-bench", "^BenchmarkParse$", "-run", "^$"}},
		{lang.TestMatch{Name: "FuzzParse", Kind: "Fuzz"}, []string{"go", "test", "-run", "^FuzzParse$"}},
	}
	for _, c := range cases {
		argv, ok := lang.TestArgv(".", "pkg/parse_test.go", c.m, "go")
		if !ok || !reflect.DeepEqual(argv, c.want) {
			t.Errorf("TestArgv(%s) = %v, ok=%v, want %v", c.m.Name, argv, ok, c.want)
		}
	}
	argv, ok := lang.TestFileArgv(".", "pkg/parse_test.go", "go")
	if !ok || !reflect.DeepEqual(argv, []string{"go", "test"}) {
		t.Fatalf("TestFileArgv = %v, ok=%v", argv, ok)
	}
}
