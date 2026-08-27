package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/coverage"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/run"
	"ike/internal/terminal"
	"ike/internal/testresults"
)

// testrun.go captures test runs for the Test Results tool window (#1911).
// A test-scope configuration whose language declares an output parser
// (lang.TestSpec.ParseOutput) runs off-loop with captured combined output —
// argv extended by the language's StructuredArgs (`go test -json`, pytest
// `-v`) — and the parsed results fill the singleton tests pane. Languages
// without a parser, and users who turn tests.results_window off, keep the raw
// Run tool terminal exactly as before (#1905).

// TestRunDoneMsg reports a captured test run's completion. Seq guards
// against a stale result overwriting a newer run's.
type TestRunDoneMsg struct {
	Seq    int
	Output string
}

// testRunState remembers the last captured run for the re-run actions.
// coverProfile is the temp file a coverage run (#2081) writes its coverage
// data to; "" for a plain run, and consumed (reset) once parsed so a plain
// re-run never re-reads a stale profile.
type testRunState struct {
	cfg          run.Config
	root         string
	coverProfile string
}

// launchTestRun runs cfg captured when the structured path applies, reporting
// whether it took the run (false falls back to the run terminal). The store
// is touched and saved exactly like the terminal path, so run.rerun repeats
// the test run — through this same capture.
func (m *Model) launchTestRun(root string, store run.Store, cfg *run.Config, created bool) (tea.Cmd, bool) {
	if !cfg.Tests || !config.Get().Tests.ResultsWindow {
		return nil, false
	}
	argv, ok := run.StructuredArgv(root, *cfg, m.explicitInterpreter(cfg.Lang))
	if !ok {
		return nil, false
	}
	store.Touch(cfg.Name, run.KindRun)
	if err := run.Save(store); err != nil {
		m.host.Notify(host.Warn, "run: config not saved: "+err.Error())
	}
	m.lastTestRun = &testRunState{cfg: *cfg, root: root}
	cmd := m.startCapturedRun(cfg, argv, cfg.Dir(root))
	m.notifyRun(cfg, created, argv)
	return cmd, true
}

// launchCoverageRun is launchTestRun for a run-with-coverage (#2081): the
// structured argv is extended by the language's coverage arguments writing a
// profile to a temp file, remembered on testRunState so finishTestRun parses
// it into the coverage store. Coverage needs the captured-run path — with
// tests.results_window off or a language without coverage support the run is
// refused with a notice rather than silently degrading to a plain run.
func (m *Model) launchCoverageRun(root string, store run.Store, cfg *run.Config, created bool) tea.Cmd {
	if !cfg.Tests || !config.Get().Tests.ResultsWindow {
		m.host.Notify(host.Info, "coverage: requires the Test Results window (tests.results_window)")
		return nil
	}
	profile, err := os.CreateTemp("", "ike-cover-*.out")
	if err != nil {
		m.host.Notify(host.Error, "coverage: "+err.Error())
		return nil
	}
	profile.Close()
	argv, ok := run.CoverageArgv(root, *cfg, m.explicitInterpreter(cfg.Lang), profile.Name())
	if !ok {
		os.Remove(profile.Name())
		m.host.Notify(host.Info, "coverage: "+cfg.Lang+" declares no coverage support")
		return nil
	}
	store.Touch(cfg.Name, run.KindRun)
	if err := run.Save(store); err != nil {
		m.host.Notify(host.Warn, "run: config not saved: "+err.Error())
	}
	m.lastTestRun = &testRunState{cfg: *cfg, root: root, coverProfile: profile.Name()}
	cmd := m.startCapturedRun(cfg, argv, cfg.Dir(root))
	m.notifyRun(cfg, created, argv)
	return cmd
}

// rerunTests handles the panel's re-run actions: every test (zero msg), the
// previously failed set, or a single test by RerunID.
func (m *Model) rerunTests(msg testresults.RerunMsg) tea.Cmd {
	st := m.lastTestRun
	if st == nil {
		m.host.Notify(host.Info, "tests: nothing to re-run yet")
		return nil
	}
	if !msg.Failed && msg.ID == "" {
		cmd, ok := m.launchTestRun(st.root, run.Load(), &st.cfg, false)
		if !ok {
			m.host.Notify(host.Info, "tests: re-run unavailable")
		}
		return cmd
	}
	ids := []string{msg.ID}
	if msg.Failed {
		p := m.testsPanel()
		if p == nil {
			return nil
		}
		if ids = p.FailedIDs(); len(ids) == 0 {
			m.host.Notify(host.Info, "tests: nothing failed in the last run")
			return nil
		}
	}
	argv, ok := run.FailedArgv(st.root, st.cfg, ids, m.explicitInterpreter(st.cfg.Lang))
	if !ok {
		m.host.Notify(host.Info, "tests: "+st.cfg.Lang+" cannot re-run by name")
		return nil
	}
	cmd := m.startCapturedRun(&st.cfg, argv, st.cfg.Dir(st.root))
	m.host.Notify(host.Info, "tests: "+strings.Join(argv, " "))
	return cmd
}

