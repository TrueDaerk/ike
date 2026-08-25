package lang

// Test-runner seam (#1150): a language declares how its test functions are
// detected and run, as data — a line-anchored regular expression plus argv
// templates — so PHP(unit)/pytest can follow Go without engine edits.
//
// Detection is deliberately regex-based rather than documentSymbol or
// Tree-sitter: it works without a running language server and in CGO_ENABLED=0
// builds, costs one O(lines) scan per buffer edit, and test declarations in
// the supported languages are strictly line-anchored (gofmt guarantees
// `func TestX(` starts a line). Command synthesis produces an argv array that
// is executed directly (no shell), so quoting is shell-agnostic by
// construction — no shell ever parses the command.

import (
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// TestSpec declares a language's test detection and run commands.
type TestSpec struct {
	// FilePattern restricts detection to files whose base name matches the
	// regexp ("" = every file of the language), e.g. `_test\.go$`.
	FilePattern string
	// Pattern matches a test declaration line. It must define a named group
	// `name` capturing the runnable test's full name; an optional named group
	// `kind` selects the argv template in Kinds.
	Pattern string
	// Kinds maps a captured kind ("" when Pattern has no `kind` group) to the
	// argv template running exactly one test. A whole element "{interpreter}"
	// expands to the resolved toolchain binary (Runner's command prefix, Tool
	// as fallback); {name} substitutes anywhere in an element (the test's
	// name, verbatim — Pattern-captured names are identifiers, safe inside a
	// `-run ^…$` anchor).
	Kinds map[string][]string
	// FileArgv is the argv template running every test in the file's scope
	// (Go: the package — the argv runs with cwd = the file's directory).
	FileArgv []string
	// Tool is the fallback binary name when no interpreter resolves.
	Tool string
	// Exclude lists detected names that are never runnable tests (Go's
	// TestMain).
	Exclude []string

	// Structured-output support (#1911) — all three optional; a language
	// without them keeps the raw run-terminal path.
	//
	// StructuredArgs are appended to a synthesized test argv when the run's
	// output is captured and parsed for the Test Results tool window (Go:
	// "-json"; pytest: "-v"). They must not change *which* tests run, only
	// the output format.
	StructuredArgs []string
	// ParseOutput parses a captured run's combined stdout+stderr into
	// structured results. Nil means the language has no parser and test runs
	// stay raw terminal output.
	ParseOutput func(output string) []TestResult
	// FailedArgv is the argv template re-running a named set of tests by
	// their RerunIDs. Besides {interpreter} and {file} it must contain the
	// placeholder {names}: with NamesJoin set it expands to the ids joined by
	// it (Go: "-run" "^({names})$" with "|"; pytest: "-k" "{names}" with
	// " or "); with NamesJoin empty, a whole-element "{names}" expands to one
	// argv element per id.
	FailedArgv []string
	// NamesJoin joins RerunIDs substituted into {names}; "" selects the
	// element-per-id expansion.
	NamesJoin string

	// Coverage support (#2081) — both optional; a language declaring neither
	// has no run-with-coverage mode. The seam is deliberately neutral: the
	// engine only hands over a profile path and receives the per-file line
	// model back, so coverage.py or phpunit's clover XML plug in without
	// engine edits.
	//
	// CoverArgs returns the extra argv elements making the test run write its
	// coverage data to profile (Go: "-coverprofile=<profile>"). Appended after
	// StructuredArgs; must not change which tests run.
	CoverArgs func(profile string) []string
	// ParseCover parses the coverage data the run wrote to profile into the
	// neutral per-file line-coverage model. dir is the run's working
	// directory, for resolving relative or import-qualified paths in the
	// profile to absolute file paths.
	ParseCover func(profile, dir string) ([]FileCoverage, error)

	// Project-rooted runs (#1926) — both optional.
	//
	// RunAtRoot runs the language's tests with cwd = the project root
	// instead of the test file's directory, and expands {file} to the file's
	// root-relative path rather than its base name. PHPUnit needs it: the
	// composer autoloader and phpunit.xml live at the root, and a run
	// started from tests/ would find neither.
	RunAtRoot bool
	// Runner, when non-nil, resolves a whole-element "{interpreter}" to the
	// command prefix running root's test tool — the project-local binary
	// (PHP: vendor/bin/phpunit) rather than the language interpreter the
	// toolchain detection yields. An empty return falls back to Tool.
	Runner func(root string) []string
}

// TestStatus is one parsed test's outcome.
type TestStatus string

const (
	TestPass TestStatus = "pass"
	TestFail TestStatus = "fail"
	TestSkip TestStatus = "skip"
)

// TestResult is one parsed test outcome of a captured run (#1911).
type TestResult struct {
	// Group is the tree's top grouping node: the Go package path, the pytest
	// file path — whatever the language's natural container is.
	Group string
	// Name is the test's display name; "/"-separated segments nest as
	// subtests in the result tree ("TestFoo/sub").
	Name string
	// Status is the outcome; parsers only emit finished tests.
	Status TestStatus
	// Elapsed is the test's duration in seconds (0 when unknown).
	Elapsed float64
	// Output is the test's buffered own output, shown in the detail pane.
	Output string
	// File/Line locate the failure (1-based line; both zero when unknown).
	// File may be relative to the run's working directory.
	File string
	Line int
	// RerunID is the token FailedArgv re-runs this test by: the Go top-level
	// test function name, the pytest test name for a -k expression.
	RerunID string
}

// TestMatch is one detected test function.
type TestMatch struct {
	// Line is the 0-based buffer line of the declaration.
	Line int
	// Name is the runnable test name (Pattern's `name` group).
	Name string
	// Kind is the captured kind keyword ("Test", "Benchmark", …; "" when the
	// language's Pattern has no kind group).
	Kind string
}

// Compiled regexps are cached per pattern string, so TestSpec stays plain
// data plugins declare without importing regexp; a malformed pattern simply
// disables the feature for its language.
var testRegexps sync.Map // pattern string -> *regexp.Regexp

func compiledPattern(pattern string) *regexp.Regexp {
	if v, ok := testRegexps.Load(pattern); ok {
		return v.(*regexp.Regexp)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	testRegexps.Store(pattern, re)
	return re
}

// testSpecFor resolves path's language and its TestSpec, checking FilePattern.
func testSpecFor(path string) (Language, *TestSpec, bool) {
	l, found := ByPath(path)
	if !found || l.Test == nil || l.Test.Pattern == "" {
		return Language{}, nil, false
	}
	if fp := l.Test.FilePattern; fp != "" {
		re := compiledPattern(fp)
		if re == nil || !re.MatchString(filepath.Base(path)) {
			return Language{}, nil, false
		}
	}
	return l, l.Test, true
}

// TestsInFile scans lines (the buffer's content) for path's language's test
// declarations. Nil when the language declares no tests or the file is not a
// test file.
func TestsInFile(path string, lines []string) []TestMatch {
	_, spec, ok := testSpecFor(path)
	if !ok {
		return nil
	}
	re := compiledPattern(spec.Pattern)
	if re == nil {
		return nil
	}
	nameIdx, kindIdx := -1, -1
	for i, n := range re.SubexpNames() {
		switch n {
		case "name":
			nameIdx = i
		case "kind":
			kindIdx = i
		}
	}
	if nameIdx < 0 {
		return nil
	}
	var out []TestMatch
scan:
	for i, line := range lines {
		sub := re.FindStringSubmatch(line)
		if sub == nil || sub[nameIdx] == "" {
			continue
		}
		name := sub[nameIdx]
		for _, ex := range spec.Exclude {
			if name == ex {
				continue scan
			}
		}
		kind := ""
		if kindIdx >= 0 {
			kind = sub[kindIdx]
		}
		out = append(out, TestMatch{Line: i, Name: name, Kind: kind})
	}
	return out
}

// TestArgv synthesizes the argv running exactly the test t declared in the
// file at path; the argv is meant to run with cwd = the file's directory.
// explicit is the user's configured interpreter for the language. ok=false
// when the language declares no template for t's kind.
func TestArgv(root, path string, t TestMatch, explicit string) ([]string, bool) {
	l, spec, ok := testSpecFor(path)
	if !ok {
		return nil, false
	}
	tpl, found := spec.Kinds[t.Kind]
	if !found || len(tpl) == 0 {
		return nil, false
	}
	return expandTestArgv(tpl, t.Name, testCommand(l.ID, root, spec, explicit), testFileArg(spec, root, path)), true
}

// TestFileArgv synthesizes the argv running every test in path's scope (the
// file's package directory — run it with cwd = filepath.Dir(path)).
func TestFileArgv(root, path, explicit string) ([]string, bool) {
	l, spec, ok := testSpecFor(path)
	if !ok || len(spec.FileArgv) == 0 {
		return nil, false
	}
	return expandTestArgv(spec.FileArgv, "", testCommand(l.ID, root, spec, explicit), testFileArg(spec, root, path)), true
}

// TestStructuredArgv is TestArgv/TestFileArgv (t nil = file scope) plus the
// language's StructuredArgs, for a run whose output is captured and parsed
// into the Test Results tool (#1911). ok=false when the base argv does not
// synthesize or the language declares no parser.
func TestStructuredArgv(root, path string, t *TestMatch, explicit string) ([]string, bool) {
	_, spec, specOK := testSpecFor(path)
	if !specOK || spec.ParseOutput == nil {
		return nil, false
	}
	var argv []string
	var ok bool
	if t == nil {
		argv, ok = TestFileArgv(root, path, explicit)
	} else {
		argv, ok = TestArgv(root, path, *t, explicit)
	}
	if !ok {
		return nil, false
	}
	return append(argv, spec.StructuredArgs...), true
}

// TestFailedArgv synthesizes the argv re-running exactly the tests named by
// ids (their RerunIDs) in path's scope, StructuredArgs included — the
// re-run-failed / re-run-single seam of the Test Results tool (#1911).
func TestFailedArgv(root, path string, ids []string, explicit string) ([]string, bool) {
	l, spec, ok := testSpecFor(path)
	if !ok || len(spec.FailedArgv) == 0 || len(ids) == 0 {
		return nil, false
	}
	cmd := testCommand(l.ID, root, spec, explicit)
	file := testFileArg(spec, root, path)
	var out []string
	for _, a := range spec.FailedArgv {
		if a == "{names}" && spec.NamesJoin == "" {
			out = append(out, ids...)
			continue
		}
		if a == "{interpreter}" {
			out = append(out, cmd...)
			continue
		}
		a = strings.ReplaceAll(a, "{names}", strings.Join(ids, spec.NamesJoin))
		a = strings.ReplaceAll(a, "{file}", file)
		out = append(out, a)
	}
	return append(out, spec.StructuredArgs...), true
}

// TestParser returns the language's test-output parser, or ok=false when the
// language declares none (raw output fallback).
func TestParser(langID string) (func(output string) []TestResult, bool) {
	l, ok := ByID(langID)
	if !ok || l.Test == nil || l.Test.ParseOutput == nil {
		return nil, false
	}
	return l.Test.ParseOutput, true
}

// TestRunsAtRoot reports whether path's language runs its tests with cwd =
// the project root rather than the test file's directory (#1926) — the flag
// run.TestConfig reads when it picks a test configuration's working
// directory.
func TestRunsAtRoot(path string) bool {
	_, spec, ok := testSpecFor(path)
	return ok && spec.RunAtRoot
}

// HasTests reports whether path's language declares test detection and path
// is a test file — the cheap pre-check before scanning buffer content.
func HasTests(path string) bool {
	_, _, ok := testSpecFor(path)
	return ok
}

// testTool resolves the binary substituted for {interpreter}: the toolchain
// resolution (explicit config beats detection — the same seam as run, LSP and
// the terminal shims), falling back to the spec's Tool name.
func testTool(langID, root string, spec *TestSpec, explicit string) string {
	if p, _ := Interpreter(langID, root, explicit); p != "" {
		return p
	}
	return spec.Tool
}

// testCommand resolves what a whole "{interpreter}" element expands to: the
// spec's Runner for root (#1926, PHP's vendor/bin/phpunit) when it yields
// one, the toolchain-resolved interpreter otherwise. Never empty — Tool is
// the last fallback.
func testCommand(langID, root string, spec *TestSpec, explicit string) []string {
	if spec.Runner != nil {
		if cmd := spec.Runner(root); len(cmd) > 0 {
			return cmd
		}
		if spec.Tool != "" {
			return []string{spec.Tool}
		}
	}
	return []string{testTool(langID, root, spec, explicit)}
}

// testFileArg is the {file} substitution: the file's path relative to root
// for a RunAtRoot language (#1926, the argv runs at the project root), its
// base name otherwise (#1911, pytest runs with cwd = the file's directory).
func testFileArg(spec *TestSpec, root, path string) string {
	if !spec.RunAtRoot {
		return filepath.Base(path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return filepath.ToSlash(rel)
}

// expandTestArgv substitutes the placeholders into a fresh slice. cmd expands
// the whole "{interpreter}" element — a runner may be a multi-element prefix
// — and file is the {file} substitution testFileArg resolved.
func expandTestArgv(tpl []string, name string, cmd []string, file string) []string {
	out := make([]string, 0, len(tpl))
	for _, a := range tpl {
		if a == "{interpreter}" {
			out = append(out, cmd...)
			continue
		}
		a = strings.ReplaceAll(a, "{name}", name)
		a = strings.ReplaceAll(a, "{file}", file)
		out = append(out, a)
	}
	return out
}
