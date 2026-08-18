package langphp

// test.go wires PHPUnit into the test-runner seam (#1926): which binary runs
// the tests of a project root, and whether the project looks like a PHPUnit
// project at all. The TestSpec itself lives in php.go, the output parser in
// testoutput.go.

import (
	"os"
	"os/exec"
	"path/filepath"

	"ike/internal/lang"
)

// phpunitLook is a seam for tests: the PATH lookup of a global phpunit.
var phpunitLook = exec.LookPath

// vendorRunners are the project-local PHPUnit binaries, in preference order.
// simple-phpunit is the Symfony bridge's wrapper, common in Symfony projects.
var vendorRunners = []string{"vendor/bin/phpunit", "vendor/bin/simple-phpunit"}

// phpunitConfigs are the configuration file names PHPUnit itself looks for in
// the project root.
var phpunitConfigs = []string{"phpunit.xml", "phpunit.xml.dist", "phpunit.dist.xml"}

// phpunitRunner is the lang.TestSpec.Runner hook: the command prefix running
// root's PHPUnit. A project-local vendor/bin binary wins — it is the version
// composer.lock pins — and a global phpunit on PATH stands in for a project
// that is recognizably PHPUnit (a configuration file in the root) but vendors
// nothing. Nil hands the seam back to the spec's Tool name, so an
// unrecognized project keeps a readable "phpunit …" argv rather than an
// unrelated absolute path.
//
// The argv runs with cwd = the project root (TestSpec.RunAtRoot), so PHPUnit
// discovers phpunit.xml / phpunit.xml.dist and the composer autoloader on its
// own — no --configuration plumbing needed.
func phpunitRunner(root string) []string {
	if p := vendorRunner(root); p != "" {
		return []string{p}
	}
	if !isPHPUnitProject(root) {
		return nil
	}
	if p, err := phpunitLook("phpunit"); err == nil {
		return []string{p}
	}
	return nil
}

// vendorRunner is root's project-local PHPUnit binary, "" when it vendors none.
func vendorRunner(root string) string {
	for _, rel := range vendorRunners {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// isPHPUnitProject reports whether root looks like a PHPUnit project: a
// phpunit configuration file in the root, or a project-local runner binary.
// The TestSpec's file detection (`*Test.php`) stays independent of it — a
// test file is recognizable on its own, and a fresh project should still show
// its run markers before `composer install`.
func isPHPUnitProject(root string) bool {
	for _, name := range phpunitConfigs {
		if st, err := os.Stat(filepath.Join(root, name)); err == nil && !st.IsDir() {
			return true
		}
	}
	return vendorRunner(root) != ""
}

// phpunitSpec is the PHP entry of the test-runner seam (#1150/#1911/#1926).
//
// Detection: PHPUnit's own convention — a `*Test.php` file (the `tests/`
// layout follows from it) holding `public function testX()` methods. The
// `#[Test]`/`@test` forms carry no name prefix and are left to the file-scope
// run, which covers them.
//
// Commands: the file scope targets the file itself; a single test filters by
// an anchored `Class::method` regexp, whose trailing `( |$)` keeps the data
// sets of a data-provider test (`testAdd with data set #0`) in while shutting
// out same-prefix siblings (`testAddNegative`). Re-run-failed alternates the
// failed method names into that one filter.
//
// Structured output: `--teamcity` streams one `##teamcity[...]` service
// message per test to stdout — the only machine-readable PHPUnit surface that
// needs no temp file, so it fits the captured-stdout model the Go and Python
// parsers use (`--log-junit` would write a file the capture never sees). It
// is stable across PHPUnit 9, 10 and 11. PHPUnit's option parser accepts
// options after the positional file argument, so appending the flag last (the
// seam's StructuredArgs contract) is fine.
var phpunitSpec = &lang.TestSpec{
	FilePattern: `Test\.php$`,
	Pattern:     `^\s*(?:(?:public|final|static)\s+)*function\s+(?P<name>test[A-Za-z0-9_]*)\s*\(`,
	Kinds: map[string][]string{
		"": {"{interpreter}", "--filter", `/::{name}( |$)/`, "{file}"},
	},
	FileArgv:       []string{"{interpreter}", "{file}"},
	Tool:           "phpunit",
	Runner:         phpunitRunner,
	RunAtRoot:      true,
	StructuredArgs: []string{"--teamcity"},
	ParseOutput:    parsePHPUnit,
	FailedArgv:     []string{"{interpreter}", "--filter", `/::({names})( |$)/`, "{file}"},
	NamesJoin:      "|",
}
