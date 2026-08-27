package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/lang"
	"ike/internal/pane"
	"ike/internal/run"
	"ike/internal/testresults"
)

// testwatch_test.go covers watch mode of the Test Results pane (#2172): the
// toggle, the save trigger, the debounce, the in-flight queue and the scope
// resolution (directory-scoped language → the saved file's package;
// file-scoped language → fall back to the last run's scope).

func init() {
	// A second fake language whose file scope is the *file* (pytest-shaped):
	// saving one of its files must fall back to the last run's scope.
	lang.Register(lang.Language{
		ID:         "fk2172",
		Extensions: []string{"pk"},
		Test: &lang.TestSpec{
			FilePattern:    `_test\.pk$`,
			Pattern:        `^func (?P<name>Test\w*)\s*\(`,
			Kinds:          map[string][]string{"": {"{interpreter}", "one", "{file}", "{name}"}},
			FileArgv:       []string{"{interpreter}", "all", "{file}"},
			Tool:           "/bin/echo",
			StructuredArgs: []string{"structured"},
			ParseOutput: func(out string) []lang.TestResult {
				return []lang.TestResult{{Group: "g", Name: "TestP", Status: lang.TestPass, RerunID: "TestP"}}
			},
		},
	})
}

// watchApp opens the tests panel, arms watch mode and seeds a last run whose
// root is dir, so scope resolution has something to fall back to. The
// debounce is shortened so the timer command can simply be run.
func watchApp(t *testing.T, dir string, last run.Config) Model {
	t.Helper()
	m := testsApp(t)
	out, _ := m.Update(TestsToggleMsg{})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.TestsKey) {
		t.Fatal("tests panel must open")
	}
	m.testsPanel().SetWatch(true)
	m.lastTestRun = &testRunState{cfg: last, root: dir}
	m.testWatchWait = time.Millisecond
	return m
}

