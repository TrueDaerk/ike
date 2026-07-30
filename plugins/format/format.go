// Package format is the compile-in plugin owning the reformat commands
// (Roadmap 0470, #1401). The command ids stay `lsp.format` / `lsp.formatRange`
// so existing keymaps (`cmd+alt+l`, user bindings, JetBrains imports) keep
// working, but the titles are language-neutral and the implementation is the
// formatter registry (internal/format): config override → external command →
// LSP → built-in, resolved per buffer by the app's handler. It self-registers
// via init() and is wired into the build by a blank import in cmd/ike/main.go.
package format

import (
	tea "charm.land/bubbletea/v2"

	iformat "ike/internal/format"
	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/registry"
)

func init() {
	registry.Register(Plugin{})
}

// Plugin provides the reformat commands.
type Plugin struct{}

// ID implements plugin.Plugin.
func (Plugin) ID() string { return "format" }

// Capabilities registers the reformat commands. They dispatch request
// messages the app answers — only the app holds the active buffer, its
// effective per-buffer options and the view to apply edits to.
func (Plugin) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{
		Commands: []plugin.Command{
			{
				ID:    "lsp.format",
				Title: "Reformat File",
				Scope: plugin.PaneScope("editor"),
				Run:   func(h host.API) tea.Cmd { return h.Dispatch(iformat.FileRequestMsg{}) },
			},
			{
				ID:    "lsp.formatRange",
				Title: "Reformat Selection",
				Scope: plugin.PaneScope("editor"),
				Run:   func(h host.API) tea.Cmd { return h.Dispatch(iformat.RangeRequestMsg{}) },
			},
		},
	}
}
