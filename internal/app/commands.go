package app

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/settings"
)

// CloseTabMsg asks the root model to close the focused editor pane's active
// tab — the pane itself only when its last tab goes (#156) — the same behavior
// as the hardcoded ctrl+w / the editor's :q. Dispatched by the editor.closeTab
// command.
type CloseTabMsg struct{}

// TabStepMsg cycles the active editor pane's tabs by Delta, wrapping around
// (0190, #158). Dispatched by editor.tab.next / editor.tab.prev.
type TabStepMsg struct{ Delta int }

// TabSelectMsg activates the active editor pane's tab at Index (0-based); out
// of range is a no-op. Dispatched by editor.tab.select1 … editor.tab.select9.
type TabSelectMsg struct{ Index int }

// TabMoveMsg reorders the active tab by Delta positions within its pane.
// Dispatched by editor.tab.moveLeft / editor.tab.moveRight.
type TabMoveMsg struct{ Delta int }

// TabReopenMsg reopens the most recently closed tab, restoring its file and
// cursor from the closed-tab ring. Dispatched by editor.tab.reopenClosed.
type TabReopenMsg struct{}

// TabCloseOthersMsg closes every tab of the active editor pane except the
// active one (#1128); tabs with unsaved changes stay open. Dispatched by
// editor.tab.closeOthers (tab context menu / palette).
type TabCloseOthersMsg struct{}

// NewEditorTabMsg appends a fresh empty editor tab to the focused (else the
// active) editor pane and focuses it (#1794, the editor half of the
// per-context ctrl+t pair). Dispatched by editor.tab.new.
type NewEditorTabMsg struct{}

// TabTogglePinMsg flips the active tab's pin (#1172): a pinned tab is exempt
// from the tab-limit LRU eviction and from Close Others; manual closes stay
// allowed. Dispatched by editor.tab.togglePin (tab context menu / palette).
type TabTogglePinMsg struct{}

// ClosePaneMsg closes the focused pane whole — every tab at once — behind the
// unsaved-changes guard (#1128). Dispatched by pane.close (pane-title context
// menu / palette).
type ClosePaneMsg struct{}

// ForceCodeInsightMsg asks the root model to lift the large-file degradation
// (#149) for the focused document: highlighting reparses and the LSP bridge
// didOpens despite the size. Dispatched by editor.forceCodeInsight.
type ForceCodeInsightMsg struct{}

// ShowKeymapHelpMsg asks the root model to open the keymap cheatsheet overlay,
// the same view the hardcoded "?" opens. Dispatched by palette.keymapHelp.
type ShowKeymapHelpMsg struct{}

// CyclePaneFocusMsg asks the root model to move focus to the next pane, the
// same behavior as the hardcoded tab. Dispatched by pane.switcher.
type CyclePaneFocusMsg struct{}

// OpenFilePathMsg asks the root model to open the palette locked to the
// open-path picker (#999): a filesystem browser for absolute/~ paths, so
// files outside the workspace open without switching projects.
type OpenFilePathMsg struct{}

// GoToFileMsg asks the root model to open the palette locked to the fuzzy file
// mode ("@"), from any context. Dispatched by project.goToFile.
type GoToFileMsg struct{}

// ShowRecentFilesMsg asks the root model to open the palette locked to the
// recent-files (MRU) mode (Roadmap 0230). Dispatched by palette.recentFiles
// (cmd+e / menu).
type ShowRecentFilesMsg struct{}

// ShowSearchEverywhereMsg asks the root model to open the palette locked to
// the search-everywhere mode ranking one query across commands and files
// (Roadmap 0230). Dispatched by palette.searchEverywhere (cmd+shift+a /
// double-shift).
type ShowSearchEverywhereMsg struct{}

// SaveAllMsg asks the root model to save every dirty editor pane. Dispatched
// by editor.saveAll.
type SaveAllMsg struct{}

// SplitViewMsg asks the root model to split the focused editor and open the
// same document as a second live shared view (#147), cursor/scroll copied
// from the source view.
type SplitViewMsg struct{ Zone layout.Zone }

// SplitFocusedMsg asks the root model to split the focused leaf toward Zone
// with a fresh empty editor (#114). Dispatched by pane.splitDown / pane.splitUp.
type SplitFocusedMsg struct{ Zone layout.Zone }

// OpenSettingsMsg asks the root model to open the settings panel (Roadmap
// 0160). Dispatched by settings.open (cmd+, / menu bar / palette).
type OpenSettingsMsg struct{}

