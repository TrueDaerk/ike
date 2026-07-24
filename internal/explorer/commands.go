// commands.go contributes the explorer's user-facing actions as registry
// Commands with default Keymaps. Each action only dispatches an explorer Msg;
// the root model routes it back into the explorer's Update. The canonical
// JetBrains binding set is owned by Roadmap 0080 — here we expose commands and
// ship sensible defaults.
package explorer

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/registry"
)

// ctxID is the focus context the explorer pane advertises; commands and keymaps
// are scoped to it so they only fire while the tree is focused.
const ctxID = "explorer"

func init() { registry.Register(corePlugin{}) }

// corePlugin contributes the explorer's built-in commands and keymaps.
type corePlugin struct{}

func (corePlugin) ID() string { return "explorer" }

func (corePlugin) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{
		Commands: []plugin.Command{
			cmd("explorer.toggleHidden", "Explorer: Toggle Hidden Files", ToggleHiddenMsg{}),
			cmd("explorer.refresh", "Explorer: Refresh", RefreshMsg{}),
			cmd("explorer.collapseAll", "Explorer: Collapse All", CollapseAllMsg{}),
			cmd("explorer.expandAll", "Explorer: Expand All", ExpandAllMsg{}),
			cmd("explorer.reveal", "Explorer: Reveal Open File", RevealMsg{}),
			cmd("explorer.newFile", "Explorer: New File", NewFileMsg{}),
			cmd("explorer.newFolder", "Explorer: New Folder", NewDirMsg{}),
			cmd("explorer.delete", "Explorer: Delete", DeleteMsg{}),
			cmd("explorer.rename", "Explorer: Rename", RenameMsg{}),
			cmd("explorer.search", "Explorer: Speed Search", SearchMsg{}),
			cmd("explorer.undo", "Explorer: Undo File Operation", UndoMsg{}),
			cmd("explorer.redo", "Explorer: Redo File Operation", RedoMsg{}),
			// Tree navigation as rebindable commands (#1041); the raw keys in
			// Update remain the zero-config fallback, vim doc hints included.
			cmdHint("explorer.cursorDown", "Explorer: Cursor Down", "j / down", CursorMoveMsg{Delta: 1}),
			cmdHint("explorer.cursorUp", "Explorer: Cursor Up", "k / up", CursorMoveMsg{Delta: -1}),
			cmdHint("explorer.top", "Explorer: Jump to Top", "gg", CursorTopMsg{}),
			cmdHint("explorer.bottom", "Explorer: Jump to Bottom", "G", CursorBottomMsg{}),
			cmdHint("explorer.pageDown", "Explorer: Page Down", "pgdn / ctrl+d", CursorPageMsg{Dir: 1}),
			cmdHint("explorer.pageUp", "Explorer: Page Up", "pgup / ctrl+u", CursorPageMsg{Dir: -1}),
			cmdHint("explorer.open", "Explorer: Open / Toggle", "enter", ActivateMsg{}),
			cmdHint("explorer.expandOrOpen", "Explorer: Expand or Open", "l / right", ExpandOrOpenMsg{}),
			cmdHint("explorer.collapseOrParent", "Explorer: Collapse or Go to Parent", "h / left", CollapseOrParentMsg{}),
			cmdHint("explorer.openInSplit", "Explorer: Open in Split", "o", OpenInSplitMsg{}),
		},
		Keymaps: []plugin.Keymap{
			keymap(".", "explorer.toggleHidden", ToggleHiddenMsg{}),
			keymap("r", "explorer.refresh", RefreshMsg{}),
			keymap("c", "explorer.collapseAll", CollapseAllMsg{}),
			keymap("C", "explorer.expandAll", ExpandAllMsg{}),
			keymap("a", "explorer.newFile", NewFileMsg{}),
			keymap("A", "explorer.newFolder", NewDirMsg{}),
			keymap("d", "explorer.delete", DeleteMsg{}),
			keymap("R", "explorer.rename", RenameMsg{}),
			keymap("/", "explorer.search", SearchMsg{}),
		},
	}
}

// cmdHint is cmd with a documentation shortcut for the cheatsheet (#1041):
// the raw tree keys are handled in the explorer's Update fallback, so they
// surface as doc hints like the editor's vim keys.
func cmdHint(id, title, hint string, msg tea.Msg) plugin.Command {
	c := cmd(id, title, msg)
	c.Shortcut = hint
	return c
}

// cmd builds an explorer Command that dispatches msg when invoked.
func cmd(id, title string, msg tea.Msg) plugin.Command {
	return plugin.Command{
		ID:    id,
		Title: title,
		Scope: plugin.PaneScope(ctxID),
		Run:   func(h host.API) tea.Cmd { return h.Dispatch(msg) },
	}
}

// keymap builds a default, explorer-scoped Keymap that dispatches msg and links
// to cmdID so the help sheet can show the shortcut.
func keymap(keys, cmdID string, msg tea.Msg) plugin.Keymap {
	return plugin.Keymap{
		Keys:      keys,
		Scope:     plugin.PaneScope(ctxID),
		CommandID: cmdID,
		Priority:  plugin.CorePriority,
		Action:    func(h host.API) tea.Cmd { return h.Dispatch(msg) },
	}
}
