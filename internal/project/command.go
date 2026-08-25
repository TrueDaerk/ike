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

// OpenPeekPickerMsg asks the root model to open the picker in its peek
// flavour (#2136): plain activation peeks the selected project instead of
// switching. Dispatched by project.peek.
type OpenPeekPickerMsg struct{}

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
			// Quick-peek (#2136): the picker where activation opens the
			// selected project temporarily; project.peek.return switches back
			// and unloads it again. No default chord — the normal picker
			// (cmd+shift+p) peeks any row via alt+enter, so a second picker
			// chord would spend budget (#711) on a duplicate entry point.
			ID:    "project.peek",
			Title: "Peek Project…",
			Scope: plugin.GlobalScope(),
			Run:   func(h host.API) tea.Cmd { return h.Dispatch(OpenPeekPickerMsg{}) },
		}, {
			// The one-key way back from a peek (#2136): return to the origin
			// project and drop the peeked workspace. Default chord cmd+shift+b
			// ("back"); free in the 0080 matrix and cmd-based per the darwin
			// keymap constraints.
			ID:    "project.peek.return",
			Title: "Return From Peek",
			Scope: plugin.GlobalScope(),
			Run:   func(h host.API) tea.Cmd { return h.Dispatch(PeekReturnMsg{}) },
		}, {
			// Escalation (#2136): stay in the peeked project for real — record
			// the open into project.history and clear the peek marker. No
			// default chord: a deliberate, rare decision (palette-driven).
			ID:    "project.peek.keep",
			Title: "Keep Peeked Project",
			Scope: plugin.GlobalScope(),
			Run:   func(h host.API) tea.Cmd { return h.Dispatch(PeekKeepMsg{}) },
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
			Keys:      "cmd+shift+b",
			Scope:     plugin.GlobalScope(),
			CommandID: "project.peek.return",
			Priority:  plugin.CorePriority,
			Action:    func(h host.API) tea.Cmd { return h.Dispatch(PeekReturnMsg{}) },
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
