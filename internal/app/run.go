package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/run"
	"ike/internal/terminal"
)

// run.go wires run configurations end to end (0350, #576): run.file executes
// the active file through its language's run command in an integrated
// terminal; run.rerun repeats the last configuration.
//
// Output goes to the **Run tool** (#1905), a dedicated tool pane like the
// [[tools.custom]] ones: the first run opens it, every later run replaces its
// session in place. Runs never take over an arbitrary terminal any more — the
// pre-#1905 "first reusable terminal" scan dropped run output into whatever
// terminal pane or tool tab happened to exist. The run.placement setting names
// the Run tool's home position (bottom/left/right/top, or in_pane for a tab in
// the editor pane); a slot assignment (#1897) overrides it like for any tool.

// runToolName is the Run tool's identity (#1905) — the name its sessions carry
// (terminal.Model.SetTool), the id a [tools.layout] slot assignment addresses,
// and what the #810 exited overlay titles the pane with. Reserved: a
// [[tools.custom]] entry of the same name would be indistinguishable from it.
const runToolName = "run"

// runInPane is the run.placement value that keeps run output as a terminal tab
// in the focused editor pane instead of docking the Run tool at a workspace
// edge; every other value names a home position (toolHomeZone).
const runInPane = "in_pane"

// runCurrentFile is the run.file handler: it ensures a configuration for the
// active file (creating and persisting the default on first run) and launches it.
func (m *Model) runCurrentFile() tea.Cmd {
	path := m.activeFilePath()
	if path == "" {
		m.host.Notify(host.Info, "run: focus a file tab first")
		return nil
	}
	root := projectRoot()
	store := run.Load()
	cfg, created, ok := store.EnsureFor(root, path)
	if !ok {
		m.host.Notify(host.Info, "run: no run command for this file type")
		return nil
	}
	return m.launchRun(root, store, cfg, created)
}

// runTestAtCursor is the run.testAtCursor handler (#1150): it resolves the
// test declared at or nearest above the focused editor's cursor and runs
// exactly that test in the run-terminal placement, registering the synthesized
// configuration with run.rerun's last-used memory.
func (m *Model) runTestAtCursor() tea.Cmd {
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, "run: focus a file tab first")
		return nil
	}
	if !lang.HasTests(ed.Path()) {
		m.host.Notify(host.Info, "run: no test runner for this file")
		return nil
	}
	line, _ := ed.CursorPos()
	t, ok := ed.NearestTestAt(line)
	if !ok {
		m.host.Notify(host.Info, "run: no test at or above the cursor")
		return nil
	}
	return m.runTest(ed.Path(), &t)
}

// runTestsInFile is the run.testsInFile handler (#1150): it runs every test
// in the active test file's scope (Go: plain `go test` in the file's package
// directory).
func (m *Model) runTestsInFile() tea.Cmd {
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, "run: focus a file tab first")
		return nil
	}
	if !lang.HasTests(ed.Path()) {
		m.host.Notify(host.Info, "run: no test runner for this file")
		return nil
	}
	return m.runTest(ed.Path(), nil)
}

// runTest synthesizes and launches the test-scope configuration for path —
// one test when t is non-nil, the whole file scope otherwise. Upsert keeps
// re-runs of the same test as one named configuration; launchRun touches the
// store, so run.rerun repeats the test.
func (m *Model) runTest(path string, t *lang.TestMatch) tea.Cmd {
	root := projectRoot()
	cfg, ok := run.TestConfig(root, path, t)
	if !ok {
		m.host.Notify(host.Info, "run: no test runner for this file")
		return nil
	}
	store := run.Load()
	created := store.ByName(cfg.Name) == nil
	return m.launchRun(root, store, store.Upsert(cfg), created)
}

// rerunLast is the run.rerun handler: it launches the last-used configuration.
func (m *Model) rerunLast() tea.Cmd {
	root := projectRoot()
	store := run.Load()
	cfg := store.Last()
	if cfg == nil {
		m.host.Notify(host.Info, "run: nothing to rerun yet")
		return nil
	}
	return m.launchRun(root, store, cfg, false)
}

