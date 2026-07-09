package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/ui"
)

// conflict.go is the save-conflict guard UI (Roadmap 0140, #82). When a stale
// editor buffer (its file changed on disk while it held unsaved edits) is
// saved, the editor yields a ConflictMsg instead of writing; the root model
// answers it with a floating prompt: keep mine / reload / cancel. 'show diff'
// joins the choices once the diff viewer (#60) lands.

// openConflictPrompt shows the prompt for the editor pane owning path. The
// conflicted tab is activated so the prompt's answers act on the document it
// names (a save from a background tab can raise the conflict too).
func (m *Model) openConflictPrompt(path string) {
	key := m.editorKeyForPath(path)
	if key == "" {
		return
	}
	inst := m.panes.Get(key)
	if idx := inst.TabForPath(path); idx >= 0 {
		inst.ActivateTab(idx)
	}
	m.conflictKey = key
	m.shell.SetContent(ui.ModelContent{
		Heading: "File changed on disk",
		Body: func() string {
			return displayPath(path) + " changed on disk while you have unsaved edits.\n\n" +
				"  [k]   keep mine — overwrite the external change\n" +
				"  [r]   reload — discard my edits\n" +
				"  [esc] cancel — decide later (the buffer stays marked)"
		},
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// updateConflict consumes every key while the conflict prompt is open. Keys
// other than the three answers are swallowed so nothing leaks past a modal
// decision.
func (m Model) updateConflict(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	inst := m.panes.Get(m.conflictKey)
	if inst == nil || inst.Kind() != pane.KindEditor {
		m.conflictKey = ""
		m.shell.Close()
		return m, nil
	}
	switch msg.String() {
	case "k":
		inst.Editor().ResolveConflictKeepMine()
		m.conflictKey = ""
		m.shell.Close()
		m.host.Notify(host.Info, "kept your version: "+displayPath(inst.Editor().Path()))
		return m, nil
	case "r":
		// Local history (#35), once it lands, snapshots the buffer here before
		// the edits are discarded.
		cmd := inst.Editor().ResolveConflictReload()
		m.conflictKey = ""
		m.shell.Close()
		m.host.Notify(host.Info, "reloaded from disk: "+displayPath(inst.Editor().Path()))
		return m, cmd
	case "esc":
		m.conflictKey = ""
		m.shell.Close()
		return m, nil
	}
	return m, nil
}

// conflictOpen reports whether the shell currently shows the conflict prompt.
func (m Model) conflictOpen() bool { return m.conflictKey != "" && m.shell.IsOpen() }