// fkPackage writes a directory holding one test file of the "fk1911"
// language plus one plain source file, and returns both paths.
func fkPackage(t *testing.T) (dir, testFile, src string) {
	t.Helper()
	dir = t.TempDir()
	testFile = filepath.Join(dir, "x_test.fk")
	if err := os.WriteFile(testFile, []byte("func TestGood(\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src = filepath.Join(dir, "impl.fk")
	if err := os.WriteFile(src, []byte("code\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, testFile, src
}

func TestWatchToggleShowsBadge(t *testing.T) {
	m := testsApp(t)
	out, _ := m.Update(TestsToggleMsg{})
	m = out.(Model)
	p := m.testsPanel()
	cmd := p.Update(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if cmd == nil {
		t.Fatal("'w' must report the toggle")
	}
	if msg, ok := cmd().(testresults.WatchMsg); !ok || !msg.On {
		t.Fatalf("'w' must emit WatchMsg{On:true}, got %#v", cmd())
	}
	if !p.Watching() {
		t.Fatal("'w' must arm watch mode")
	}
	if !strings.Contains(stripped(m), "WATCH") {
		t.Fatalf("the armed panel must show the badge:\n%s", stripped(m))
	}
	p.Update(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if p.Watching() {
		t.Fatal("'w' must disarm again")
	}
	if strings.Contains(stripped(m), "WATCH") {
		t.Fatal("the badge must vanish with the mode")
	}
}

func TestWatchSaveTriggersScopedRerun(t *testing.T) {
	dir, testFile, src := fkPackage(t)
	// The last run was a single test; saving a plain source file of the same
	// directory-scoped package must widen the re-run to the whole package.
	last, ok := run.TestConfig(dir, testFile, &lang.TestMatch{Name: "TestGood"})
	if !ok {
		t.Fatal("TestConfig must synthesize the seed run")
	}
	m := watchApp(t, dir, last)

	out, cmd := m.Update(testWatchSavedMsg{path: src})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("a save under an armed panel must arm the debounce timer")
	}
	out, cmd = m.Update(cmd()) // the debounce expiring
	m = out.(Model)
	if cmd == nil {
		t.Fatal("the expired debounce must start a captured run")
	}
	if got := m.lastTestRun.cfg.TestName; got != "" {
		t.Fatalf("the watch run must target the package, not %q", got)
	}
	if !m.testsPanel().Running() {
		t.Fatal("the watch run must show as running")
	}
	m = drain(t, m, cmd)
	if p := m.testsPanel(); p.Running() || p.Rows() == 0 {
		t.Fatal("the watch run must land in the panel")
	}
}

func TestWatchDebouncesBurstOfSaves(t *testing.T) {
	dir, testFile, src := fkPackage(t)
	last, _ := run.TestConfig(dir, testFile, nil)
	m := watchApp(t, dir, last)

	out, first := m.Update(testWatchSavedMsg{path: src})
	m = out.(Model)
	out, second := m.Update(testWatchSavedMsg{path: testFile})
	m = out.(Model)

	// The superseded timer fires into the void.
	out, cmd := m.Update(first())
	m = out.(Model)
	if cmd != nil {
		t.Fatal("a superseded debounce timer must not start a run")
	}
	out, cmd = m.Update(second())
	m = out.(Model)
	if cmd == nil {
		t.Fatal("the newest save's timer must start the single run")
	}
	if m.testRunSeq != 1 {
		t.Fatalf("the burst must produce exactly one run, got %d", m.testRunSeq)
	}
}

func TestWatchQueuesAtMostOneRerunWhileRunning(t *testing.T) {
	dir, testFile, src := fkPackage(t)
	last, _ := run.TestConfig(dir, testFile, nil)
	m := watchApp(t, dir, last)

	// Start a run and leave it in flight (its completion is not fed back).
	out, running := m.Update(testWatchSavedMsg{path: src})
	m = out.(Model)
	out, running = m.Update(running())
	m = out.(Model)
	if running == nil || !m.testsPanel().Running() {
		t.Fatal("the first watch run must be in flight")
	}
	seqBefore := m.testRunSeq

	// Two more saves land while it runs: both debounce timers resolve to a
	// queue write, and the queue holds one entry.
	for i := 0; i < 2; i++ {
		out, cmd := m.Update(testWatchSavedMsg{path: src})
		m = out.(Model)
		out, cmd = m.Update(cmd())
		m = out.(Model)
		if cmd != nil {
			t.Fatalf("save %d must queue, not start a second run", i)
		}
	}
	if m.testWatch.queued == nil {
		t.Fatal("a save during an in-flight run must queue a re-run")
	}
	if m.testRunSeq != seqBefore {
		t.Fatalf("no run may start while one is in flight (seq %d → %d)", seqBefore, m.testRunSeq)
	}

	// The in-flight run completing drains exactly one queued re-run.
	m = drain(t, m, running)
	if m.testWatch.queued != nil {
		t.Fatal("the queue must be empty after draining")
	}
	if m.testRunSeq != seqBefore+1 {
		t.Fatalf("draining must start exactly one re-run (seq %d → %d)", seqBefore, m.testRunSeq)
	}
}

func TestWatchFileScopedLanguageFallsBackToLastScope(t *testing.T) {
	dir, testFile, _ := fkPackage(t)
	last, _ := run.TestConfig(dir, testFile, nil)
	m := watchApp(t, dir, last)

	// A file-scoped language (pytest-shaped): saving a plain source file of
	// it says nothing about which test covers it, so the last scope re-runs.
	src := filepath.Join(dir, "impl.pk")
	if err := os.WriteFile(src, []byte("code\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, root := m.testWatchScope(src)
	if root != dir || cfg == nil || cfg.File != last.File {
		t.Fatalf("scope = %#v, want the last run's %q", cfg, last.File)
	}

	// Its own test file, however, resolves to itself.
	pkTest := filepath.Join(dir, "y_test.pk")
	if err := os.WriteFile(pkTest, []byte("func TestP(\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _ = m.testWatchScope(pkTest)
	if cfg == nil || filepath.Base(cfg.File) != "y_test.pk" {
		t.Fatalf("a saved test file must scope to itself, got %#v", cfg)
	}
}

func TestWatchScopeFallsBackOutsideRootAndWithoutTests(t *testing.T) {
	dir, testFile, _ := fkPackage(t)
	last, _ := run.TestConfig(dir, testFile, nil)
	m := watchApp(t, dir, last)

	if cfg, _ := m.testWatchScope(filepath.Join(t.TempDir(), "other.fk")); cfg.File != last.File {
		t.Fatalf("a save outside the run root must fall back, got %q", cfg.File)
	}
	// A directory-scoped language file in a package without test files has
	// no affected target either.
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	lonely := filepath.Join(sub, "lonely.fk")
	if err := os.WriteFile(lonely, []byte("code\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := m.testWatchScope(lonely); cfg.File != last.File {
		t.Fatalf("a package without tests must fall back, got %q", cfg.File)
	}
}

func TestWatchOffAndClosedPaneStopTriggering(t *testing.T) {
	dir, testFile, src := fkPackage(t)
	last, _ := run.TestConfig(dir, testFile, nil)
	m := watchApp(t, dir, last)

	// Disarming drops the pending scope and the queue.
	m.testWatch.queued = &last
	out, _ := m.Update(testresults.WatchMsg{On: false})
	m = out.(Model)
	if m.testWatch.queued != nil || m.testWatch.cfg != nil {
		t.Fatal("disarming must drop the pending and queued re-runs")
	}
	m.testsPanel().SetWatch(false)
	if _, cmd := m.Update(testWatchSavedMsg{path: src}); cmd != nil {
		t.Fatal("a save with watch off must do nothing")
	}

	// Closing the pane ends the mode with it — the state lives on the panel.
	m.testsPanel().SetWatch(true)
	m.activeWS().Panes.Close(pane.TestsKey)
	if _, cmd := m.Update(testWatchSavedMsg{path: src}); cmd != nil {
		t.Fatal("a save with the panel closed must do nothing")
	}
}

func TestTestDirScopedDiscriminatesLanguages(t *testing.T) {
	if !lang.TestDirScoped("/p/impl.fk") {
		t.Fatal("a FileArgv without {file} is directory-scoped")
	}
	if lang.TestDirScoped("/p/impl.pk") {
		t.Fatal("a FileArgv naming {file} is file-scoped")
	}
	if lang.TestDirScoped("/p/notes.unknownext") {
		t.Fatal("a language without a test spec is not directory-scoped")
	}
}
