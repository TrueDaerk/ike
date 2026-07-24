package menu

// Defaults is the built-in menu content (spec #90). Entries reference registry
// command ids; ids that are not registered yet (future roadmaps, blocked
// ledger) render disabled with their dependency hint until they land.
func Defaults() []Menu {
	return []Menu{
		{Title: "File", Items: []Item{
			{Title: "New Scratch File", Command: "scratch.new"},
			{Title: "Open Scratch File…", Command: "scratch.list"},
			{Title: "Open File…", Command: "file.openPath"},
			{Title: "Save", Command: "editor.write"},
			{Title: "Save All", Command: "editor.saveAll"},
			{Title: "Close Tab", Command: "editor.closeTab"},
			{Title: "Reopen Closed Tab", Command: "editor.tab.reopenClosed"},
			{Title: "Switch Project", Command: "project.switch"},
		}},
		{Title: "Edit", Items: []Item{
			{Title: "Undo", Command: "editor.undo"},
			{Title: "Redo", Command: "editor.redo"},
			{Title: "Copy", Command: "editor.copy"},
			{Title: "Cut", Command: "editor.cut"},
			{Title: "Paste", Command: "editor.paste"},
			{Title: "Paste from History", Command: "editor.pasteFromHistory"},
			{Title: "Duplicate Line", Command: "editor.duplicateLine"},
			{Title: "Find in File", Command: "editor.find"},
		}},
		{Title: "View", Items: []Item{
			{Title: "Focus Explorer / Editor", Command: "explorer.toggle"},
			{Title: "Split View Right", Command: "editor.splitViewRight"},
			{Title: "Split View Down", Command: "editor.splitViewDown"},
			{Title: "Maximize Pane", Command: "pane.maximize"},
			{Title: "Zen Mode", Command: "view.zenMode"},
			{Title: "Hide All Tool Windows", Command: "window.hideAllTools"},
		}},
		{Title: "Navigate", Items: []Item{
			{Title: "Go to File", Command: "project.goToFile"},
			{Title: "Recent Files", Command: "palette.recentFiles"},
			{Title: "Pinned Files", Command: "nav.pins"},
			{Title: "Go to Declaration", Command: "lsp.definition"},
			{Title: "Back", Command: "nav.back"},
			{Title: "Forward", Command: "nav.forward"},
		}},
		{Title: "Run", Items: []Item{
			{Title: "Run File", Command: "run.file"},
			{Title: "Rerun Last", Command: "run.rerun"},
			{Title: "Run Test at Cursor", Command: "run.testAtCursor"},
			{Title: "Run Tests in File", Command: "run.testsInFile"},
			{Title: "Toggle Breakpoint", Command: "debug.toggleBreakpoint"},
			{Title: "Debug File", Command: "debug.start"},
			{Title: "Listen for PHP Debug Connections", Command: "debug.listen"},
			{Title: "Step Over", Command: "debug.stepOver"},
			{Title: "Step Into", Command: "debug.stepInto"},
			{Title: "Step Out", Command: "debug.stepOut"},
			{Title: "Continue", Command: "debug.continue"},
			{Title: "Stop Debug Session", Command: "debug.stop"},
		}},
		{Title: "Tools", Items: []Item{
			{Title: "Problems", Command: "problems.toggle"},
			{Title: "Terminal", Command: "terminal.toggle"},
			{Title: "New Terminal", Command: "terminal.new"},
			{Title: "New Terminal Tab", Command: "terminal.newTab"},
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