// OpenPythonEnvWizardMsg opens the settings panel on the Toolchain page with
// the venv creation wizard pushed (#884). Dispatched by python.newEnvironment.
type OpenPythonEnvWizardMsg struct{}

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

// OpenTodoIndexMsg asks the root model to open the TODO/FIXME index overlay
// (#61). Dispatched by todo.list (cmd+k t / palette).
type OpenTodoIndexMsg struct{}

// MatchStepMsg asks the root model to jump to the next (Delta 1) or previous
// (Delta -1) retained find-in-path match, without the overlay open.
// Dispatched by search.nextMatch / search.prevMatch.
type MatchStepMsg struct{ Delta int }

// RenameFileMsg asks the root model to rename a file (#175, shift+f6):
// with the explorer focused it opens the explorer's inline rename prompt on
// the selection; with an editor focused it prompts for the focused file's new
// name. Dispatched by file.rename.
type RenameFileMsg struct{}

// MoveFileMsg asks the root model to move a file into another folder (#175,
// f6): the explorer's selection or the focused editor's file, with the target
// picked from the palette's directory mode. Dispatched by file.move.
type MoveFileMsg struct{}

// TerminalNewMsg asks the root model to open a new integrated terminal pane
// split off the focused leaf (Roadmap 0170, #95). Dispatched by terminal.new.
type TerminalNewMsg struct{}

// TerminalNewTabMsg asks the root model to open a shell in a new tab of the
// active editor pane (#573), next to the file tabs.
type TerminalNewTabMsg struct{}

// RunFileMsg runs the active file through its run configuration (0350, #576).
type RunFileMsg struct{}

// DebugToggleBreakpointMsg flips the breakpoint on the focused editor's
// cursor line (0350, #577).
type DebugToggleBreakpointMsg struct{}

// DebugStartMsg launches the active file's configuration under the debugger
// (0350, #579); DebugStopMsg ends the session. The step messages drive a
// paused session: over (F8), into (F7), out (shift+F8), continue (F9).
// DebugListenMsg toggles listening for incoming PHP/Xdebug debug
// connections from php-fpm/Apache (#823): on starts the persistent DBGp
// listener session, off stops it.
type DebugListenMsg struct{}

type (
	DebugStartMsg    struct{}
	DebugStopMsg     struct{}
	DebugStepOverMsg struct{}
	DebugStepIntoMsg struct{}
	DebugStepOutMsg  struct{}
	DebugContinueMsg struct{}
)

// DebugTestAtCursorMsg debugs the test at or nearest above the cursor
// (#1914): run.testAtCursor's selection rules with a debug launch (delve's
// test mode for Go).
type DebugTestAtCursorMsg struct{}

// RunSelectMsg opens the run-configuration picker (#1914): every stored
// configuration plus the matching .vscode/launch.json entries; picking one
// runs it (debug-kind entries start a debug session).
type RunSelectMsg struct{}

// RunRerunMsg reruns the last-used run configuration (#576).
type RunRerunMsg struct{}

// TaskSelectMsg opens the Run Task picker (#1915): the Makefile targets,
// package.json scripts and justfile recipes discovered in the project root;
// picking one runs it as an ephemeral run configuration.
type TaskSelectMsg struct{}

// TaskPromoteMsg opens the task picker in promote mode (#1915): the picked
// task is stored as a normal run configuration instead of run.
type TaskPromoteMsg struct{}

// RunTestAtCursorMsg runs the test function at or nearest above the focused
// editor's cursor (#1150); RunTestsInFileMsg runs every test in the active
// test file's scope (Go: its package). Both register with run.rerun's
// last-used memory.
type (
	RunTestAtCursorMsg struct{}
	RunTestsInFileMsg  struct{}
)

// TerminalToggleMsg drives the JetBrains alt+f12 state machine (#97): no
// terminal → create one; unfocused → focus it; focused → return focus to the
// previously focused pane. Dispatched by terminal.toggle.
type TerminalToggleMsg struct{}

// TerminalPopupMsg toggles the popup terminal (#1398): the floating tab-host
// terminal overlay outside the pane layout. Dispatched by terminal.popup.
type TerminalPopupMsg struct{}

// TerminalClearMsg clears the focused (else first) terminal's scrollback and
// repaints its screen (#97). Dispatched by terminal.clear.
type TerminalClearMsg struct{}

