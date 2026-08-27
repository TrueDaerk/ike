package app

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/run"
)

// testwatch.go is watch mode of the Test Results tool window (#2172): with
// the panel's 'w' toggle armed, every buffer save re-runs the affected tests
// through the ordinary captured-run pipeline (testrun.go). Three rules keep
// it calm — a burst of saves collapses into one run (debounce), a run never
// starts while one is in flight (at most one re-run is queued behind it), and
// the whole thing is per pane: the flag lives on testresults.Model, so
// closing the panel ends the mode and nothing is persisted.

// testWatchDebounce is how long a save waits for the next one before the
// re-run fires. Long enough to swallow a Save All or a formatter's rewrite,
// short enough to feel immediate.
const testWatchDebounce = 400 * time.Millisecond

// testWatchSavedMsg reports one buffer save to watch mode. The editor emitter
// sends it from a goroutine, like the other save-fanout messages.
type testWatchSavedMsg struct{ path string }

// testWatchFireMsg is the debounce timer expiring. Seq drops the timers of
// superseded saves: only the newest save's timer matches.
type testWatchFireMsg struct{ seq int }

// testWatchState is the driver's bookkeeping. seq is the debounce generation,
// cfg the scope the pending timer will run, and queued the one re-run held
// back because a run was in flight (nil = nothing queued; at most one, by
// construction — a second save while queued just overwrites the scope).
type testWatchState struct {
	seq    int
	cfg    *run.Config
	root   string
	queued *run.Config
}

// testWatchSaved handles a save: resolve the affected scope and (re-)arm the
// debounce timer. A closed or unarmed panel, or a save before the first test
// run, is a no-op — watch mode has no scope to fall back to then.
func (m *Model) testWatchSaved(path string) tea.Cmd {
	p := m.testsPanel()
	if p == nil || !p.Watching() || m.lastTestRun == nil || path == "" {
		return nil
	}
	cfg, root := m.testWatchScope(path)
	if cfg == nil {
		return nil
	}
	m.testWatch.seq++
	m.testWatch.cfg, m.testWatch.root = cfg, root
	seq := m.testWatch.seq
	return tea.Tick(m.testWatchDelay(), func(time.Time) tea.Msg {
		return testWatchFireMsg{seq: seq}
	})
}

// testWatchDelay is the debounce interval, injectable so tests need no sleep.
func (m *Model) testWatchDelay() time.Duration {
	if m.testWatchWait > 0 {
		return m.testWatchWait
	}
	return testWatchDebounce
}

// testWatchFire runs the debounced re-run, unless a newer save superseded it.
// While a run is in flight the scope is queued instead — one slot, so a burst
// of saves during a long run produces exactly one follow-up run.
func (m *Model) testWatchFire(msg testWatchFireMsg) tea.Cmd {
	if msg.seq != m.testWatch.seq || m.testWatch.cfg == nil {
		return nil
	}
	p := m.testsPanel()
	if p == nil || !p.Watching() {
		return nil
	}
	cfg, root := m.testWatch.cfg, m.testWatch.root
	m.testWatch.cfg = nil
	if p.Running() {
		m.testWatch.queued = cfg
		m.testWatch.root = root
		return nil
	}
	return m.startWatchRun(*cfg, root)
}

// testWatchDrain starts the queued re-run, if any — called when a captured
// run finishes. A panel closed or disarmed meanwhile drops the queue.
func (m *Model) testWatchDrain() tea.Cmd {
	cfg := m.testWatch.queued
	if cfg == nil {
		return nil
	}
	m.testWatch.queued = nil
	p := m.testsPanel()
	if p == nil || !p.Watching() {
		return nil
	}
	return m.startWatchRun(*cfg, m.testWatch.root)
}

// startWatchRun launches cfg through the captured-run path and adopts it as
// the last test run, so the panel's r/f/t act on the watched scope. Unlike
// launchTestRun it neither touches the run store nor announces the command —
// a watch re-run is background noise, not a user-started run.
func (m *Model) startWatchRun(cfg run.Config, root string) tea.Cmd {
	argv, ok := run.StructuredArgv(root, cfg, m.explicitInterpreter(cfg.Lang))
	if !ok {
		return nil
	}
	m.lastTestRun = &testRunState{cfg: cfg, root: root}
	return m.startCapturedRun(&cfg, argv, cfg.Dir(root))
}

// testWatchScope resolves the saved file to the test target to re-run:
//
//   - the file itself, when it is a test file of a language with a parser;
//   - its directory's test files, when the language's file scope *is* the
//     directory (lang.TestDirScoped — Go's package);
//   - otherwise nil-signalled fallback to the last run's own scope, which is
//     what a language like Python gets: saving foo.py says nothing about
//     which test file covers it.
//
// The fallback also catches saves outside the last run's project root.
func (m *Model) testWatchScope(path string) (*run.Config, string) {
	st := m.lastTestRun
	root := st.root
	fallback := func() (*run.Config, string) {
		cfg := st.cfg
		return &cfg, root
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, abs)
	}
	if rel, err := filepath.Rel(root, abs); err != nil || strings.HasPrefix(rel, "..") {
		return fallback()
	}
	target := ""
	switch {
	case lang.HasTests(abs):
		target = abs
	case lang.TestDirScoped(abs):
		target = firstTestFile(filepath.Dir(abs))
	}
	if target == "" {
		return fallback()
	}
	cfg, ok := run.TestConfig(root, target, nil)
	if !ok {
		return fallback()
	}
	if _, ok := run.StructuredArgv(root, cfg, m.explicitInterpreter(cfg.Lang)); !ok {
		return fallback()
	}
	return &cfg, root
}

// firstTestFile names dir's alphabetically first test file, or "" when the
// directory holds none — a package without tests has no affected target and
// falls back to the last scope.
func firstTestFile(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && lang.HasTests(filepath.Join(dir, e.Name())) {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return filepath.Join(dir, names[0])
}

// testWatchToggled confirms the panel's 'w' toggle with a toast; the flag
// itself already lives on the panel. Disarming also drops a queued re-run so
// a run started later never fires behind the user's back.
func (m *Model) testWatchToggled(on bool) {
	if !on {
		m.testWatch.cfg, m.testWatch.queued = nil, nil
		m.testWatch.seq++
		m.host.Notify(host.Info, "tests: watch off")
		return
	}
	if m.lastTestRun == nil {
		m.host.Notify(host.Info, "tests: watch on — run tests once to give it a scope")
		return
	}
	m.host.Notify(host.Info, "tests: watch on — saving re-runs the affected tests")
}
