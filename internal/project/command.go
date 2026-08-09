package project

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/registry"
)

// command.go registers the `project.switch` command (Roadmap 0090, #12): a
// compile-in plugin contributing the command plus a default Keymap slot. The
// command only dispatches OpenPickerMsg — the root model opens the picker and
// routes the selection; this package never mutates panes directly.

// OpenPickerMsg asks the root model to open the project picker: the palette
// locked to the recent-projects mode (picker.go). Dispatched by project.switch.
type OpenPickerMsg struct{}

// commands is the compile-in plugin exposing the project-switching entry point.
type commands struct{}

func (commands) ID() string { return "project" }

func (commands) Capabilities() plugin.Capabilities {
	open := func(h host.API) tea.Cmd { return h.Dispatch(OpenPickerMsg{}) }
	return plugin.Capabilities{
		Commands: []plugin.Command{{
			ID:    "project.switch",
			Title: "Switch Project…",
			Scope: plugin.GlobalScope(),
			Run:   open,
		}, {
			// The root model resumes the MRU background workspace, or quits
			// when none is open (#1355). Default chord cmd+shift+w (#1358).
			ID:    "project.close",
			Title: "Close Project",
			Scope: plugin.GlobalScope(),
			Run:   func(h host.API) tea.Cmd { return h.Dispatch(CloseProjectMsg{}) },
		}, {
			// No default chord: cloning is a rare, dialog-driven action
			// (palette / File menu), and the chord budget is full (#711).
			ID:    "project.clone",
			Title: "Clone Repository…",
			Scope: plugin.GlobalScope(),
			Run:   func(h host.API) tea.Cmd { return h.Dispatch(OpenCloneMsg{}) },
		}, {
			// No default chord for the same reason (#711): creating a project
			// is a rare, dialog-driven action (palette / File menu, #1718).
			ID:    "project.new",
			Title: "New Project…",
			Scope: plugin.GlobalScope(),
			Run:   func(h host.API) tea.Cmd { return h.Dispatch(OpenNewProjectMsg{}) },
		}},
		// Default binding slot only — the canonical chord is owned by Roadmap
		// 0080/0081. cmd+shift+p mirrors JetBrains' Recent Projects popup
		// (macOS keymap export); ctrl+shift+p is the delivered secondary.
		Keymaps: []plugin.Keymap{{
			Keys:      "cmd+shift+p",
			Scope:     plugin.GlobalScope(),
			CommandID: "project.switch",
			Priority:  plugin.CorePriority,
			Action:    open,
		}, {
			Keys:      "cmd+shift+w",
			Scope:     plugin.GlobalScope(),
			CommandID: "project.close",
			Priority:  plugin.CorePriority,
			Action:    func(h host.API) tea.Cmd { return h.Dispatch(CloseProjectMsg{}) },
		}},
	}
}

func init() { registry.Register(commands{}) }
