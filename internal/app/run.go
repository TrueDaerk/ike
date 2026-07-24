package app

import (
	"strings"

	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/layout"
	"ike/internal/run"
	"ike/internal/terminal"
)

// run.go wires run configurations end to end (0350, #576): run.file executes
// the active file through its language's run command in an integrated
// terminal; run.rerun repeats the last configuration. Placement: a reusable
// (never-typed-in or finished) terminal is taken over first; otherwise the
// run.placement setting decides between a terminal tab in the editor pane
// (in_pane) and a fresh bottom terminal pane (new_terminal).

// runCurrentFile is the run.file handler: it ensures a configuration for the
// active file (creating and persisting the default on first run) and launches it.
func (m *Model) runCurrentFile() {
	path := m.activeFilePath()
	if path == "" {
		m.host.Notify(host.Info, "run: focus a file tab first")
		return
	}
	root := projectRoot()
	store := run.Load()
	cfg, created, ok := store.EnsureFor(root, path)
	if !ok {
		m.host.Notify(host.Info, "run: no run command for this file type")
		return
	}
	m.launchRun(root, store, cfg, created)
}

// runTestAtCursor is the run.testAtCursor handler (#1150): it resolves the
// test declared at or nearest above the focused editor's cursor and runs
// exactly that test in the run-terminal placement, registering the synthesized
// configuration with run.rerun's last-used memory.
func (m *Model) runTestAtCursor() {
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, "run: focus a file tab first")
		return
	}
	if !lang.HasTests(ed.Path()) {
		m.host.Notify(host.Info, "run: no test runner for this file")
		return
	}
	line, _ := ed.CursorPos()
	t, ok := ed.NearestTestAt(line)
	if !ok {
		m.host.Notify(host.Info, "run: no test at or above the cursor")
		return
	}
	m.runTest(ed.Path(), &t)
}

// runTestsInFile is the run.testsInFile handler (#1150): it runs every test
// in the active test file's scope (Go: plain `go test` in the file's package
// directory).
func (m *Model) runTestsInFile() {
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, "run: focus a file tab first")
		return
	}
	if !lang.HasTests(ed.Path()) {
		m.host.Notify(host.Info, "run: no test runner for this file")
		return
	}
	m.runTest(ed.Path(), nil)
}

// runTest synthesizes and launches the test-scope configuration for path —
// one test when t is non-nil, the whole file scope otherwise. Upsert keeps
// re-runs of the same test as one named configuration; launchRun touches the
// store, so run.rerun repeats the test.
func (m *Model) runTest(path string, t *lang.TestMatch) {
	root := projectRoot()
	cfg, ok := run.TestConfig(root, path, t)
	if !ok {
		m.host.Notify(host.Info, "run: no test runner for this file")
		return
	}
	store := run.Load()
	created := store.ByName(cfg.Name) == nil
	m.launchRun(root, store, store.Upsert(cfg), created)
}

// rerunLast is the run.rerun handler: it launches the last-used configuration.
func (m *Model) rerunLast() {
	root := projectRoot()
	store := run.Load()
	cfg := store.Last()
	if cfg == nil {
		m.host.Notify(host.Info, "run: nothing to rerun yet")
		return
	}
	m.launchRun(root, store, cfg, false)
}

// launchRun synthesizes cfg's command line and streams it into a terminal
// per the placement rules. The store is persisted with the updated last-used
// marker; a failed save warns but never blocks the run.
func (m *Model) launchRun(root string, store run.Store, cfg *run.Config, created bool) {
	argv, ok := run.Argv(root, *cfg, m.explicitInterpreter(cfg.Lang))
	if !ok {
		m.host.Notify(host.Error, "run: "+cfg.Lang+" contributes no run command")
		return
	}
	store.Touch(cfg.Name)
	if err := run.Save(store); err != nil {
		m.host.Notify(host.Warn, "run: config not saved: "+err.Error())
	}
	env := terminal.MergeEnv(terminalEnv(), cfg.EnvSlice())
	dir := cfg.Dir(root)

	// A reusable terminal (never typed into, or its process ended) is taken
	// over in place — pane or tab (#574).
	if inst, tab, term := m.activeWS().Panes.ReusableRunTerminal(); term != nil {
		key := term.SessionKey()
		if key == "" {
			key = m.activeWS().Panes.MintTerminalKey()
		}
		term.StartCommand(key, argv, dir, env)
		term.SetLabel(cfg.Name)
		if tab >= 0 {
			inst.ActivateTab(tab)
		}
		m.setFocus(inst.Key())
		m.notifyRun(cfg, created, argv)
		return
	}

	placement := "in_pane"
	if v, ok := m.host.Config().Get("run.placement"); ok && v != "" {
		placement = v
	}
	if placement == "in_pane" {
		if target := m.activeEditorKey(); target != "" {
			inst := m.activeWS().Panes.Get(target)
			key := m.activeWS().Panes.MintTerminalKey()
			term := terminal.NewCommand(key, argv, dir, 80, 24, env, m.host.Send)
			term.SetLabel(cfg.Name)
			inst.AddTerminalTab(term)
			m.setFocus(target)
			saveLayout(m.activeWS().Tree, m.activeWS().Panes)
			m.notifyRun(cfg, created, argv)
			return
		}
	}
	// new_terminal placement — and the in_pane fallback when no editor pane
	// exists: a bottom-split terminal pane, like terminal.new.
	target := m.activeEditorKey()
	if target == "" {
		target = m.activeWS().Panes.Focused()
	}
	if target == "" || m.activeWS().Tree == nil {
		m.host.Notify(host.Error, "run: no pane to place the terminal")
		return
	}
	key := m.activeWS().Panes.AddCommandTerminal(argv, cfg.Name, dir, env, m.host.Send)
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, key, layout.ZoneBottom)
	if !ok {
		m.activeWS().Panes.Close(key)
		return
	}
	m.activeWS().Tree = tree
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	m.notifyRun(cfg, created, argv)
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
