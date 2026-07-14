package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/pane"
	"ike/internal/registry"
	"ike/internal/run"
)

// runFakeToolchain contributes a fast-exiting run command for .rfake files.
type runFakeToolchain struct{}

func (runFakeToolchain) Detect(string) (map[string]any, bool) { return nil, false }
func (runFakeToolchain) RunCommand(_ string, spec lang.RunSpec, _ string) ([]string, bool) {
	return []string{"/bin/echo", "ran", spec.File}, true
}

func init() {
	lang.Register(lang.Language{ID: "runfake", Extensions: []string{"rfake"}, Toolchain: runFakeToolchain{}})
}

// runModel builds a sized model with the given placement whose active editor
// shows a runnable temp file.
func runModel(t *testing.T, placement string) Model {
	t.Helper()
	if testStoreRoot != "" {
		os.Setenv("IKE_CONFIG_DIR", filepath.Join(testStoreRoot, "run-"+t.Name()+"-"+placement))
	}
	path := filepath.Join(t.TempDir(), "prog.rfake")
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewWith(registry.New(), host.MapConfig{"run.placement": placement})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tm, _ = tm.(Model).Update(explorer.OpenFileMsg{Path: path})
	return tm.(Model)
}

// TestRunFileInPane verifies the in_pane placement: the run opens as a
// terminal tab in the focused editor pane, labelled after the config, and the
// default configuration is persisted.
func TestRunFileInPane(t *testing.T) {
	m := runModel(t, "in_pane")
	tm, _ := m.Update(RunFileMsg{})
	m = tm.(Model)
	inst := m.panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindEditor {
		t.Fatal("focus must stay on the editor pane")
	}
	if inst.TabCount() != 2 {
		t.Fatalf("tabs = %d, want 2 (file + run terminal)", inst.TabCount())
	}
	term := inst.ActiveTerminal()
	if term == nil || !term.IsCommand() {
		t.Fatal("the active tab must host the run's command terminal")
	}
	if term.Label() != "prog.rfake" {
		t.Fatalf("terminal label = %q, want the config name", term.Label())
	}
	store := run.Load()
	if store.LastUsed != "prog.rfake" || store.ByName("prog.rfake") == nil {
		t.Fatalf("default config must persist with last-used set: %+v", store)
	}
}

// TestRunFileNewTerminal verifies the new_terminal placement: a bottom
// terminal pane running the command.
func TestRunFileNewTerminal(t *testing.T) {
	m := runModel(t, "new_terminal")
	tm, _ := m.Update(RunFileMsg{})
	m = tm.(Model)
	inst := m.panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatal("focus must land on the new terminal pane")
	}
	if !inst.Terminal().IsCommand() {
		t.Fatal("the pane must run a command session")
	}
}

// TestRunReusesFinishedTerminal verifies the take-over: a second run replaces
// the first run's (unoccupied) terminal instead of opening another.
func TestRunReusesFinishedTerminal(t *testing.T) {
	m := runModel(t, "in_pane")
	tm, _ := m.Update(RunFileMsg{})
	m = tm.(Model)
	tm, _ = m.Update(RunFileMsg{})
	m = tm.(Model)
	inst := m.panes.FocusedInstance()
	if inst.TabCount() != 2 {
		t.Fatalf("tabs = %d after rerun, want 2 (terminal reused)", inst.TabCount())
	}
}

// TestRunUnknownFileType is a friendly no-op.
func TestRunUnknownFileType(t *testing.T) {
	if testStoreRoot != "" {
		os.Setenv("IKE_CONFIG_DIR", filepath.Join(testStoreRoot, "run-unknown"))
	}
	path := filepath.Join(t.TempDir(), "prog.unknowable")
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewWith(registry.New(), host.MapConfig{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tm, _ = tm.(Model).Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	before := m.panes.Len()
	tm, _ = m.Update(RunFileMsg{})
	m = tm.(Model)
	if m.panes.Len() != before {
		t.Fatal("an unrunnable file must not open panes")
	}
}

// TestRerunWithoutHistory is a friendly no-op too.
func TestRerunWithoutHistory(t *testing.T) {
	if testStoreRoot != "" {
		os.Setenv("IKE_CONFIG_DIR", filepath.Join(testStoreRoot, "rerun-empty"))
	}
	m := NewWith(registry.New(), host.MapConfig{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(Model)
	before := m.panes.Len()
	tm, _ = m.Update(RunRerunMsg{})
	if tm.(Model).panes.Len() != before {
		t.Fatal("rerun with no history must not open panes")
	}
}
