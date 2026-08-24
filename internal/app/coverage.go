package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/coverage"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/run"
)

// coverage.go wires run-with-coverage (#2081): run.testsWithCoverage runs the
// active file's test scope through the captured path with the language's
// coverage arguments, finishTestRun (testrun.go) parses the profile into the
// app's coverage store, and the editors of covered files render per-line
// gutter marks. coverage.toggle hides/shows the marks without dropping the
// data; a plain rerun or rerun-failed leaves the store untouched, so coverage
// of files those runs did not exercise survives.

// runTestsWithCoverage is the run.testsWithCoverage handler: runTestsInFile
// with coverage collection enabled.
func (m *Model) runTestsWithCoverage() tea.Cmd {
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, "run: focus a file tab first")
		return nil
	}
	if !lang.HasTests(ed.Path()) {
		m.host.Notify(host.Info, "run: no test runner for this file")
		return nil
	}
	root := projectRoot()
	cfg, ok := run.TestConfig(root, ed.Path(), nil)
	if !ok {
		m.host.Notify(host.Info, "run: no test runner for this file")
		return nil
	}
	store := run.Load()
	created := store.ByName(cfg.Name) == nil
	return m.launchCoverageRun(root, store, store.Upsert(cfg), created)
}

// toggleCoverageMarks is the coverage.toggle handler: flip the display state
// and push the change to every open editor of a covered file.
func (m *Model) toggleCoverageMarks() tea.Cmd {
	if m.coverage.Empty() {
		m.host.Notify(host.Info, "coverage: no data — run tests with coverage first")
		return nil
	}
	m.coverageShown = !m.coverageShown
	if m.coverageShown {
		m.host.Notify(host.Info, "coverage: marks shown")
	} else {
		m.host.Notify(host.Info, "coverage: marks hidden")
	}
	return m.pushCoverageMarks()
}

// coverageMarksCmd seeds a freshly opened buffer with its stored coverage
// marks (openPath), so a covered file opened after the run still shows them.
func (m Model) coverageMarksCmd(path string) tea.Cmd {
	if !m.coverageShown {
		return nil
	}
	marks, stale, ok := m.coverage.Marks(path)
	if !ok {
		return nil
	}
	return func() tea.Msg { return coverage.MarksMsg{Path: path, Marks: marks, Stale: stale} }
}
