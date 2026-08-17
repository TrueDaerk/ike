package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/pane"
	"ike/internal/registry"
	"ike/internal/testresults"
)

// tests_panel_test.go covers the Test Results tool window wiring (#1911):
// the toggle lifecycle, layout persistence, and the captured-run pipeline
// (structured argv → off-loop exec → parse → panel fill → re-run failed)
// through a synthetic language whose "test tool" is /bin/echo.

func init() {
	lang.Register(lang.Language{
		ID:         "fk1911",
		Extensions: []string{"fk"},
		Test: &lang.TestSpec{
			FilePattern: `_test\.fk$`,
			Pattern:     `^func (?P<name>Test\w*)\s*\(`,
			Kinds:       map[string][]string{"": {"{interpreter}", "one", "{name}"}},
			FileArgv:    []string{"{interpreter}", "all"},
			Tool:        "/bin/echo",
			// The parser reads /bin/echo's own argv back out of its output.
			StructuredArgs: []string{"structured"},
			ParseOutput: func(out string) []lang.TestResult {
				if !strings.Contains(out, "structured") {
					return nil
				}
				if strings.Contains(out, "rerun") {
					return []lang.TestResult{
						{Group: "g", Name: "TestBad", Status: lang.TestPass, RerunID: "TestBad"},
					}
				}
				return []lang.TestResult{
					{Group: "g", Name: "TestGood", Status: lang.TestPass, RerunID: "TestGood"},
					{Group: "g", Name: "TestBad", Status: lang.TestFail, File: "x_test.fk", Line: 2, RerunID: "TestBad"},
				}
			},
			FailedArgv: []string{"{interpreter}", "rerun", "{names}"},
			NamesJoin:  "|",
		},
	})
}

func testsApp(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return out.(Model)
}

func TestTestsToggleLifecycle(t *testing.T) {
	m := testsApp(t)
	before := m.activeWS().Panes.Focused()

	out, _ := m.Update(TestsToggleMsg{})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.TestsKey) || m.activeWS().Panes.Focused() != pane.TestsKey {
		t.Fatalf("first toggle must open + focus the panel (focus=%q)", m.activeWS().Panes.Focused())
	}
	out, _ = m.Update(TestsToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != before {
		t.Fatalf("focus = %q, want %q", m.activeWS().Panes.Focused(), before)
	}
	out, _ = m.Update(TestsToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != pane.TestsKey {
		t.Fatal("third toggle must re-focus the panel")
	}
}

func TestTestsPanePersists(t *testing.T) {
	m := testsApp(t)
	out, _ := m.Update(TestsToggleMsg{})
	m = out.(Model)
	_, ids, ok := loadLayout()
	if !ok {
		t.Fatal("layout must have been saved")
	}
	if ids[pane.TestsKey].Kind != "tests" {
		t.Fatalf("identity = %+v", ids[pane.TestsKey])
	}
	// A fresh model over the same config dir restores the pane (empty).
	m2 := NewWith(registry.New(), host.MapConfig{})
	out2, _ := m2.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 = out2.(Model)
	if !m2.activeWS().Panes.Has(pane.TestsKey) {
		t.Fatal("restored layout must keep the tests pane leaf")
	}
	_ = out
}

// runFakeTests drives one captured run of the fake language's file scope end
// to end and returns the updated model.
func runFakeTests(t *testing.T, m Model) Model {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "x_test.fk")
	if err := os.WriteFile(file, []byte("func TestGood(\nfunc TestBad(\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := m.runTest(file, nil)
	if cmd == nil {
		t.Fatal("a parser-backed test run must return the captured-run command")
	}
	// The run went to the panel, not the run terminal.
	if p := m.testsPanel(); p == nil || !p.Running() {
		t.Fatal("the tests panel must be open and running")
	}
	out, _ := m.Update(cmd()) // cmd runs /bin/echo synchronously
	return out.(Model)
}

func TestCapturedRunFillsPanel(t *testing.T) {
	m := testsApp(t)
	m = runFakeTests(t, m)
	p := m.testsPanel()
	if p == nil || p.Running() {
		t.Fatal("the finished run must land in the panel")
	}
	if p.Rows() != 3 { // group + two tests
		t.Fatalf("rows = %d, want 3", p.Rows())
	}
	if got := p.FailedIDs(); len(got) != 1 || got[0] != "TestBad" {
		t.Fatalf("failed ids = %v", got)
	}
	if !strings.Contains(stripped(m), "1 passed") || !strings.Contains(stripped(m), "1 failed") {
		t.Fatalf("summary must render:\n%s", stripped(m))
	}
}

// drain runs cmd (unpacking batches) and feeds every produced message back
// into the model.
func drain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = drain(t, m, c)
		}
		return m
	}
	if msg == nil {
		return m
	}
	out, next := m.Update(msg)
	return drain(t, out.(Model), next)
}

func TestRerunFailedRunsOnlyFailed(t *testing.T) {
	m := testsApp(t)
	m = runFakeTests(t, m)
	out, cmd := m.Update(testresults.RerunMsg{Failed: true})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("re-run failed must start a captured run")
	}
	m = drain(t, m, cmd)
	p := m.testsPanel()
	// The FailedArgv echo run ("rerun TestBad structured") parses to exactly
	// the one re-run test.
	if p.Rows() != 2 || len(p.FailedIDs()) != 0 {
		t.Fatalf("re-run result: rows=%d failed=%v", p.Rows(), p.FailedIDs())
	}
}

func TestStaleRunResultDropped(t *testing.T) {
	m := testsApp(t)
	m = runFakeTests(t, m)
	rows := m.testsPanel().Rows()
	out, _ := m.Update(TestRunDoneMsg{Seq: m.testRunSeq - 1, Output: "stale"})
	m = out.(Model)
	if m.testsPanel().Rows() != rows {
		t.Fatal("a stale completion must not overwrite the panel")
	}
}

func TestNoParserKeepsRunTerminalPath(t *testing.T) {
	m := testsApp(t)
	// The runtst-style language without ParseOutput: launchTestRun must
	// decline so launchRun falls through to the terminal path.
	lang.Register(lang.Language{
		ID:         "fkraw",
		Extensions: []string{"fr"},
		Test: &lang.TestSpec{
			FilePattern: `_test\.fr$`,
			Pattern:     `^func (?P<name>Test\w*)\s*\(`,
			FileArgv:    []string{"{interpreter}", "all"},
			Tool:        "/bin/echo",
		},
	})
	dir := t.TempDir()
	file := filepath.Join(dir, "y_test.fr")
	if err := os.WriteFile(file, []byte("func TestX(\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.runTest(file, nil)
	if m.testsPanel() != nil {
		t.Fatal("a language without a parser must not open the tests panel")
	}
}
