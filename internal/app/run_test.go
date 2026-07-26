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
	inst := m.activeWS().Panes.FocusedInstance()
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
	inst := m.activeWS().Panes.FocusedInstance()
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
	inst := m.activeWS().Panes.FocusedInstance()
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
	before := m.activeWS().Panes.Len()
	tm, _ = m.Update(RunFileMsg{})
	m = tm.(Model)
	if m.activeWS().Panes.Len() != before {
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
	before := m.activeWS().Panes.Len()
	tm, _ = m.Update(RunRerunMsg{})
	if tm.(Model).activeWS().Panes.Len() != before {
		t.Fatal("rerun with no history must not open panes")
	}
}

// TestRunScratchFromProjectRoot covers #1223: a scratch file lives outside the
// project tree, yet run.file executes it from the project root, so the
// project's toolchain (venv, [lang.<id>] interpreter) and its relative paths
// apply exactly like for a file inside the repository.
func TestRunScratchFromProjectRoot(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	m = dispatch(t, m, NewScratchMsg{Ext: "rfake"})

	scratchPath := m.activeWS().Panes.FocusedInstance().Editor().Path()
	tm, _ := m.Update(RunFileMsg{})
	m = tm.(Model)

	store := run.Load()
	cfg := store.ByName("scratch-1.rfake")
	if cfg == nil {
		t.Fatalf("the scratch run must persist a configuration: %+v", store)
	}
	// Outside the project tree, so the file is stored absolute — and the
	// working directory stays the project root.
	if cfg.File != scratchPath {
		t.Fatalf("cfg.File = %q, want the absolute scratch path %q", cfg.File, scratchPath)
	}
	root := projectRoot()
	if cfg.Cwd != "" || cfg.Dir(root) != root {
		t.Fatalf("scratch run must use the project root as cwd (cwd %q, dir %q)", cfg.Cwd, cfg.Dir(root))
	}
	argv, ok := run.Argv(root, *cfg, "")
	if !ok || argv[len(argv)-1] != scratchPath {
		t.Fatalf("argv = %v, %v; want the scratch as the target", argv, ok)
	}
	if term := m.activeWS().Panes.FocusedInstance().ActiveTerminal(); term == nil || !term.IsCommand() {
		t.Fatal("the scratch run must open a command terminal")
	}
}
