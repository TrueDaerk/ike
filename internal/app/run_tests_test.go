package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/registry"
	"ike/internal/run"
)

// The fake test language (#1150): detection on _test.atst files, fast-exiting
// echo argv so spawned command sessions never linger.
func init() {
	lang.Register(lang.Language{
		ID:         "apptst",
		Extensions: []string{"atst"},
		Test: &lang.TestSpec{
			FilePattern: `_test\.atst$`,
			Pattern:     `^func (?P<name>(?P<kind>Test)\w*)\s*\(`,
			Kinds:       map[string][]string{"Test": {"{interpreter}", "ran", "^{name}$"}},
			FileArgv:    []string{"{interpreter}", "ran-all"},
			Tool:        "/bin/echo",
		},
	})
}

// lastRunCfg reloads the store and returns its last-used configuration.
func lastRunCfg() *run.Config {
	s := run.Load()
	return s.Last()
}

// testRunModel is a sized model showing a test file whose first line declares
// TestOne and line 3 declares TestTwo.
func testRunModel(t *testing.T) (Model, string) {
	t.Helper()
	if testStoreRoot != "" {
		os.Setenv("IKE_CONFIG_DIR", filepath.Join(testStoreRoot, "runtests-"+t.Name()))
	}
	path := filepath.Join(t.TempDir(), "x_test.atst")
	content := "func TestOne(t T) {\n}\nhelper\nfunc TestTwo(t T) {\n}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewWith(registry.New(), host.MapConfig{
		"run.placement":       "in_pane",
		"editor.line_numbers": "true",
	})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tm, _ = tm.(Model).Update(explorer.OpenFileMsg{Path: path})
	return tm.(Model), path
}

// TestRunTestAtCursor: the cursor sits on TestOne's declaration line; the
// command runs exactly that test in the run-terminal placement and registers
// it as the rerun-last target.
func TestRunTestAtCursor(t *testing.T) {
	m, _ := testRunModel(t)
	tm, _ := m.Update(RunTestAtCursorMsg{})
	m = tm.(Model)
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindEditor || inst.TabCount() != 2 {
		t.Fatal("the test must run as a terminal tab in the editor pane")
	}
	if term := inst.ActiveTerminal(); term == nil || !term.IsCommand() {
		t.Fatal("the active tab must host the test run's command terminal")
	}
	store := run.Load()
	last := store.Last()
	if last == nil || !last.Tests || last.TestName != "TestOne" || last.TestKind != "Test" {
		t.Fatalf("rerun memory must hold the test config, got %+v", last)
	}
}

// TestRunTestAtCursorNearest: with the cursor below TestTwo, the nearest
// preceding declaration wins.
func TestRunTestAtCursorNearest(t *testing.T) {
	m, _ := testRunModel(t)
	// Move the caret to the last line (G) inside the focused editor.
	tm, _ := m.Update(tea.KeyPressMsg{Text: "G", Code: 'G'})
	m = tm.(Model)
	tm, _ = m.Update(RunTestAtCursorMsg{})
	m = tm.(Model)
	if last := lastRunCfg(); last == nil || last.TestName != "TestTwo" {
		t.Fatalf("nearest test above the cursor must run, got %+v", lastRunCfg())
	}
}

// TestRunTestsInFile runs the file scope and registers it for rerun.
func TestRunTestsInFile(t *testing.T) {
	m, _ := testRunModel(t)
	tm, _ := m.Update(RunTestsInFileMsg{})
	m = tm.(Model)
	last := lastRunCfg()
	if last == nil || !last.Tests || last.TestName != "" {
		t.Fatalf("file-scope test config must be last-used, got %+v", last)
	}
	if inst := m.activeWS().Panes.FocusedInstance(); inst == nil || inst.TabCount() != 2 {
		t.Fatal("the run must open a terminal tab")
	}
}

// TestRunTestRerun: run.rerun after a test run repeats the test (reusing the
// finished terminal, no extra tab).
func TestRunTestRerun(t *testing.T) {
	m, _ := testRunModel(t)
	tm, _ := m.Update(RunTestAtCursorMsg{})
	m = tm.(Model)
	tm, _ = m.Update(RunRerunMsg{})
	m = tm.(Model)
	if inst := m.activeWS().Panes.FocusedInstance(); inst == nil || inst.TabCount() != 2 {
		t.Fatal("rerun must reuse the finished test terminal")
	}
	if last := lastRunCfg(); last == nil || last.TestName != "TestOne" {
		t.Fatalf("rerun must keep the test config last-used, got %+v", lastRunCfg())
	}
}

// TestRunTestAtCursorNoTest: a file without a marker at or above the cursor
// notifies instead of running.
func TestRunTestAtCursorNoTest(t *testing.T) {
	if testStoreRoot != "" {
		os.Setenv("IKE_CONFIG_DIR", filepath.Join(testStoreRoot, "runtests-none"))
	}
	path := filepath.Join(t.TempDir(), "y_test.atst")
	if err := os.WriteFile(path, []byte("helper\nfunc TestLate(t T) {\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewWith(registry.New(), host.MapConfig{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tm, _ = tm.(Model).Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	before := m.activeWS().Panes.Len()
	tm, _ = m.Update(RunTestAtCursorMsg{}) // cursor on line 0, no test at or above
	if tm.(Model).activeWS().Panes.Len() != before {
		t.Fatal("no test above the cursor must not open panes")
	}
}

// editorRectOf finds the editor pane's rect for mouse targeting.
func editorRectOf(t *testing.T, m Model) layout.Rect {
	t.Helper()
	for key, rect := range m.lay.Panes {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
			return rect
		}
	}
	t.Fatal("no editor pane rect")
	return layout.Rect{}
}

// TestGutterClickDecision (#1150): a plain left click in the gutter keeps
// toggling the breakpoint — on every line, including test-marker lines — and
// ctrl/cmd+click on a marker line runs that test instead.
func TestGutterClickDecision(t *testing.T) {
	m, path := testRunModel(t)
	r := editorRectOf(t, m)
	x := r.X + paneContentX // first gutter cell (the sign column)
	y := r.Y + paneContentY // row of line 0 = TestOne's declaration

	// Plain click: breakpoint toggles, nothing runs.
	tm, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m = tm.(Model)
	if got := m.bpts.Lines(bpKey(path)); len(got) != 1 || got[0] != 0 {
		t.Fatalf("plain gutter click must toggle the breakpoint, lines = %v", got)
	}
	if lastRunCfg() != nil {
		t.Fatal("plain gutter click must not run the test")
	}

	// ctrl+click on a non-marker line still toggles the breakpoint.
	tm, _ = m.Update(tea.MouseClickMsg{X: x, Y: y + 2, Button: tea.MouseLeft, Mod: tea.ModCtrl})
	m = tm.(Model)
	if got := m.bpts.Lines(bpKey(path)); len(got) != 2 {
		t.Fatalf("ctrl+click off the marker must toggle a breakpoint, lines = %v", got)
	}

	// ctrl+click on the marker line: the test runs, the breakpoints stay.
	// (Run last: the spawned run terminal becomes the pane's active tab and
	// would swallow later editor clicks.)
	tm, _ = m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft, Mod: tea.ModCtrl})
	m = tm.(Model)
	if last := lastRunCfg(); last == nil || last.TestName != "TestOne" {
		t.Fatalf("ctrl+gutter-click on the marker must run the test, got %+v", lastRunCfg())
	}
	if got := m.bpts.Lines(bpKey(path)); len(got) != 2 {
		t.Fatalf("the run click must not touch breakpoints, lines = %v", got)
	}
}
