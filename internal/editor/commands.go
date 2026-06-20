package editor

import (
	tea "github.com/charmbracelet/bubbletea"

	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/registry"
)

// ContextID is advertised by the editor pane for context-scoped command/keymap
// resolution (it matches the root model's editor context).
const ContextID = "editor"

// commands.go is the single bridge between editor actions / ex-commands and the
// plugin registry. Each registered Command dispatches an ActionMsg, which the
// root model routes back into the focused editor's Update. The palette (07) and
// keybindings (08) invoke these by id — the editor grows no parallel dispatch.

// action builds a registry Command that runs a named editor action via ActionMsg.
func action(id, title, name string) plugin.Command {
	return plugin.Command{
		ID:    id,
		Title: title,
		Scope: plugin.PaneScope(ContextID),
		Run: func(h host.API) tea.Cmd {
			return h.Dispatch(ActionMsg{Action: name})
		},
	}
}

// editorPlugin contributes the editor's actions and ex-commands as registry
// Commands. It is compiled in and self-registers below.
type editorPlugin struct{}

// ID implements plugin.Plugin.
func (editorPlugin) ID() string { return "editor" }

// Capabilities implements plugin.Plugin.
func (editorPlugin) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{
		Commands: []plugin.Command{
			action("editor.write", "Save File", "write"),
			action("editor.quit", "Close Editor", "quit"),
			action("editor.write_quit", "Save and Close", "write_quit"),
			action("editor.undo", "Undo", "undo"),
			action("editor.redo", "Redo", "redo"),
		},
	}
}

func init() { registry.Register(editorPlugin{}) }
