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
	// argv template running exactly one test. Elements may contain the
	// placeholders {interpreter} (the resolved toolchain binary, Tool as
	// fallback) and {name} (the test's name, verbatim — Pattern-captured
	// names are identifiers, safe inside a `-run ^…$` anchor).
	Kinds map[string][]string
	// FileArgv is the argv template running every test in the file's scope
	// (Go: the package — the argv runs with cwd = the file's directory).
	FileArgv []string
	// Tool is the fallback binary name when no interpreter resolves.
	Tool string
	// Exclude lists detected names that are never runnable tests (Go's
	// TestMain).
	Exclude []string
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
	return expandTestArgv(tpl, t.Name, testTool(l.ID, root, spec, explicit)), true
}

// TestFileArgv synthesizes the argv running every test in path's scope (the
// file's package directory — run it with cwd = filepath.Dir(path)).
func TestFileArgv(root, path, explicit string) ([]string, bool) {
	l, spec, ok := testSpecFor(path)
	if !ok || len(spec.FileArgv) == 0 {
		return nil, false
	}
	return expandTestArgv(spec.FileArgv, "", testTool(l.ID, root, spec, explicit)), true
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

// expandTestArgv substitutes the placeholders into a fresh slice.
func expandTestArgv(tpl []string, name, tool string) []string {
	out := make([]string, len(tpl))
	for i, a := range tpl {
		a = strings.ReplaceAll(a, "{interpreter}", tool)
		a = strings.ReplaceAll(a, "{name}", name)
		out[i] = a
	}
	return out
}