// DiffFilesMsg asks the root model to compare two files (#60): it opens the
// "@" file picker twice — left (old) side, then right (new) side — and splits
// the focused leaf with a read-only diff viewer pane over the two picks.
// Dispatched by diff.files.
type DiffFilesMsg struct{}

// MarkdownPreviewMsg asks the root model to open a rendered markdown preview
// pane split right of the active editor, bound to its markdown buffer (#62).
// With a preview for the buffer already open it focuses that pane instead.
// Dispatched by markdown.preview.
type MarkdownPreviewMsg struct{}

// ToggleExplorerFocusMsg asks the root model to move focus to the explorer, or
// back to the active editor when the explorer already holds focus (the
// terminal approximation of JetBrains' Cmd+1 tool-window toggle). Dispatched
// by explorer.toggle.
type ToggleExplorerFocusMsg struct{}

// ZenModeMsg toggles zen mode (#359): the active editor maximized plus the
// tab bar and status line hidden. Dispatched by view.zenMode.
type ZenModeMsg struct{}

// HideToolWindowsMsg toggles hide-all-tool-windows (#791): first press
// snapshots and hides every visible tool pane, second press restores.
type HideToolWindowsMsg struct{}

// PinSlotMsg pins the active file to a numbered slot (#788, harpoon-style).
type PinSlotMsg struct{ Slot int }

// PinJumpMsg opens the file pinned to a slot (#788).
type PinJumpMsg struct{ Slot int }

// PinPickerMsg opens the pinned-files picker (#788): view, reorder, unpin.
type PinPickerMsg struct{}

// MaximizePaneMsg toggles the focused pane's zoom (#358, tmux-style): render
// it alone over the whole body, or restore the previous layout. Dispatched by
// pane.maximize.
type MaximizePaneMsg struct{}

// ShowPasteHistoryMsg asks the root model to open the palette locked to the
// paste-history mode over the focused editor's yank/delete history (#57).
// Dispatched by editor.pasteFromHistory (cmd+shift+v).
type ShowPasteHistoryMsg struct{}

// ShowScratchFilesMsg asks the root model to open the palette locked to the
// scratch-files mode (Roadmap 0280, #352). Dispatched by scratch.list.
type ShowScratchFilesMsg struct{}

// NewScratchMsg asks the root model to create a scratch file with the given
// extension under the scratch store and open it (Roadmap 0280, #351).
// Dispatched by scratch.new and the per-language scratch.new.<id> commands.
type NewScratchMsg struct{ Ext string }

// CopyPathMsg copies the focused file's path to the clipboard (#1173):
// absolute, project-relative, or relpath:line at the cursor. With the
// explorer focused, its selection is the subject (no line form there).
type CopyPathMsg struct{ Kind int }

const (
	copyAbs = iota
	copyRel
	copyRef
)

// HistoryForSelectionMsg runs vcs.historyForSelection (#1430): git history of
// the focused editor's visual selection lines (caret line fallback) via
// `git log -L`, shown as a commit picker with per-commit range patches.
type HistoryForSelectionMsg struct{}

// OpenInBrowserMsg opens the focused file in the platform default browser
// (#1429). With the explorer focused, its selection is the subject; only
// browser-viewable types (markup, images, PDF) are opened.
type OpenInBrowserMsg struct{}

// CompareClipboardMsg runs diff.compareWithClipboard (#1477): open the diff
// view comparing the active buffer — or the visual selection, when one is
// active — against the clipboard contents.
type CompareClipboardMsg struct{}

// OpenRegexTesterMsg opens the regex tester (#1937): a floating pattern +
// test-text dialog with live match and capture-group display, prefilled from
// the focused editor's visual selection. Dispatched by tools.regexTester.
type OpenRegexTesterMsg struct{}

// OpenJQPlaygroundMsg opens the jq playground (#1936): a floating query line
// over the JSON at hand — the focused HTTP response body, else the focused
// editor's visual selection, else its whole buffer — with the program's
// output live underneath. Dispatched by json.jqPlayground.
type OpenJQPlaygroundMsg struct{}

