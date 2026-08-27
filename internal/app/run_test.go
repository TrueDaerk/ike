package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/registry"
	"ike/internal/run"
	"ike/internal/terminal"
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

// TestRunFileBottomDock verifies the default placement (#1905): the Run tool
// opens as a dedicated pane docked at the bottom workspace edge.
func TestRunFileBottomDock(t *testing.T) {
	m := runModel(t, "bottom")
	tm, _ := m.Update(RunFileMsg{})
	m = tm.(Model)
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatal("focus must land on the Run tool pane")
	}
	if !inst.Terminal().IsCommand() {
		t.Fatal("the pane must run a command session")
	}
	if inst.Terminal().Tool() != runToolName {
		t.Fatalf("tool marker = %q, want %q", inst.Terminal().Tool(), runToolName)
	}
	if inst.Terminal().Label() != "prog.rfake" {
		t.Fatalf("Run tool label = %q, want the config name", inst.Terminal().Label())
	}
	// Prefix, not equality: a command this short may already have finished,
	// and a finished session appends the #2192 exited marker to the chrome.
	if got := toolPaneTitle(inst.Terminal()); !strings.HasPrefix(got, "⚙ RUN — prog.rfake") {
		t.Fatalf("Run tool chrome = %q, want the tool plus the configuration", got)
	}
	if got := layout.EdgeLeaf(m.activeWS().Tree, layout.ZoneBottom); got != inst.Key() {
		t.Fatalf("bottom edge leaf = %q, want the Run tool pane %q", got, inst.Key())
	}
}

// TestRunFileLegacyNewTerminalPlacement covers the migration (#1905): a config
// still saying new_terminal keeps placing runs at the bottom edge.
func TestRunFileLegacyNewTerminalPlacement(t *testing.T) {
	m := runModel(t, "new_terminal")
	tm, _ := m.Update(RunFileMsg{})
	m = tm.(Model)
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindTerminal || inst.Terminal().Tool() != runToolName {
		t.Fatal("new_terminal must open the Run tool as a dedicated pane")
	}
	if got := layout.EdgeLeaf(m.activeWS().Tree, layout.ZoneBottom); got != inst.Key() {
		t.Fatalf("bottom edge leaf = %q, want the Run tool pane %q", got, inst.Key())
	}
}

// TestRunReusesRunTool verifies the rerun semantics (#1905): the second run
// replaces the Run tool's session in place — same pane, no second one.
func TestRunReusesRunTool(t *testing.T) {
	m := runModel(t, "bottom")
	tm, _ := m.Update(RunFileMsg{})
	m = tm.(Model)
	first := m.activeWS().Panes.FocusedInstance().Key()
	panes := m.activeWS().Panes.Len()

	tm, _ = m.Update(RunFileMsg{})
	m = tm.(Model)
	if got := m.activeWS().Panes.Len(); got != panes {
		t.Fatalf("panes after rerun = %d, want %d (the Run tool is reused)", got, panes)
	}
	locs := m.toolLocations(runToolName)
	if len(locs) != 1 || locs[0].key != first {
		t.Fatalf("rerun must reuse the one Run tool pane %q, got %+v", first, locs)
	}
	if !m.activeWS().Panes.Get(first).Terminal().IsCommand() {
		t.Fatal("the reused Run tool must host the new command session")
	}
}

// TestRunReusesRunToolTab is TestRunReusesRunTool for the in_pane placement:
// the rerun lands in the existing Run tab, not in a second one.
func TestRunReusesRunToolTab(t *testing.T) {
	m := runModel(t, "in_pane")
	tm, _ := m.Update(RunFileMsg{})
	m = tm.(Model)
	tm, _ = m.Update(RunFileMsg{})
	m = tm.(Model)
	inst := m.activeWS().Panes.FocusedInstance()
	if inst.TabCount() != 2 {
		t.Fatalf("tabs = %d after rerun, want 2 (the Run tab is reused)", inst.TabCount())
	}
	if term := inst.ActiveTerminal(); term == nil || term.Tool() != runToolName {
		t.Fatal("the run tab must carry the Run tool identity")
	}
}

