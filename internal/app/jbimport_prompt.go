package app

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/keymap"
	"ike/internal/keymap/jbimport"
	"ike/internal/pathcomplete"
	"ike/internal/ui"
)

// jbimport_prompt.go drives the JetBrains keymap import (#677): the
// keymap.importJetBrains command opens a shell prompt asking for the exported
// XML's path (tab = filesystem completion via pathcomplete, mirroring the
// settings path inputs); enter runs the import, writing keymap.bindings.*
// overrides at user scope and reporting a summary toast.

// ImportJetBrainsKeymapMsg asks the root model to open the JetBrains keymap
// import prompt. Dispatched by keymap.importJetBrains (palette).
type ImportJetBrainsKeymapMsg struct{}

// jbImportDoneMsg carries a finished import back into Update: the summary (or
// error) for the toast plus the config reload the writes produced.
type jbImportDoneMsg struct {
	summary string
	err     error
	reload  tea.Msg
}

// startJBImport opens the shell prompt asking for the export's path.
func (m *Model) startJBImport() {
	m.jbImportOpen = true
	m.jbImportInput = "~" + string(os.PathSeparator)
	m.jbImportPos = len([]rune(m.jbImportInput))
	m.renderJBImportPrompt(nil)
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// jbImportPromptOpen reports whether the shell shows the import prompt.
func (m Model) jbImportPromptOpen() bool { return m.jbImportOpen && m.shell.IsOpen() }

// renderJBImportPrompt (re)fills the shell with the prompt for the current
// input; candidates (from the last tab press) render underneath.
func (m *Model) renderJBImportPrompt(candidates []string) {
	m.renderCompletionPrompt(m.jbImportInput, m.jbImportPos, candidates,
		"Import JetBrains keymap XML",
		"tab complete · enter import · esc cancel")
}

// updateJBImportPrompt consumes every key while the import prompt is open:
// tab completes the path, everything else is shared line editing via
// ui.EditKey (#2002).
func (m Model) updateJBImportPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	closePrompt := func() {
		m.jbImportOpen = false
		m.jbImportInput = ""
		m.jbImportPos = 0
		m.shell.Close()
	}
	var candidates []string
	switch {
	case msg.Code == tea.KeyEscape:
		closePrompt()
		return m, nil
	case msg.Code == tea.KeyEnter:
		path := strings.TrimSpace(m.jbImportInput)
		closePrompt()
		if path == "" {
			return m, nil
		}
		return m, m.runJBImport(expandHome(path))
	case msg.Code == tea.KeyTab:
		res := pathcomplete.Complete(m.jbImportInput)
		m.jbImportInput = res.Completed
		m.jbImportPos = len([]rune(m.jbImportInput))
		candidates = res.Candidates
	default:
		if out, pos, handled, _ := ui.EditKey(msg, m.jbImportInput, m.jbImportPos); handled {
			m.jbImportInput, m.jbImportPos = out, pos
		}
	}
	m.renderJBImportPrompt(candidates)
	return m, nil
}

// pasteJBImportPrompt inserts a paste into the path input at its cursor
// (#1873).
func (m *Model) pasteJBImportPrompt(text string) bool {
	out, pos, changed := ui.PasteText(m.jbImportInput, m.jbImportPos, text)
	if !changed {
		return false
	}
	m.jbImportInput, m.jbImportPos = out, pos
	m.renderJBImportPrompt(nil)
	return true
}

// expandHome resolves a leading "~" to the user's home directory.
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~"+string(os.PathSeparator)) {
		if home, err := os.UserHomeDir(); err == nil {
			return home + path[1:]
		}
	}
	return path
}

// runJBImport reads and applies the export off the update loop, writing the
// overrides at user scope and reloading the config through the normal pipeline.
func (m *Model) runJBImport(path string) tea.Cmd {
	opts := m.cfgOpts
	return func() tea.Msg {
		f, err := os.Open(path)
		if err != nil {
			return jbImportDoneMsg{err: err}
		}
		defer f.Close()
		res, err := jbimport.Apply(f, keymap.Defaults(configuredPreset()), func(key, value string) error {
			return config.WriteKey(opts, config.UserScope, key, value)
		})
		if err != nil {
			return jbImportDoneMsg{err: err}
		}
		c, diags := config.Load(opts)
		return jbImportDoneMsg{
			summary: res.Summary(),
			reload:  config.ConfigReloadedMsg{Config: c, Diags: diags},
		}
	}
}

// configuredPreset names the active binding preset, defaulting to JetBrains.
func configuredPreset() string {
	if c := config.Get(); c != nil {
		if p := strings.TrimSpace(c.Keymap.Preset); p != "" {
			return p
		}
	}
	return keymap.PresetJetBrains
}

// finishJBImport handles the import outcome: toast the summary (or error) and
// hand the produced config reload to the normal pipeline.
func (m *Model) finishJBImport(msg jbImportDoneMsg) tea.Cmd {
	if msg.err != nil {
		m.host.Notify(host.Error, "keymap import: "+msg.err.Error())
		return nil
	}
	m.host.Notify(host.Info, "keymap import: "+msg.summary)
	if msg.reload == nil {
		return nil
	}
	reload := msg.reload
	return func() tea.Msg { return reload }
}
