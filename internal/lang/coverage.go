package lang

// Coverage seam (#2081): a language plugin declares how a test run produces
// coverage data (TestSpec.CoverArgs) and how that data parses into a neutral
// per-file line model (TestSpec.ParseCover). The engine — run command
// synthesis, coverage store, editor gutter — only ever sees CoverKind and
// FileCoverage, so a non-Go plugin registers coverage support without engine
// changes.

// CoverKind classifies one line's coverage in a run.
type CoverKind int

const (
	// CoverCovered — every statement block touching the line executed.
	CoverCovered CoverKind = iota + 1
	// CoverUncovered — no statement block touching the line executed.
	CoverUncovered
	// CoverPartial — the line sits in both an executed and an unexecuted
	// block (Go: two statements sharing a line, one reached).
	CoverPartial
)

// FileCoverage is one file's per-line coverage of a run.
type FileCoverage struct {
	// Path is the file's absolute path — ParseCover resolves whatever the
	// raw coverage data records (import-qualified, relative) before handing
	// the model to the engine.
	Path string
	// Lines maps 1-based line numbers to their coverage; lines no statement
	// block touches (blanks, declarations, comments) are absent.
	Lines map[int]CoverKind
}

// TestCoverageArgv is TestStructuredArgv plus the language's coverage
// arguments writing the run's coverage data to profile (t nil = file scope).
// ok=false when the base argv does not synthesize or the language declares no
// coverage support.
func TestCoverageArgv(root, path string, t *TestMatch, explicit, profile string) ([]string, bool) {
	_, spec, specOK := testSpecFor(path)
	if !specOK || spec.CoverArgs == nil || spec.ParseCover == nil {
		return nil, false
	}
	argv, ok := TestStructuredArgv(root, path, t, explicit)
	if !ok {
		return nil, false
	}
	return append(argv, spec.CoverArgs(profile)...), true
}

// CoverParser returns the language's coverage-data parser, or ok=false when
// the language declares none.
func CoverParser(langID string) (func(profile, dir string) ([]FileCoverage, error), bool) {
	l, ok := ByID(langID)
	if !ok || l.Test == nil || l.Test.ParseCover == nil {
		return nil, false
	}
	return l.Test.ParseCover, true
}