// TestRunNeverHijacksOpenPanes is the #1905 regression guard: with a tool pane
// and a plain terminal pane open, a run leaves both alone and opens its own
// Run tool instead of dropping its output into one of them.
func TestRunNeverHijacksOpenPanes(t *testing.T) {
	withTools(t, sleepTool("watcher"))
	m := runModel(t, "bottom")
	tm, _ := m.Update(ToolOpenMsg{Name: "watcher"})
	m = tm.(Model)
	tool := m.toolPane("watcher")
	if tool == nil {
		t.Fatal("the tool pane must open")
	}
	t.Cleanup(func() { tool.Terminal().Close() })
	tm, _ = m.Update(TerminalNewMsg{})
	m = tm.(Model)
	shell := m.activeWS().Panes.FocusedInstance()
	if shell == nil || shell.Kind() != pane.KindTerminal || shell.Terminal().Tool() != "" {
		t.Fatal("terminal.new must open a plain terminal pane")
	}
	t.Cleanup(func() { shell.Terminal().Close() })
	shellSess := shell.Terminal().SessionKey()

	tm, _ = m.Update(RunFileMsg{})
	m = tm.(Model)

	locs := m.toolLocations(runToolName)
	if len(locs) != 1 {
		t.Fatalf("the run must open exactly one Run tool, got %+v", locs)
	}
	if runKey := locs[0].key; runKey == tool.Key() || runKey == shell.Key() {
		t.Fatalf("the run hijacked an open pane (%q)", runKey)
	}
	if tool.Terminal().Tool() != "watcher" || tool.TabCount() != 0 {
		t.Fatal("the tool pane must keep its own session and gain no run tab")
	}
	if shell.Terminal().Tool() != "" || shell.Terminal().IsCommand() {
		t.Fatal("the plain terminal must stay a plain shell")
	}
	if shell.Terminal().SessionKey() != shellSess {
		t.Fatal("the plain terminal's session must be untouched")
	}
}

// TestRunToolExitOverlayAndClose covers the tool-pane lifecycle (#810) for the
// Run tool: the pane survives the program's exit showing the restart/close
// dialog, and closes like any pane.
func TestRunToolExitOverlayAndClose(t *testing.T) {
	m := runModel(t, "bottom")
	tm, _ := m.Update(RunFileMsg{})
	m = tm.(Model)
	inst := m.activeWS().Panes.FocusedInstance()
	key := inst.Key()
	t.Cleanup(func() { inst.Terminal().Close() })
	inst.Terminal().Close()
	tm, _ = m.Update(terminal.ExitedMsg{Key: inst.Terminal().SessionKey()})
	m = tm.(Model)
	if !m.activeWS().Panes.Has(key) {
		t.Fatal("the Run tool pane must stay open when the program exits")
	}
	view := inst.Terminal().View()
	if !strings.Contains(view, "run exited") {
		t.Fatalf("the exit dialog must name the Run tool, view: %q", view)
	}
	if !strings.Contains(view, "[ Restart (r) ]") || !strings.Contains(view, "[ Close (ctrl+w) ]") {
		t.Fatalf("the exit dialog must offer restart/close, view: %q", view)
	}
	if !m.closeKey(key) || m.activeWS().Panes.Has(key) {
		t.Fatal("the Run tool pane must close like any tool pane")
	}
}

// TestRunToolIsSessionState guards the persistence rule (#1905): the Run
// tool's pane is recorded as session state, so a restart drops its leaf
// instead of re-running the program.
func TestRunToolIsSessionState(t *testing.T) {
	m := runModel(t, "bottom")
	tm, _ := m.Update(RunFileMsg{})
	m = tm.(Model)
	inst := m.activeWS().Panes.FocusedInstance()
	t.Cleanup(func() { inst.Terminal().Close() })
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	_, ids, ok := loadLayout()
	if !ok {
		t.Fatal("the layout must persist")
	}
	if got := ids[inst.Key()].Kind; got != "runTool" {
		t.Fatalf("Run tool identity = %q, want runTool", got)
	}
}

// TestRunToolFollowsSlotAssignment verifies the Run tool participates in slot
// placement (#1897) like every other tool: the assignment beats run.placement.
func TestRunToolFollowsSlotAssignment(t *testing.T) {
	withToolLayout(t, []string{"EEZ", "EEZ"}, []string{"Z=run"})
	m := runModel(t, "in_pane")
	tm, _ := m.Update(RunFileMsg{})
	m = tm.(Model)
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindTerminal || inst.Terminal().Tool() != runToolName {
		t.Fatal("an assigned slot must open the Run tool as its own pane")
	}
	t.Cleanup(func() { inst.Terminal().Close() })
	if got := layout.EdgeLeaf(m.activeWS().Tree, layout.ZoneRight); got != inst.Key() {
		t.Fatalf("right-edge slot leaf = %q, want the Run tool pane %q", got, inst.Key())
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
