package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
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

// coverageStatusApp opens src in a wide app whose tests.coverage_status is
// set to on — the status-line segment's opt-in (#2246).
func coverageStatusApp(t *testing.T, src string, on bool) Model {
	t.Helper()
	orig := config.Get()
	t.Cleanup(func() { config.Set(orig) })
	c, _ := config.Load(config.Options{})
	c.Tests.CoverageStatus = on
	config.Set(c)
	m := coverageApp(t)
	// The temp path in the file segment is long; widen the bar so the
	// overflow guard cannot clip the segment under test.
	out, _ := m.Update(tea.WindowSizeMsg{Width: 400, Height: 30})
	m = out.(Model)
	out, _ = m.openPath(src, false)
	return out.(Model)
}

// TestCoverageStatusSegment: with the setting on, the focused file's
// percentage reaches the status line, an edit marks it stale, and an
// uncovered file shows nothing.
func TestCoverageStatusSegment(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "f.cvt")
	if err := os.WriteFile(src, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registerCovLang(t, src)
	m := coverageStatusApp(t, src, true)
	if line := m.statusLine(); strings.Contains(line, "cov ") {
		t.Fatalf("without a coverage run the segment must stay hidden: %q", line)
	}
	m.coverage.SetRun([]lang.FileCoverage{{Path: src, Lines: map[int]lang.CoverKind{
		1: lang.CoverCovered, 2: lang.CoverUncovered, 3: lang.CoverPartial,
	}}})
	if line := m.statusLine(); !strings.Contains(line, "cov 66.7%") {
		t.Fatalf("the focused file's percentage must show: %q", line)
	}
	out, _ := m.Update(editor.SyncMsg{Path: src})
	m = out.(Model)
	if line := m.statusLine(); !strings.Contains(line, "cov 66.7% stale") {
		t.Fatalf("an edit must mark the segment stale: %q", line)
	}

	other := filepath.Join(dir, "g.cvt")
	if err := os.WriteFile(other, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ = m.openPath(other, false)
	m = out.(Model)
	if line := m.statusLine(); strings.Contains(line, "cov ") {
		t.Fatalf("an uncovered file must hide the segment: %q", line)
	}
}

// TestCoverageStatusSegmentOptIn: the segment stays off while
// tests.coverage_status is off — the default.
func TestCoverageStatusSegmentOptIn(t *testing.T) {
	src := filepath.Join(t.TempDir(), "f.cvt")
	if err := os.WriteFile(src, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registerCovLang(t, src)
	m := coverageStatusApp(t, src, false)
	m.coverage.SetRun([]lang.FileCoverage{{Path: src, Lines: map[int]lang.CoverKind{1: lang.CoverCovered}}})
	if line := m.statusLine(); strings.Contains(line, "cov ") {
		t.Fatalf("the segment must be opt-in: %q", line)
	}
}

// TestCoverageFilesForPanel: the store's per-file summaries reach the Test
// Results panel worst-covered first.
func TestCoverageFilesForPanel(t *testing.T) {
	m := coverageApp(t)
	m.coverage.SetRun([]lang.FileCoverage{
		{Path: "/full.go", Lines: map[int]lang.CoverKind{1: lang.CoverCovered}},
		{Path: "/half.go", Lines: map[int]lang.CoverKind{1: lang.CoverCovered, 2: lang.CoverUncovered}},
	})
	files := m.coverageFiles()
	if len(files) != 2 || files[0].Path != "/half.go" || files[0].Percent != 50 || files[1].Percent != 100 {
		t.Fatalf("coverageFiles = %+v", files)
	}
}
