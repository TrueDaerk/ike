package app

import (
	"os"
	"strings"

	"ike/internal/host"
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
	if inst, tab, term := m.panes.ReusableRunTerminal(); term != nil {
		key := term.SessionKey()
		if key == "" {
			key = m.panes.MintTerminalKey()
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
			inst := m.panes.Get(target)
			key := m.panes.MintTerminalKey()
			term := terminal.NewCommand(key, argv, dir, 80, 24, env, m.host.Send)
			term.SetLabel(cfg.Name)
			inst.AddTerminalTab(term)
			m.setFocus(target)
			saveLayout(m.tree, m.panes)
			m.notifyRun(cfg, created, argv)
			return
		}
	}
	// new_terminal placement — and the in_pane fallback when no editor pane
	// exists: a bottom-split terminal pane, like terminal.new.
	target := m.activeEditorKey()
	if target == "" {
		target = m.panes.Focused()
	}
	if target == "" || m.tree == nil {
		m.host.Notify(host.Error, "run: no pane to place the terminal")
		return
	}
	key := m.panes.AddCommandTerminal(argv, cfg.Name, dir, env, m.host.Send)
	tree, ok := layout.SplitLeaf(m.tree, target, key, layout.ZoneBottom)
	if !ok {
		m.panes.Close(key)
		return
	}
	m.tree = tree
	m.setFocus(key)
	m.layout()
	saveLayout(m.tree, m.panes)
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
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}
