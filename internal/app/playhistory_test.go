package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/jqplay"
	"ike/internal/watch"
)

// playhistory_test.go pins the program history's lifetime (#2536): one list
// per user, independent of the buffer, the dialect, the source snapshot and
// the process. The in-memory sharing is covered by TestJQPlaygroundHistory*
// (#1973, #1977) and TestYQPlaygroundHistoryIsShared (#2039); these are the
// cases the issue reports as lost — an external overwrite of the source file,
// a removal that ends the mode, and a restart.

// openOtherFile opens name with body in the app and returns the model with
// that buffer focused.
func openOtherFile(t *testing.T, m Model, name, body string) Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.openPath(path, false)
	return drainCmd(tm.(Model), cmd)
}

// TestJQPlaygroundHistorySurvivesSourceOverwrite is the issue's acceptance
// case: programs run before the source file is overwritten externally are
// offered by ↑ after the refresh, after closing and reopening the mode on the
// same file, and — in the other dialect — over another file.
func TestJQPlaygroundHistorySurvivesSourceOverwrite(t *testing.T) {
	m, path := playWatchApp(t, `{"a":1,"b":2}`)
	m = dismissOnboarding(m)
	m = runJQProgram(m, ".a")
	m = runJQProgram(m, ".b")

	m = playExternalWrite(t, m, path, `{"a":10,"b":20}`)
	if !m.playOpen() {
		t.Fatal("an overwrite renews the input; the playground must stay open")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.play.program.Text; got != ".a" {
		t.Fatalf("↑ after the overwrite = %q, want the program before the one on the line", got)
	}

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	m = openJQ(t, m)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.play.program.Text; got != ".b" {
		t.Fatalf("↑ after reopening on the overwritten file = %q, want the newest program", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	// The same list, from the yq playground over a YAML file.
	m = openOtherFile(t, m, "deploy.yaml", "spec:\n  replicas: 3\n")
	m = openYQ(t, m)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.play.program.Text; got != ".b" {
		t.Errorf("↑ in the yq playground over another file = %q, want the jq program run before the overwrite", got)
	}
	if m.play.hist != m.playHistory {
		t.Error("the playground must share the root model's one history list")
	}
}

// TestJQPlaygroundHistorySurvivesSourceRemoval: a rename-away or delete ends
// the mode (#2356) — the program on the query line, never committed with
// enter, is recorded by that close like by any other, and offered over the
// next file.
func TestJQPlaygroundHistorySurvivesSourceRemoval(t *testing.T) {
	m, path := playWatchApp(t, `{"foo":1}`)
	m = dismissOnboarding(m)
	m = setProgram(m, ".foo")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.Update(watch.EventMsg{Kind: watch.FileRemoved, Path: path})
	m = drainCmd(tm.(Model), cmd)
	if m.playOpen() {
		t.Fatal("the playground must close over a removed file")
	}

	m = openOtherFile(t, m, "other.json", `{"foo":2}`)
	m = openJQ(t, m)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.play.program.Text; got != ".foo" {
		t.Errorf("↑ over the next file = %q, want the program the removal closed over", got)
	}
}

// TestJQPlaygroundHistoryPersistsAcrossRestart: the list is per user, on
// disk under the config directory, so a fresh process offers the programs
// the previous one ran — newest first, from either dialect.
func TestJQPlaygroundHistoryPersistsAcrossRestart(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m = dismissOnboarding(m)
	m = runJQProgram(m, ".a")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	m = openOtherFile(t, m, "deploy.yaml", "b: 2\n")
	m = openYQ(t, m)
	m = setProgram(m, ".b")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, err := os.Stat(jqplay.HistoryFile()); err != nil {
		t.Fatalf("the history must be written under IKE_CONFIG_DIR: %v", err)
	}

	// A second process: a fresh model over the same config directory.
	fresh := New()
	tm, _ := fresh.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m2 := dismissOnboarding(tm.(Model))
	m2 = openOtherFile(t, m2, "data.json", `{"c":3}`)
	m2 = openJQ(t, m2)
	m2 = drainKey(m2, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m2.play.program.Text; got != ".b" {
		t.Fatalf("↑ after a restart = %q, want the newest program of the previous run", got)
	}
	m2 = drainKey(m2, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m2.play.program.Text; got != ".a" {
		t.Errorf("second ↑ after a restart = %q, want the older program", got)
	}
}

// TestJQPlaygroundHistoryFileIsPerUser: the app's list is the one
// jqplay.HistoryFile names — user state, not the project's .ike.
func TestJQPlaygroundHistoryFileIsPerUser(t *testing.T) {
	m := newSized()
	m.playHist().Add(".x")
	want := filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "playground-history.json")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("history file %s: %v", want, err)
	}
	if _, err := os.Stat(filepath.Join(".ike", "playground-history.json")); err == nil {
		t.Error("the history must not land in the project's .ike")
	}
}