// launchRun synthesizes cfg's command line and streams it into a terminal
// per the placement rules. The store is persisted with the updated last-used
// marker; a failed save warns but never blocks the run.
func (m *Model) launchRun(root string, store run.Store, cfg *run.Config, created bool) tea.Cmd {
	// A test-scope run with an output parser goes to the Test Results tool
	// window (#1911) instead of the run terminal; tests.results_window off,
	// or a language without a parser, keeps the raw path below.
	if cmd, ok := m.launchTestRun(root, store, cfg, created); ok {
		return cmd
	}
	argv, ok := run.Argv(root, *cfg, m.explicitInterpreter(cfg.Lang))
	if !ok {
		m.host.Notify(host.Error, "run: "+cfg.Lang+" contributes no run command")
		return nil
	}
	// Only a stored configuration becomes the rerun target (#1915): an
	// ephemeral task run must not point last_used at a name the store cannot
	// resolve.
	if store.ByName(cfg.Name) != nil {
		store.Touch(cfg.Name)
		if err := run.Save(store); err != nil {
			m.host.Notify(host.Warn, "run: config not saved: "+err.Error())
		}
	}
	env := terminal.MergeEnv(terminalEnv(), cfg.EnvSlice())
	dir := cfg.Dir(root)

	// The problem-matcher tee (#1915) is built before the launch so the run's
	// previous findings clear even when no matcher resolves.
	tap := m.taskOutputTap(root, cfg)

	// The Run tool owns run output (#1905): an open one takes the new command
	// in place, otherwise it opens at its placement. No other terminal is ever
	// touched — a plain terminal the user opened stays theirs.
	if !m.startInRunTool(cfg, argv, dir, env) && !m.openRunTool(cfg, argv, dir, env) {
		m.host.Notify(host.Error, "run: no pane to place the run tool")
		return nil
	}
	// The tap installs onto the fresh session right after the spawn — the
	// child has not produced output yet, so the tee sees the run's whole
	// stream.
	if tap != nil {
		if term := m.runToolTerminal(); term != nil {
			term.SetOutputTap(tap)
		}
	}
	m.notifyRun(cfg, created, argv)
	return nil
}

// startInRunTool restarts an already open Run tool with the new command,
// wherever it lives — dedicated pane or hosted tab (#1905). The session is
// replaced in place, ending a still-running program: the Run tool is the run's
// one home, so a rerun reuses it instead of piling up panes. Reports whether a
// Run tool was there to take the command.
func (m *Model) startInRunTool(cfg *run.Config, argv []string, dir string, env []string) bool {
	locs := m.toolLocations(runToolName)
	if len(locs) == 0 {
		return false
	}
	loc := locs[0]
	inst := m.activeWS().Panes.Get(loc.key)
	if inst == nil {
		return false
	}
	term := inst.Terminal()
	if loc.tab >= 0 {
		term = inst.TabTerminal(loc.tab)
	}
	if term == nil {
		return false
	}
	key := term.SessionKey()
	if key == "" {
		key = m.activeWS().Panes.MintTerminalKey()
	}
	term.StartCommand(key, argv, dir, env)
	term.SetLabel(cfg.Name)
	if loc.tab >= 0 {
		inst.ActivateTab(loc.tab)
	}
	m.setFocus(loc.key)
	m.rememberTool(runToolName, loc.key)
	return true
}

// openRunTool opens the Run tool for cfg's command: the in_pane placement adds
// it as a terminal tab in the focused editor pane, every other value goes
// through the shared tool placement (slot assignment > home position >
// adaptive split). Reports whether the tool materialized.
func (m *Model) openRunTool(cfg *run.Config, argv []string, dir string, env []string) bool {
	sp := toolSpawn{
		name:  runToolName,
		label: cfg.Name, // chrome names the pane/tab after the configuration
		argv:  argv,
		dir:   dir,
		env:   env,
	}
	placement := m.runPlacement()
	// A slot assignment (#1897) pins the Run tool like any other tool — it
	// wins over in_pane exactly as it wins over a home position.
	if _, slot := assignedSlot(runToolName); placement == runInPane && slot == "" {
		if target := m.activeEditorKey(); target != "" {
			m.activeWS().Panes.Get(target).AddTerminalTab(m.newToolTab(sp))
			m.activeWS().ReturnFocus = m.activeWS().Panes.Focused()
			m.setFocus(target)
			m.rememberTool(runToolName, target)
			m.layout()
			saveLayout(m.activeWS().Tree, m.activeWS().Panes)
			return true
		}
		placement = "" // no editor pane: fall back to the adaptive split
	}
	return m.placeTool(sp, placement)
}

// runPlacement reads the Run tool's home position (#1905). The pre-#1905
// new_terminal value — a bottom-split terminal pane — lives on as an alias for
// the bottom dock, so an old config keeps placing runs where it did.
func (m Model) runPlacement() string {
	v, ok := m.host.Config().Get("run.placement")
	if !ok || v == "" {
		return "bottom"
	}
	if v == "new_terminal" {
		return "bottom"
	}
	return v
}

// notifyRun surfaces what was launched; the first run also says the default
// configuration was stored.
func (m *Model) notifyRun(cfg *run.Config, created bool, argv []string) {
	msg := "run: " + strings.Join(argv, " ")
	if created {
		msg += "  (saved as \"" + cfg.Name + "\")"
	}
	m.host.Notify(host.Info, msg)
}

// explicitInterpreter reads the user's [lang.<id>] interpreter choice, the
// same seam the LSP toolchain and terminal shims resolve through.
func (m Model) explicitInterpreter(langID string) string {
	if v, ok := m.host.Config().Get("lang." + langID + ".interpreter"); ok {
		return v
	}
	return ""
}

// projectRoot is the absolute working directory (IKE chdirs into the project).
func projectRoot() string {
	if wd, err := cachedGetwd(); err == nil {
		return wd
	}
	return "."
}
