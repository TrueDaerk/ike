package app

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/ui"
)

// saveas.go is the app side of saving an untitled buffer (#730): an empty
// editor pane (e.g. created by a split) is a typable untitled buffer, and its
// first save has no path to write to. The editor's saveGuarded emits
// SaveAsPromptMsg instead of failing; the shell prompts for a path (the
// rename-prompt pattern, #175, with the shared single-line editing from
// #763), and accepting saves the buffer, binds the tab to the new file and
// runs the same wiring an openPath would (watcher, MRU, explorer, highlight,
// file-opened hooks).

// startSaveAsPrompt opens the path prompt for the focused pane's untitled
// buffer. closeAfter carries the ":wq" intent through the prompt.
func (m *Model) startSaveAsPrompt(closeAfter bool) {
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindEditor {
		return
	}
	if ed := inst.Editor(); ed == nil || ed.HasFile() {
		return
	}
	m.saveAsKey = key
	m.saveAsClose = closeAfter
	m.saveAsInput = ""
	m.saveAsPos = 0
	m.saveAsErr = ""
	m.renderSaveAsPrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// saveAsOpen reports whether the shell currently shows the save-as prompt.
func (m Model) saveAsOpen() bool { return m.saveAsKey != "" && m.shell.IsOpen() }

// renderSaveAsPrompt (re)fills the shell for the current input; called on
// open and after every accepted key.
func (m *Model) renderSaveAsPrompt() {
	line := "> " + ui.CursorView(m.saveAsInput, m.saveAsPos)
	errLine := ""
	if m.saveAsErr != "" {
		errLine = "\nE: " + m.saveAsErr
	}
	m.shell.SetContent(ui.ModelContent{
		Heading: "Save untitled buffer — path relative to " + displayPath(m.explorer().Root()),
		Body: func() string {
			return line + errLine + "\n\nenter save · esc cancel"
		},
	})
}

// updateSaveAsPrompt consumes every key while the prompt is open: enter
// saves and binds, esc cancels, everything else is line editing.
func (m Model) updateSaveAsPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	closePrompt := func() {
		m.saveAsKey = ""
		m.saveAsInput = ""
		m.saveAsPos = 0
		m.saveAsClose = false
		m.saveAsErr = ""
		m.shell.Close()
	}
	switch {
	case msg.Code == tea.KeyEscape:
		closePrompt()
		return m, nil
	case msg.Code == tea.KeyEnter:
		name := strings.TrimSpace(m.saveAsInput)
		if name == "" {
			return m, nil
		}
		key, closeAfter := m.saveAsKey, m.saveAsClose
		path := filepath.FromSlash(name)
		if !filepath.IsAbs(path) {
			path = filepath.Join(m.explorer().Root(), path)
		}
		path = filepath.Clean(path)
		if _, err := os.Stat(path); err == nil {
			// Never silently clobber an existing file from a fresh buffer;
			// the prompt stays open for a different name.
			m.saveAsErr = "file exists: " + displayPath(path)
			m.renderSaveAsPrompt()
			return m, nil
		}
		closePrompt()
		cmd := m.bindUntitled(key, path)
		if cmd == nil {
			return m, nil
		}
		if closeAfter {
			return m, tea.Sequence(cmd, func() tea.Msg { return editor.CloseMsg{} })
		}
		return m, cmd
	}
	if out, pos, handled, _ := ui.EditKey(msg, m.saveAsInput, m.saveAsPos); handled {
		m.saveAsInput, m.saveAsPos = out, pos
		m.renderSaveAsPrompt()
	}
	return m, nil
}

// bindUntitled writes the untitled buffer in pane key to path and turns the
// tab into a regular file tab: the save goes through editor.SaveTo (EventSave,
// undo checkpoint), then the openPath wiring runs — watcher tracking, MRU,
// explorer active-file, layout persistence, highlighting and the file-opened
// hooks — so the fresh file behaves exactly like one opened from disk.
func (m *Model) bindUntitled(key, path string) tea.Cmd {
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindEditor {
		return nil
	}
	ed := inst.Editor()
	if ed == nil || ed.HasFile() {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		m.host.Notify(host.Warn, "save failed: "+err.Error())
		return nil
	}
	m.watcher.MarkSaved(path) // the write must not echo back as external
	if err := ed.SaveTo(path); err != nil {
		return nil // the ex line already shows the error
	}
	var cmds []tea.Cmd
	m.recent.Touch(path)
	m.watcher.Track(path)
	m.explorer().SetActive(path)
	m.syncExplorerOpen()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	cmds = append(cmds, ed.Reparse())
	cmds = append(cmds, m.vcsMarksCmd(ed))
	cmds = append(cmds, m.fireHooks(plugin.EventFileOpened, path)...)
	return tea.Batch(cmds...)
}
