package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/coverage"
	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/registry"
	"ike/internal/run"
)

// coverage_test.go covers the app-side coverage wiring (#2081): profile
// consumption through the language seam into the store, mark seeding for
// opened files, the display toggle, and edit-driven store staleness.

func coverageApp(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return out.(Model)
}

// registerCovLang installs a fake language whose ParseCover returns fixed
// marks for path — the seam-neutrality proof doubling as a fixture.
func registerCovLang(t *testing.T, path string) {
	t.Helper()
	lang.Register(lang.Language{ID: "covapp", Extensions: []string{"cvt"}, Test: &lang.TestSpec{
		FilePattern: `_test\.cvt$`,
		Pattern:     `^func (?P<name>Test\w+)`,
		Kinds:       map[string][]string{"": {"covtool", "test"}},
		FileArgv:    []string{"covtool", "test"},
		Tool:        "covtool",
		ParseOutput: func(string) []lang.TestResult { return nil },
		CoverArgs:   func(profile string) []string { return []string{"--cover", profile} },
		ParseCover: func(profile, dir string) ([]lang.FileCoverage, error) {
			return []lang.FileCoverage{{Path: path, Lines: map[int]lang.CoverKind{1: lang.CoverCovered, 2: lang.CoverUncovered}}}, nil
		},
	}})
}

// TestFinishTestRunConsumesCoverage: a coverage run's completion fills the
// store from the profile, deletes the temp file and pushes marks.
func TestFinishTestRunConsumesCoverage(t *testing.T) {
	m := coverageApp(t)
	src := filepath.Join(t.TempDir(), "f.cvt")
	registerCovLang(t, src)
	profile := filepath.Join(t.TempDir(), "cover.out")
	if err := os.WriteFile(profile, []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.testRunSeq = 1
	m.lastTestRun = &testRunState{cfg: run.Config{Lang: "covapp", Tests: true}, root: t.TempDir(), coverProfile: profile}
	cmd := m.finishTestRun(TestRunDoneMsg{Seq: 1, Output: ""})
	if m.coverage.Empty() {
		t.Fatal("the store must hold the parsed run")
	}
	if marks, stale, ok := m.coverage.Marks(src); !ok || stale || marks[0] != lang.CoverCovered || marks[1] != lang.CoverUncovered {
		t.Fatalf("marks = %v stale=%v ok=%v", marks, stale, ok)
	}
	if !m.coverageShown {
		t.Fatal("a fresh coverage run must show its marks")
	}
	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Fatal("the profile temp file must be removed")
	}
	_ = cmd // no editor of src is open; the push may be empty
	if m.lastTestRun.coverProfile != "" {
		t.Fatal("the profile must be consumed — a plain re-run must not re-parse it")
	}
}

// TestCoverageMarksCmdSeedsOpenedFiles: a covered file opened after the run
// gets its stored marks; hidden display or unknown files yield nothing.
func TestCoverageMarksCmdSeedsOpenedFiles(t *testing.T) {
	m := coverageApp(t)
	m.coverage.SetRun([]lang.FileCoverage{{Path: "/cov.go", Lines: map[int]lang.CoverKind{1: lang.CoverCovered}}})
	cmd := m.coverageMarksCmd("/cov.go")
	if cmd == nil {
		t.Fatal("a covered file must seed marks")
	}
	msg, ok := cmd().(coverage.MarksMsg)
	if !ok || msg.Path != "/cov.go" || msg.Marks[0] != lang.CoverCovered {
		t.Fatalf("msg = %+v", msg)
	}
	if m.coverageMarksCmd("/other.go") != nil {
		t.Fatal("an uncovered file must not seed marks")
	}
	m.coverageShown = false
	if m.coverageMarksCmd("/cov.go") != nil {
		t.Fatal("a hidden display must not seed marks")
	}
}

// TestToggleCoverageMarks: the toggle flips the display state; without data
// it only explains itself.
func TestToggleCoverageMarks(t *testing.T) {
	m := coverageApp(t)
	if m.toggleCoverageMarks(); !m.coverageShown {
		t.Fatal("an empty store must leave the display state untouched")
	}
	m.coverage.SetRun([]lang.FileCoverage{{Path: "/cov.go", Lines: map[int]lang.CoverKind{1: lang.CoverCovered}}})
	m.toggleCoverageMarks()
	if m.coverageShown {
		t.Fatal("the first toggle must hide")
	}
	m.toggleCoverageMarks()
	if !m.coverageShown {
		t.Fatal("the second toggle must show again")
	}
}

// TestEditMarksStoreStale: a buffer change (editor.SyncMsg) flags the file's
// stored coverage stale for later opens.
func TestEditMarksStoreStale(t *testing.T) {
	m := coverageApp(t)
	m.coverage.SetRun([]lang.FileCoverage{{Path: "/cov.go", Lines: map[int]lang.CoverKind{1: lang.CoverCovered}}})
	out, _ := m.Update(editor.SyncMsg{Path: "/cov.go"})
	m = out.(Model)
	if _, stale, ok := m.coverage.Marks("/cov.go"); !ok || !stale {
		t.Fatal("an edit must mark the stored coverage stale")
	}
}
