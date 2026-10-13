package complexity

import (
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"ike/internal/lang"
	"ike/internal/pathglob"
)

// testfile.go is the "is this test code" predicate (#2950) behind the
// Complexity window's hide-tests toggle, its test: filter key and the CLI's
// --no-tests: a file is a test file when its language's test-runner seam
// (lang.TestSpec.FilePattern) claims the base name, else when the
// project-relative path matches a built-in fallback for the languages
// without one (JavaScript / TypeScript specs, shell suites, test trees).

// testFileGlobs match the base name of a test file in a language without a
// FilePattern.
var testFileGlobs = []string{"*.spec.*", "*.test.*", "*_test.sh", "*.bats"}

// testDirs are the directory names whose whole subtree is test code, at any
// depth of the project-relative path (src/__tests__/foo.ts counts).
var testDirs = map[string]bool{"__tests__": true, "tests": true, "test": true, "spec": true}

// testPatterns caches the compiled FilePattern regexps.
var testPatterns sync.Map // string → *regexp.Regexp (nil on a bad pattern)

// IsTestFile reports whether path holds test code. root makes the fallback
// directory rule read the project-relative path, so a project that itself
// lives below a "test" directory does not count as test code throughout.
func IsTestFile(root, path string) bool {
	base := filepath.Base(path)
	if l, ok := lang.ByPath(path); ok && l.Test != nil && l.Test.FilePattern != "" {
		if re := testPattern(l.Test.FilePattern); re != nil && re.MatchString(base) {
			return true
		}
	}
	for _, g := range testFileGlobs {
		if pathglob.Match(g, base) {
			return true
		}
	}
	rel := path
	if root != "" {
		if r, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
	}
	dirs := strings.Split(filepath.ToSlash(rel), "/")
	for _, d := range dirs[:len(dirs)-1] {
		if testDirs[d] {
			return true
		}
	}
	return false
}

// testPattern compiles a FilePattern once.
func testPattern(p string) *regexp.Regexp {
	if v, ok := testPatterns.Load(p); ok {
		return v.(*regexp.Regexp)
	}
	re, err := regexp.Compile(p)
	if err != nil {
		re = nil
	}
	testPatterns.Store(p, re)
	return re
}
