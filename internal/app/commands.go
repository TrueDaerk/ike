package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/plugin"
	"ike/internal/registry"
)

// CloseTabMsg asks the root model to close the focused editor pane, the same
// behavior as the hardcoded ctrl+w / the editor's :q. Dispatched by the
// editor.closeTab command.
type CloseTabMsg struct{}

// ShowKeymapHelpMsg asks the root model to open the keymap cheatsheet overlay,
// the same view the hardcoded "?" opens. Dispatched by palette.keymapHelp.
type ShowKeymapHelpMsg struct{}

// CyclePaneFocusMsg asks the root model to move focus to the next pane, the
// same behavior as the hardcoded tab. Dispatched by pane.switcher.
type CyclePaneFocusMsg struct{}

// GoToFileMsg asks the root model to open the palette locked to the fuzzy file
// mode ("@"), from any context. Dispatched by project.goToFile.
type GoToFileMsg struct{}

// SaveAllMsg asks the root model to save every dirty editor pane. Dispatched
// by editor.saveAll.
type SaveAllMsg struct{}

// SplitFocusedMsg asks the root model to split the focused leaf toward Zone
// with a fresh empty editor (#114). Dispatched by pane.splitDown / pane.splitUp.
type SplitFocusedMsg struct{ Zone layout.Zone }

// OpenSettingsMsg asks the root model to open the settings panel (Roadmap
// 0160). Dispatched by settings.open (cmd+, / menu bar / palette).
type OpenSettingsMsg struct{}

// ToggleMenuMsg asks the root model to open (or close) the menu bar's first
// dropdown (Roadmap 0160). Dispatched by menu.open (f10).
type ToggleMenuMsg struct{}

// ShowNotificationHistoryMsg asks the root model to open the notification
// history list in the floating shell (Roadmap 0130). Dispatched by
// notifications.history.
type ShowNotificationHistoryMsg struct{}

// OpenFindInPathMsg asks the root model to open the find-in-path overlay
// (Roadmap 0150). Dispatched by project.findInPath (cmd+shift+f / palette).
type OpenFindInPathMsg struct{}

// OpenReplaceInPathMsg asks the root model to open the find-in-path overlay
// in replace mode (Roadmap 0150, #86). Dispatched by project.replaceInPath
// (cmd+shift+r / palette).
type OpenReplaceInPathMsg struct{}

// MatchStepMsg asks the root model to jump to the next (Delta 1) or previous
// (Delta -1) retained find-in-path match, without the overlay open.
// Dispatched by search.nextMatch / search.prevMatch.
type MatchStepMsg struct{ Delta int }

// ToggleExplorerFocusMsg asks the root model to move focus to the explorer, or
// back to the active editor when the explorer already holds focus (the
// terminal approximation of JetBrains' Cmd+1 tool-window toggle). Dispatched
// by explorer.toggle.
type ToggleExplorerFocusMsg struct{}

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
			appCommand("palette.keymapHelp", "Keymap Cheatsheet", ShowKeymapHelpMsg{}),
			appCommand("pane.switcher", "Switch Pane Focus", CyclePaneFocusMsg{}),
			appCommand("project.goToFile", "Go to File", GoToFileMsg{}),
			appCommand("project.findInPath", "Find in Path", OpenFindInPathMsg{}),
			appCommand("project.replaceInPath", "Replace in Path", OpenReplaceInPathMsg{}),
			appCommand("search.nextMatch", "Next Search Match", MatchStepMsg{Delta: 1}),
			appCommand("search.prevMatch", "Previous Search Match", MatchStepMsg{Delta: -1}),
			appCommand("editor.saveAll", "Save All", SaveAllMsg{}),
			appCommand("explorer.toggle", "Focus Explorer / Editor", ToggleExplorerFocusMsg{}),
			appCommand("notifications.history", "Notification History", ShowNotificationHistoryMsg{}),
			appCommand("menu.open", "Open Menu Bar", ToggleMenuMsg{}),
			appCommand("settings.open", "Settings", OpenSettingsMsg{}),
			appCommand("pane.splitDown", "Split Down", SplitFocusedMsg{Zone: layout.ZoneBottom}),
			appCommand("pane.splitUp", "Split Up", SplitFocusedMsg{Zone: layout.ZoneTop}),
			appCommand("pane.splitRight", "Split Right", SplitFocusedMsg{Zone: layout.ZoneRight}),
			appCommand("pane.splitLeft", "Split Left", SplitFocusedMsg{Zone: layout.ZoneLeft}),
		},
	}
}

func init() { registry.Register(appCommands{}) }
