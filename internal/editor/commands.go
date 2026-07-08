package editor

import (
	tea "charm.land/bubbletea/v2"

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
// shortcut is the documentation hint shown in the help sheet: the editor's vim
// keys (ex-commands like ":w", modal keys like "u") are handled directly in the
// editor, not through the keymap layer, so they are surfaced as doc hints.
func action(id, title, name, shortcut string) plugin.Command {
	return plugin.Command{
		ID:       id,
		Title:    title,
		Scope:    plugin.PaneScope(ContextID),
		Shortcut: shortcut,
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
			action("editor.write", "Save File", "write", ":w"),
			action("editor.quit", "Close Editor", "quit", ":q"),
			action("editor.write_quit", "Save and Close", "write_quit", ":wq"),
			action("editor.undo", "Undo", "undo", "u"),
			action("editor.redo", "Redo", "redo", "ctrl+r"),
			action("editor.copy", "Copy", "copy", "y"),
			action("editor.cut", "Cut", "cut", "d"),
			action("editor.paste", "Paste", "paste", "p"),
			action("editor.lineStart", "Move to Line Start", "line_start", "0"),
			action("editor.lineEnd", "Move to Line End", "line_end", "$"),
			action("editor.find", "Find in File", "find", "/"),
			action("editor.duplicateLine", "Duplicate Line", "duplicate_line", ""),
			action("editor.commentLine", "Toggle Line Comment", "comment_line", ""),
		},
	}
}

func init() { registry.Register(editorPlugin{}) }
