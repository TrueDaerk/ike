package menu

// Defaults is the built-in menu content (spec #90). Entries reference registry
// command ids; ids that are not registered yet (future roadmaps, blocked
// ledger) render disabled with their dependency hint until they land.
func Defaults() []Menu {
	return []Menu{
		{Title: "File", Items: []Item{
			{Title: "Save", Command: "editor.write"},
			{Title: "Save All", Command: "editor.saveAll"},
			{Title: "Close Tab", Command: "editor.closeTab"},
			{Title: "Switch Project", Command: "project.switch"},
		}},
		{Title: "Edit", Items: []Item{
			{Title: "Undo", Command: "editor.undo"},
			{Title: "Redo", Command: "editor.redo"},
			{Title: "Copy", Command: "editor.copy"},
			{Title: "Cut", Command: "editor.cut"},
			{Title: "Paste", Command: "editor.paste"},
			{Title: "Duplicate Line", Command: "editor.duplicateLine"},
			{Title: "Find in File", Command: "editor.find"},
		}},
		{Title: "View", Items: []Item{
			{Title: "Focus Explorer / Editor", Command: "explorer.toggle"},
			{Title: "Zen Mode", Command: "view.zenMode"},
		}},
		{Title: "Navigate", Items: []Item{
			{Title: "Go to File", Command: "project.goToFile"},
			{Title: "Go to Declaration", Command: "lsp.definition"},
			{Title: "Back", Command: "nav.back"},
			{Title: "Forward", Command: "nav.forward"},
		}},
		{Title: "Tools", Items: []Item{
			{Title: "Terminal", Command: "tools.terminal"},
			{Title: "Restart Language Servers", Command: "lsp.restart"},
			{Title: "Plugins", Command: "tools.plugins"},
		}},
		{Title: "Settings", Items: []Item{
			{Title: "Settings…", Command: "settings.open"},
			{Title: "Keymap Cheatsheet", Command: "palette.keymapHelp"},
		}},
		{Title: "Help", Items: []Item{
			{Title: "Commands & Shortcuts", Command: "palette.keymapHelp"},
			{Title: "Notification History", Command: "notifications.history"},
		}},
	}
}
