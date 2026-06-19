// Package example is the reference IKE plugin. It exercises every extension
// point — Command, Keymap, Pane, FileHandler, Hook — and self-registers via
// init(). Blank-import it from cmd/ike/main.go to enable it:
//
//	import _ "ike/plugins/example"
package example

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/registry"
)

func init() { registry.Register(Plugin{}) }

// Plugin is the reference plugin.
type Plugin struct{}

// ID implements plugin.Plugin.
func (Plugin) ID() string { return "example" }

// Capabilities implements plugin.Plugin, contributing one of every extension.
func (Plugin) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{
		Commands: []plugin.Command{{
			ID:    "example.hello",
			Title: "Example: Say Hello",
			Scope: plugin.GlobalScope(),
			Run: func(h host.API) tea.Cmd {
				h.SetStatus("hello from the example plugin")
				return nil
			},
		}},
		Keymaps: []plugin.Keymap{{
			Keys:     "ctrl+e",
			Scope:    plugin.GlobalScope(),
			Priority: plugin.CorePriority + 1, // explicitly overrides core
			Action: func(h host.API) tea.Cmd {
				return h.Dispatch(GreetMsg{})
			},
		}},
		Panes: []plugin.Pane{{
			ID:        "example.panel",
			Title:     "Example Panel",
			ContextID: "example.panel",
			New:       func(h host.API) tea.Model { return panel{} },
		}},
		FileHandlers: []plugin.FileHandler{{
			ID:         "example.example-files",
			Extensions: []string{".example"},
			Match: func(_ string, head []byte) bool {
				return len(head) >= 7 && string(head[:7]) == "EXAMPLE"
			},
			Open: func(h host.API, path string) tea.Cmd {
				h.SetStatus("example handler opened " + path)
				return nil
			},
		}},
		Hooks: []plugin.Hook{{
			ID:    "example.on-open",
			Event: plugin.EventFileOpened,
			Notify: func(h host.API, payload any) tea.Cmd {
				if p, ok := payload.(string); ok {
					h.SetStatus("example saw open: " + p)
				}
				return nil
			},
		}},
	}
}

// GreetMsg is emitted by the example keymap action.
type GreetMsg struct{}

// panel is the example pane model. It advertises its context id so
// context-scoped commands resolve when it is focused.
type panel struct{}

func (panel) Init() tea.Cmd                         { return nil }
func (p panel) Update(tea.Msg) (tea.Model, tea.Cmd) { return p, nil }
func (panel) View() string                          { return lipgloss.NewStyle().Render("example panel") }
func (panel) ContextID() string                     { return "example.panel" }