// startCapturedRun opens/updates the tests pane and returns the off-loop
// command running argv in dir with captured combined output.
func (m *Model) startCapturedRun(cfg *run.Config, argv []string, dir string) tea.Cmd {
	m.testRunSeq++
	seq := m.testRunSeq
	if m.testsPanel() == nil && config.Get().Tests.AutoOpen {
		m.testsReturnFocus = m.activeWS().Panes.Focused()
		m.openTestsPanel()
		// A run started from the editor keeps the editor focused — the pane
		// is a progress display until the user reaches for it.
		if m.testsReturnFocus != "" && m.activeWS().Panes.Has(m.testsReturnFocus) {
			m.setFocus(m.testsReturnFocus)
		}
	}
	if p := m.testsPanel(); p != nil {
		p.StartRun(cfg.Name, dir)
	}
	env := terminal.MergeEnv(terminalEnv(), cfg.EnvSlice())
	return func() tea.Msg {
		c := exec.Command(argv[0], argv[1:]...)
		c.Dir = dir
		c.Env = append(os.Environ(), env...)
		out, _ := c.CombinedOutput() // a non-zero exit is a failed test, not an error
		return TestRunDoneMsg{Seq: seq, Output: string(out)}
	}
}

// finishTestRun parses a completed captured run and fills the pane; stale
// completions (an older run finishing after a newer one started) are dropped.
// A coverage run (#2081) additionally parses its profile into the coverage
// store, stamps the run-wide percentage onto the panel and pushes gutter
// marks to every open editor of a covered file — the returned commands.
func (m *Model) finishTestRun(msg TestRunDoneMsg) tea.Cmd {
	if msg.Seq != m.testRunSeq || m.lastTestRun == nil {
		return nil
	}
	var results []lang.TestResult
	if parse, ok := lang.TestParser(m.lastTestRun.cfg.Lang); ok {
		results = parse(msg.Output)
	}
	if p := m.testsPanel(); p != nil {
		p.FinishRun(results, msg.Output)
	}
	return m.finishCoverage()
}

// finishCoverage consumes the last run's coverage profile, if any: parse
// through the language seam, replace the store, surface the percentage and
// fan the marks out to open editors. The profile temp file is removed either
// way — a failed parse must not accumulate temp files.
func (m *Model) finishCoverage() tea.Cmd {
	st := m.lastTestRun
	if st.coverProfile == "" {
		return nil
	}
	profile := st.coverProfile
	st.coverProfile = ""
	defer os.Remove(profile)
	parse, ok := lang.CoverParser(st.cfg.Lang)
	if !ok {
		return nil
	}
	files, err := parse(profile, st.cfg.Dir(st.root))
	if err != nil {
		m.host.Notify(host.Warn, "coverage: "+err.Error())
		return nil
	}
	m.coverage.SetRun(files)
	m.coverageShown = true
	if p := m.testsPanel(); p != nil {
		if pct, ok := m.coverage.Percent(); ok {
			p.SetCoverage(pct)
		}
	}
	return m.pushCoverageMarks()
}

// pushCoverageMarks sends every covered file's current marks (or a clear when
// the display is hidden) to its open editor views.
func (m *Model) pushCoverageMarks() tea.Cmd {
	var cmds []tea.Cmd
	for _, path := range m.coverage.Paths() {
		msg := coverage.MarksMsg{Path: path}
		if m.coverageShown {
			msg.Marks, msg.Stale, _ = m.coverage.Marks(path)
		}
		if cmd := m.routeToEditor(path, msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// locateTest jumps to the source declaration of the test named by id: the
// last run's test file is scanned through the language's TestSpec pattern —
// the same detection the gutter markers use — and, for a multi-file scope
// (a Go package), its sibling test files too.
func (m *Model) locateTest(id string) (tea.Model, tea.Cmd) {
	st := m.lastTestRun
	if st == nil {
		return m, nil
	}
	file := st.cfg.File
	if !filepath.IsAbs(file) {
		file = filepath.Join(st.root, file)
	}
	dir := filepath.Dir(file)
	candidates := []string{file}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if p := filepath.Join(dir, e.Name()); !e.IsDir() && p != file {
				candidates = append(candidates, p)
			}
		}
	}
	for _, path := range candidates {
		if !lang.HasTests(path) {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, t := range lang.TestsInFile(path, strings.Split(string(data), "\n")) {
			if t.Name == id {
				return m.openPathAt(path, t.Line, 0)
			}
		}
	}
	m.host.Notify(host.Info, "tests: source of "+id+" not found")
	return m, nil
}