// DiffStepMsg steps the focused diff pane's current hunk (0340, #495).
type DiffStepMsg struct{ Delta int }

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
	cmds := []plugin.Command{
		appCommand("editor.closeTab", "Close Tab", CloseTabMsg{}),
		appCommand("editor.tab.next", "Next Tab", TabStepMsg{Delta: 1}),
		appCommand("editor.tab.prev", "Previous Tab", TabStepMsg{Delta: -1}),
		appCommand("editor.tab.moveLeft", "Move Tab Left", TabMoveMsg{Delta: -1}),
		appCommand("editor.tab.moveRight", "Move Tab Right", TabMoveMsg{Delta: 1}),
		appCommand("editor.tab.new", "New Empty Editor Tab", NewEditorTabMsg{}),
		appCommand("editor.tab.reopenClosed", "Reopen Closed Tab", TabReopenMsg{}),
		appCommand("editor.tab.closeOthers", "Close Other Tabs", TabCloseOthersMsg{}),
		appCommand("editor.tab.togglePin", "Pin/Unpin Tab", TabTogglePinMsg{}),
	}
	for i := 1; i <= 9; i++ {
		n := strconv.Itoa(i)
		cmds = append(cmds, appCommand("editor.tab.select"+n, "Go to Tab "+n, TabSelectMsg{Index: i - 1}))
	}
	return plugin.Capabilities{
		// The [[elasticsearch.endpoints]] list editor (#1927): registered as a
		// plugin settings page — not appended in app.go like the Tools page —
		// so docgen's settings reference documents it too.
		SettingsPages: []settings.Page{
			{Title: "Elasticsearch", Custom: settings.NewESPage(config.Discover("."))},
		},
		Commands: append(append(cmds,
			appCommand("palette.keymapHelp", "Keymap Cheatsheet", ShowKeymapHelpMsg{}),
			appCommand("help.welcomeTour", "Welcome Tour", ShowWelcomeTourMsg{}),
			appCommand("pane.switcher", "Switch Pane Focus", CyclePaneFocusMsg{}),
			appCommand("project.goToFile", "Go to File", GoToFileMsg{}),
			appCommand("palette.recentFiles", "Recent Files", ShowRecentFilesMsg{}),
			appCommand("palette.searchEverywhere", "Search Everywhere", ShowSearchEverywhereMsg{}),
			appCommand("project.findInPath", "Find in Path", OpenFindInPathMsg{}),
			appCommand("project.replaceInPath", "Replace in Path", OpenReplaceInPathMsg{}),
			appCommand("todo.list", "TODO Index", OpenTodoIndexMsg{}),
			appCommand("search.nextMatch", "Next Search Match", MatchStepMsg{Delta: 1}),
			appCommand("search.prevMatch", "Previous Search Match", MatchStepMsg{Delta: -1}),
			appCommand("editor.saveAll", "Save All", SaveAllMsg{}),
			appCommand("nav.back", "Navigate Back", NavBackMsg{}),
			appCommand("nav.forward", "Navigate Forward", NavForwardMsg{}),
			appCommand("nav.pins", "Pinned Files", PinPickerMsg{}),
			appCommand("nav.bookmarks", "Bookmarks", ShowBookmarksMsg{}),
			appCommand("bookmark.toggle", "Toggle Bookmark", BookmarkToggleMsg{}),
			appCommand("bookmark.toggleMnemonic", "Toggle Bookmark with Mnemonic", BookmarkMnemonicMsg{}),
			appCommand("bookmark.jumpMnemonic", "Go to Bookmark by Mnemonic", BookmarkMnemonicMsg{Jump: true}),
			appCommand("bookmark.annotate", "Edit Bookmark Note", BookmarkNoteMsg{}),
			appCommand("bookmark.next", "Next Bookmark", BookmarkStepMsg{Delta: 1}),
			appCommand("bookmark.previous", "Previous Bookmark", BookmarkStepMsg{Delta: -1}),
			appCommand("nav.pinSlot1", "Pin File to Slot 1", PinSlotMsg{Slot: 1}),
			appCommand("nav.pinSlot2", "Pin File to Slot 2", PinSlotMsg{Slot: 2}),
			appCommand("nav.pinSlot3", "Pin File to Slot 3", PinSlotMsg{Slot: 3}),
			appCommand("nav.pinSlot4", "Pin File to Slot 4", PinSlotMsg{Slot: 4}),
			appCommand("nav.pinGoto1", "Go to Pinned File 1", PinJumpMsg{Slot: 1}),
			appCommand("nav.pinGoto2", "Go to Pinned File 2", PinJumpMsg{Slot: 2}),
			appCommand("nav.pinGoto3", "Go to Pinned File 3", PinJumpMsg{Slot: 3}),
			appCommand("nav.pinGoto4", "Go to Pinned File 4", PinJumpMsg{Slot: 4}),
			appCommand("file.openPath", "Open File…", OpenFilePathMsg{}),
			appCommand("file.rename", "Rename File", RenameFileMsg{}),
			appCommand("file.move", "Move File", MoveFileMsg{}),
			appCommand("explorer.toggle", "Focus Explorer / Editor", ToggleExplorerFocusMsg{}),
			appCommand("markdown.preview", "Markdown Preview", MarkdownPreviewMsg{}),
			appCommand("diff.files", "Diff Two Files…", DiffFilesMsg{}),
			appCommand("diff.compareWithClipboard", "Compare with Clipboard", CompareClipboardMsg{}),
			appCommand("file.localHistory", "Show Local History", LocalHistoryMsg{}),
			appCommand("file.timeline", "Show Timeline", TimelineMsg{}),
			appCommand("file.copyPath", "Copy Path", CopyPathMsg{Kind: copyAbs}),
			appCommand("file.copyRelPath", "Copy Relative Path", CopyPathMsg{Kind: copyRel}),
			appCommand("file.copyReference", "Copy Reference", CopyPathMsg{Kind: copyRef}),
			appCommand("file.openInBrowser", "Open in Browser", OpenInBrowserMsg{}),
			appCommand("tools.setup", "Set Up Tool Panes", ShowToolSetupMsg{}),
			appCommand("tools.regexTester", "Regex Tester…", OpenRegexTesterMsg{}),
			appCommand("json.jqPlayground", "jq Playground…", OpenJQPlaygroundMsg{}),
			appCommand("terminal.new", "New Terminal", TerminalNewMsg{}),
			appCommand("terminal.newTab", "New Terminal Tab", TerminalNewTabMsg{}),
			appCommand("terminal.ssh", "SSH Host…", SSHPickerMsg{}),
			appCommand("run.file", "Run File", RunFileMsg{}),
			appCommand("run.rerun", "Rerun Last", RunRerunMsg{}),
			appCommand("run.testAtCursor", "Run Test at Cursor", RunTestAtCursorMsg{}),
			appCommand("run.testsInFile", "Run Tests in File", RunTestsInFileMsg{}),
			appCommand("run.select", "Run/Debug Configurations…", RunSelectMsg{}),
			appCommand("run.task", "Run Task…", TaskSelectMsg{}),
			appCommand("run.taskPromote", "Promote Task to Run Configuration…", TaskPromoteMsg{}),
			appCommand("http.run", "Run HTTP Request", HTTPRunMsg{}),
			appCommand("http.copyBody", "Copy HTTP Response Body", HTTPCopyBodyMsg{}),
			appCommand("http.copyHeaders", "Copy HTTP Response Headers", HTTPCopyHeadersMsg{}),
			appCommand("http.copyFold", "Copy Folded Range (HTTP Response)", HTTPCopyFoldMsg{}),
			appCommand("http.responseHistory", "Browse HTTP Response History", HTTPResponseHistoryMsg{}),
			appCommand("http.showResponse", "Show Stored HTTP Response", HTTPShowResponseMsg{}),
			appCommand("http.resend", "Re-send Stored HTTP Request", HTTPResendMsg{}),
			appCommand("http.selectEnvironment", "Select HTTP Environment", HTTPSelectEnvMsg{}),
			appCommand("http.importOpenAPI", "Import OpenAPI Spec…", ImportOpenAPIMsg{}),
			appCommand("http.cancel", "Cancel Running HTTP Request", HTTPCancelMsg{}),
			appCommand("debug.toggleBreakpoint", "Toggle Breakpoint", DebugToggleBreakpointMsg{}),
			appCommand("debug.breakpoints", "Breakpoints", BreakpointsToggleMsg{}),
			appCommand("debug.start", "Debug File", DebugStartMsg{}),
			appCommand("debug.testAtCursor", "Debug Test at Cursor", DebugTestAtCursorMsg{}),
			appCommand("debug.listen", "Listen for PHP Debug Connections", DebugListenMsg{}),
			appCommand("debug.stop", "Stop Debug Session", DebugStopMsg{}),
			appCommand("debug.stepOver", "Step Over", DebugStepOverMsg{}),
			appCommand("debug.stepInto", "Step Into", DebugStepIntoMsg{}),
			appCommand("debug.stepOut", "Step Out", DebugStepOutMsg{}),
			appCommand("debug.continue", "Continue", DebugContinueMsg{}),
			appCommand("terminal.toggle", "Toggle Terminal", TerminalToggleMsg{}),
			appCommand("terminal.popup", "Popup Terminal", TerminalPopupMsg{}),
			appCommand("terminal.clear", "Clear Terminal", TerminalClearMsg{}),
			appCommand("notifications.history", "Notification History", ShowNotificationHistoryMsg{}),
			appCommand("menu.open", "Open Menu Bar", ToggleMenuMsg{}),
			appCommand("settings.open", "Settings", OpenSettingsMsg{}),
			appCommand("python.newEnvironment", "New Python Environment…", OpenPythonEnvWizardMsg{}),
			appCommand("keymap.importJetBrains", "Import JetBrains Keymap XML…", ImportJetBrainsKeymapMsg{}),
			appCommand("pane.splitDown", "Split Down", SplitFocusedMsg{Zone: layout.ZoneBottom}),
			appCommand("pane.splitUp", "Split Up", SplitFocusedMsg{Zone: layout.ZoneTop}),
			appCommand("pane.splitRight", "Split Right", SplitFocusedMsg{Zone: layout.ZoneRight}),
			appCommand("pane.splitLeft", "Split Left", SplitFocusedMsg{Zone: layout.ZoneLeft}),
			appCommand("editor.splitViewRight", "Split View Right", SplitViewMsg{Zone: layout.ZoneRight}),
			appCommand("editor.splitViewDown", "Split View Down", SplitViewMsg{Zone: layout.ZoneBottom}),
			appCommand("editor.pasteFromHistory", "Paste from History", ShowPasteHistoryMsg{}),
			appCommand("editor.forceCodeInsight", "Force Code Insight (Large File)", ForceCodeInsightMsg{}),
			appCommand("pane.maximize", "Maximize Pane", MaximizePaneMsg{}),
			appCommand("pane.close", "Close Pane", ClosePaneMsg{}),
			appCommand("view.zenMode", "Zen Mode", ZenModeMsg{}),
			appCommand("window.hideAllTools", "Hide All Tool Windows", HideToolWindowsMsg{}),
			appCommand("window.saveLayout", "Save Window Layout…", SaveLayoutPromptMsg{}),
			appCommand("window.layouts", "Window Layouts…", ShowLayoutsMsg{}),
			appCommand("window.setDefaultLayout", "Set Default Window Layout…", ShowLayoutsMsg{SetDefault: true}),
			appCommand("window.restoreLayout", "Restore Default Layout", RestoreDefaultLayoutMsg{}),
			appCommand("vcs.revertFile", "Revert File", RevertActiveFileMsg{}),
			appCommand("vcs.revertHunk", "Revert Hunk Under Caret", RevertHunkMsg{}),
			appCommand("vcs.undoRevert", "Undo Revert…", UndoRevertMsg{}),
			appCommand("vcs.diff", "Diff File Against HEAD", DiffHeadMsg{}),
			appCommand("vcs.mergeFile", "Resolve Conflicts in Merge View", MergeFileMsg{}),
			appCommand("vcs.mergeApply", "Merge: Save Result and Stage", MergeApplyMsg{}),
			appCommand("vcs.blameLine", "Toggle Inline Blame", ToggleBlameMsg{}),
			appCommand("vcs.historyForSelection", "Show History for Selection", HistoryForSelectionMsg{}),
			appCommand("vcs.panel", "Toggle VCS Tool Window", VCSPanelToggleMsg{}),
			appCommand("problems.toggle", "Problems", ProblemsToggleMsg{}),
			appCommand("structure.toggle", "Structure", StructureToggleMsg{}),
			appCommand("dom.toggle", "DOM Inspector", DOMToggleMsg{}),
			appCommand("usages.toggle", "Usages", UsagesToggleMsg{}),
			appCommand("tests.toggle", "Test Results", TestsToggleMsg{}),
			appCommand("issues.toggle", "GitHub Issues", IssuesToggleMsg{}),
			appCommand("diff.nextChange", "Next Change (Diff)", DiffStepMsg{Delta: 1}),
			appCommand("diff.prevChange", "Previous Change (Diff)", DiffStepMsg{Delta: -1}),
		), append(append(append(scratchCommands(), toolCommands()...), memoryCommands()...), esCommands()...)...),
	}
}

func init() { registry.Register(appCommands{}) }
