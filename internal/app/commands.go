package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/registry"
)

// CloseTabMsg asks the root model to close the focused editor pane, the same
// behavior as the hardcoded ctrl+w / the editor's :q. Dispatched by the
// editor.closeTab command.
type CloseTabMsg struct{}

// appCommands is the compile-in plugin exposing root-model actions as registry
// commands, so the default keybindings (Roadmap 0080/0081) and the palette can
// drive them; the root model owns the behavior, this file only names it.
type appCommands struct{}

func (appCommands) ID() string { return "app" }

// appCommand builds a registry Command that dispatches msg back into the root
// model's Update, mirroring the editor's action() bridge.
func appCommand(id, title string, msg tea.Msg) plugin.Command {
	return plugin.Command{
		ID:    id,
		Title: title,
		Scope: plugin.GlobalScope(),
		Run: func(h host.API) tea.Cmd {
			return h.Dispatch(msg)
		},
	}
}

func (appCommands) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{
		Commands: []plugin.Command{
			appCommand("editor.closeTab", "Close Tab", CloseTabMsg{}),
		},
	}
}

func init() { registry.Register(appCommands{}) }
