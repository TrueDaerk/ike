package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testPkgDir is the working directory the test binary started in: the
// package directory, which every fixture that reads a relative path assumes.
var testPkgDir string

// A project switch changes the process working directory (switch.go), and a
// test that switches into a t.TempDir() root leaves the whole binary there
// once the directory is removed: every later terminal spawns in a directory
// that no longer exists, relative fixture paths fail to open, and which tests
// break depends on the run order (#2997). The guard runs before every model
// is built: a vanished working directory is never deliberate — a test that
// chdirs on purpose keeps the directory alive — so it is put back to the
// package directory, with a line naming the victim so the culprit (the test
// before it) is easy to find.
func init() {
	testPkgDir, _ = os.Getwd()
	newModelHook = func() {
		if _, err := os.Stat("."); err == nil {
			return
		}
		if err := os.Chdir(testPkgDir); err == nil {
			fmt.Fprintf(os.Stderr, "cwd guard: the working directory was gone, restored %s\n", testPkgDir)
		}
	}
}

// projectTempDir is t.TempDir for a directory a model is switched into. A
// switched-in root gets written off the loop after the test ends — the
// complexity index persists its cache into <root>/.ike when the walk lands —
// and t.TempDir's own removal then fails with "directory not empty" (#2997).
// The removal here retries for a moment instead.
func projectTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ike-project-")
	if err != nil {
		t.Fatal(err)
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	t.Cleanup(func() {
		for i := 0; i < 50; i++ {
			if err := os.RemoveAll(dir); err == nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Logf("could not remove %s", dir)
	})
	return dir
}

// withProjectWalks re-enables the project walks for one test: the ones Init
// starts (TODO scan, complexity index) and the completion indexes' scan of
// the real project root.
func withProjectWalks(t *testing.T) {
	t.Helper()
	initProjectWalks = true
	root := completionIndexRoot
	completionIndexRoot = func(root string) string { return root }
	t.Cleanup(func() {
		initProjectWalks = false
		completionIndexRoot = root
	})
}
