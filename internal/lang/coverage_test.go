package lang

import (
	"reflect"
	"testing"
)

// coverage_test.go covers the neutral coverage seam (#2081): argv synthesis
// appends the language's coverage arguments after the structured ones, the
// parser resolves per language, and a language without the hooks simply has
// no coverage mode — no engine special-casing anywhere.

func coverSpec(ext string) *TestSpec {
	s := tstSpec()
	s.FilePattern = `_test\.` + ext + `$`
	s.StructuredArgs = []string{"--structured"}
	s.ParseOutput = func(string) []TestResult { return nil }
	s.CoverArgs = func(profile string) []string { return []string{"--cover-out", profile} }
	s.ParseCover = func(profile, dir string) ([]FileCoverage, error) {
		return []FileCoverage{{Path: "/x", Lines: map[int]CoverKind{1: CoverCovered}}}, nil
	}
	return s
}

// TestCoverageArgvAppendsCoverArgs: file scope argv = structured argv plus the
// language's coverage arguments carrying the profile path.
func TestCoverageArgvAppendsCoverArgs(t *testing.T) {
	Register(Language{ID: "covlang", Extensions: []string{"cov"}, Test: coverSpec("cov")})
	argv, ok := TestCoverageArgv("/r", "/r/a_test.cov", nil, "", "/tmp/p.out")
	if !ok {
		t.Fatal("coverage argv must synthesize for a language with both hooks")
	}
	base, ok := TestStructuredArgv("/r", "/r/a_test.cov", nil, "")
	if !ok {
		t.Fatal("structured argv must synthesize")
	}
	want := append(base, "--cover-out", "/tmp/p.out")
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
}

// TestCoverageArgvNeedsBothHooks: a language missing either hook has no
// coverage mode.
func TestCoverageArgvNeedsBothHooks(t *testing.T) {
	argsOnly := coverSpec("aon")
	argsOnly.ParseCover = nil
	Register(Language{ID: "argsonly", Extensions: []string{"aon"}, Test: argsOnly})
	if _, ok := TestCoverageArgv("/r", "/r/a_test.aon", nil, "", "/tmp/p"); ok {
		t.Fatal("CoverArgs without ParseCover must not enable coverage")
	}
	nocov := coverSpec("ncv")
	nocov.CoverArgs, nocov.ParseCover = nil, nil
	Register(Language{ID: "nocov", Extensions: []string{"ncv"}, Test: nocov})
	if _, ok := TestCoverageArgv("/r", "/r/a_test.ncv", nil, "", "/tmp/p"); ok {
		t.Fatal("a language without coverage hooks must report ok=false")
	}
}

// TestCoverParserResolvesPerLanguage: the parser lookup mirrors TestParser.
func TestCoverParserResolvesPerLanguage(t *testing.T) {
	Register(Language{ID: "covlang", Extensions: []string{"cov"}, Test: coverSpec("cov")})
	parse, ok := CoverParser("covlang")
	if !ok {
		t.Fatal("CoverParser must resolve for a language with ParseCover")
	}
	files, err := parse("ignored", "ignored")
	if err != nil || len(files) != 1 || files[0].Lines[1] != CoverCovered {
		t.Fatalf("parser must pass through: %v %v", files, err)
	}
	if _, ok := CoverParser("nocov"); ok {
		t.Fatal("CoverParser must report ok=false without ParseCover")
	}
}
