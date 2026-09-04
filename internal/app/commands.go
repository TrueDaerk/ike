package app

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/intention"
	"ike/internal/jqplay"
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

// TabPickerMsg opens the tab picker (#2151): the focused editor pane's tabs
// listed most-recently-used first in a locked palette mode, filtered by the
// palette's speed search, enter activating the picked tab. Dispatched by
// editor.tab.picker.
type TabPickerMsg struct{}

// ClosePaneMsg closes the focused pane whole — every tab at once — behind the
// unsaved-changes guard (#1128). Dispatched by pane.close (pane-title context
// menu / palette).
type ClosePaneMsg struct{}

// ForceCodeInsightMsg asks the root model to lift the large-file degradation
// (#149) for the focused document: highlighting reparses and the LSP bridge
// didOpens despite the size. Dispatched by editor.forceCodeInsight.
type ForceCodeInsightMsg struct{}

// LargeFileDetailsMsg asks the root model to open the large-file detail popup
// (#2159): which per-edit services are degraded for the focused document and
// at which thresholds. Dispatched by editor.largeFileDetails and a click on
// the status line's large-file badge.
type LargeFileDetailsMsg struct{}

// OpenSearchMsg asks the focused pane to open its own search or filter
// (#2409). Dispatched by search.open (cmd+f), the Global half of the shared
// find chord: the root model asks the pane for the pane.Searchable capability
// and notifies when the pane has no search of its own.
type OpenSearchMsg struct{}

// ShowKeymapHelpMsg asks the root model to open the keymap cheatsheet overlay,
// the same view the hardcoded "?" opens. Dispatched by palette.keymapHelp.
type ShowKeymapHelpMsg struct{}

// KeymapDoctorMsg asks the root model to open the keymap doctor (#2080): the
// terminal reality probe run inside the session, whose saved verdicts become
// this terminal's reachability overrides. Dispatched by keymap.doctor.
type KeymapDoctorMsg struct{}

// KeymapDeadBindingsMsg asks the root model to open the keymap doctor's
// dead-binding report (#2161): the active keymap audited against this
// platform and terminal, with a suggested rebind per unreachable chord.
// Dispatched by keymap.deadBindings.
type KeymapDeadBindingsMsg struct{}

// CyclePaneFocusMsg asks the root model to move focus to the next pane, the
// same behavior as the hardcoded tab. Dispatched by pane.switcher.
type CyclePaneFocusMsg struct{}

// PaneFocusIndexMsg asks the root model to focus the pane carrying Index in
// the chrome (#2407): the numbers run in layout reading order and are drawn in
// the pane title bars. Dispatched by pane.focus1…pane.focus9.
type PaneFocusIndexMsg struct{ Index int }

// PaneFocusByIndexMsg asks the root model to open the pane-number prompt
// (#2407), the typed flavour of the digit chords — for the panes past nine and
// for terminals that swallow the chords. Dispatched by pane.focusByIndex.
type PaneFocusByIndexMsg struct{}

// SwitchProjectMRUMsg asks the root model to switch to the Index-th (1-based)
// most recently used other project (#2489): the numbering the project picker
// and the Recent Projects column render as their row hints. Dispatched by
// project.switchMRU1…project.switchMRU9.
type SwitchProjectMRUMsg struct{ Index int }

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

// PaneResizeModeMsg asks the root model to arm the sticky keyboard pane
// resize mode for the focused pane (#2150). Dispatched by pane.resizeMode.
type PaneResizeModeMsg struct{}

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

// OpenFindInAllProjectsMsg asks the root model to open the all-projects
// search form (#2394). Dispatched by project.findInAllProjects
// (cmd+alt+shift+f / palette).
type OpenFindInAllProjectsMsg struct{}

// ShowAllFindResultsMsg asks the root model to open the all-projects search
// results overlay (#2394, #2413). Dispatched by
// project.findInAllProjectsResults (cmd+alt+shift+r / palette).
type ShowAllFindResultsMsg struct{}

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

// DebugBreakpointPropertiesMsg opens the breakpoint-properties form on the
// focused editor's cursor line (#2245) — condition, hit count, log message.
type DebugBreakpointPropertiesMsg struct{}

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

// DebugRunToCursorMsg resumes the paused debuggee towards the focused
// editor's cursor line (#2405); DebugRunToLineMsg asks for the line number
// first. Both install one temporary breakpoint that never enters the store.
type (
	DebugRunToCursorMsg struct{}
	DebugRunToLineMsg   struct{}
)

// GoToLineMsg opens editor.goToLine's prompt (#2486): a `line[:column]` target
// — absolute or relative — that moves the caret inside the current buffer.
type GoToLineMsg struct{}

// DebugEvaluateMsg evaluates the editor's selection — or an expression the
// prompt asks for — in the paused frame and shows the result in the evaluate
// popup (#2174).
type DebugEvaluateMsg struct{}

// DebugCopyMsg runs debug.copy (cmd+c in the debug panel, #2400): the
// selected variable value, watch, stack frame — or the console's mouse
// selection — goes to the clipboard.
type DebugCopyMsg struct{}

// IssuesCopyMsg runs issues.copy (cmd+c in the issues window, #2400): the
// selection when there is one, else the selected issue's URL.
type IssuesCopyMsg struct{}

// LSPDoctorCopyMsg runs lsp.doctor.copy (cmd+c in the LSP Doctor, #2487):
// the whole report — header counts and every server row with its checks,
// diagnosis and fix — goes to the clipboard as plain text.
type LSPDoctorCopyMsg struct{}

// IssuesStepMsg runs issues.selectPrev / issues.selectNext (ctrl+up /
// ctrl+down, #2400): walk the issues window's selection.
type IssuesStepMsg struct{ Delta int }

// HTTPSearchMsg runs http.search (cmd+f / ctrl+f in the response viewer,
// #2400): open the pane's in-pane search prompt.
type HTTPSearchMsg struct{}

// DebugConsoleMsg toggles the combined debug area between its variables and
// console views and focuses it (#2190) — the keyboard route that works even
// while a PTY debuggee owns the raw keys.
type DebugConsoleMsg struct{}

// DebugTestAtCursorMsg debugs the test at or nearest above the cursor
// (#1914): run.testAtCursor's selection rules with a debug launch (delve's
// test mode for Go).
type DebugTestAtCursorMsg struct{}

// RunSelectMsg opens the run-configuration picker (#1914): every stored
// configuration plus the matching .vscode/launch.json entries; picking one
// runs it (debug-kind entries start a debug session).
type RunSelectMsg struct{}

// RunRerunMsg reruns the last-used run configuration the way it was started
// (#576, #2173): a launch that ran under the debugger reruns under it.
type RunRerunMsg struct{}

// RunEditConfigMsg opens the run-configuration form (#2173): the picker in
// edit mode, and the picked stored configuration's environment editor.
type RunEditConfigMsg struct{}

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

// RunTestsWithCoverageMsg is RunTestsInFileMsg with per-line coverage
// collection (#2081); CoverageToggleMsg hides/shows the resulting editor
// gutter marks without dropping the run's data.
type (
	RunTestsWithCoverageMsg struct{}
	CoverageToggleMsg       struct{}
)

// TerminalToggleMsg drives the JetBrains alt+f12 state machine (#97): no
// terminal → create one; unfocused → focus it; focused → return focus to the
// previously focused pane. Dispatched by terminal.toggle.
type TerminalToggleMsg struct{}

// TerminalPopupMsg toggles the popup terminal (#1398): the floating tab-host
// terminal overlay outside the pane layout. Dispatched by terminal.popup.
type TerminalPopupMsg struct{}

// TerminalPopupPinMsg toggles the popup terminal's pinned mode (#2406): the
// popup stays visible while the keyboard goes back to the editor, anchored to
// the bottom edge, and the popup chord then only moves focus. Dispatched by
// terminal.popup.pin.
type TerminalPopupPinMsg struct{}

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

// RerenderDiagramsMsg asks every open markdown preview to forget its cached
// diagram renderings and run the external renderers again (#2421). Dispatched
// by preview.rerenderDiagrams.
type RerenderDiagramsMsg struct{}

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
//
// Content seeds the new scratch (#2339: "New Scratch from Selection" hands
// over the selected text). An empty Content means "no content of my own", and
// the scratch is seeded with the language's file template as before — the
// language creators all take that path.
type NewScratchMsg struct {
	Ext     string
	Content string
}

// NewScratchFromSelectionMsg asks the root model to create a scratch from the
// active selection, inheriting the source file's extension (#2339).
// Dispatched by scratch.newFromSelection.
type NewScratchFromSelectionMsg struct{}

// PromoteScratchMsg asks the root model to promote a scratch to a project file
// (#2339): the focused editor's scratch, or the one Path names when the
// scratch manager dispatches it for its selected row. Dispatched by
// scratch.promote.
type PromoteScratchMsg struct{ Path string }

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

// OpenPlaygroundMsg opens the playground of one dialect (#1936, #2039): a
// query line mounted inline in the pane holding the document at hand (#1970) —
// for jq the focused HTTP response body, else the focused editor's visual
// selection, else its whole buffer; for yq the editor alone — with the
// program's output live in a read-only result buffer underneath. Dispatched by
// json.jqPlayground and yaml.yqPlayground.
type OpenPlaygroundMsg struct{ Dialect jqplay.Dialect }

// OpenPlaygroundAtPathMsg opens the playground prefilled with the caret's
// document path (#1982) instead of the default program — the explicit form of
// what used to happen on every open. Dispatched by json.jqPlaygroundAtPath and
// yaml.yqPlaygroundAtPath.
type OpenPlaygroundAtPathMsg struct{ Dialect jqplay.Dialect }

// OpenPlaygroundForBufferMsg opens whichever playground speaks the focused
// buffer's language (#2415): jq for JSON, yq for YAML, xmq for XML/HTML —
// one chord for "query this file", with the per-dialect commands still there
// for anyone who wants them bound separately. A language no playground speaks
// answers in a notification. Dispatched by playground.open.
type OpenPlaygroundForBufferMsg struct{}

// TogglePlaygroundQueryViewMsg toggles the playground's expanded query view (#2032):
// the query line lays the whole program out over several wrapped rows —
// broken at its `|` stages, highlighted like the one-line view — instead of
// windowing one row around the cursor. Dispatched by json.jqQueryView; a no-op
// while no playground is open. It carries no dialect — it is a view toggle on
// whichever playground is up.
type TogglePlaygroundQueryViewMsg struct{}

// OpenMergedLogMsg is declared in logsets.go: it merges the focused log
// buffer's rotation set into one chronological timeline (#1996).

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

// paneCommand is appCommand scoped to one pane context: the command only
// exists while a pane advertising ctxID has the focus, which is what keeps a
// grid action like the column profile (#1940) out of the palette everywhere
// else.
func paneCommand(id, title, ctxID string, msg tea.Msg) plugin.Command {
	c := appCommand(id, title, msg)
	c.Scope = plugin.PaneScope(ctxID)
	return c
}

// langCommand narrows a command to buffers of the given languages (#2483):
// the help overlay lists it — badged with the family's canonical name — only
// while the focused buffer (editor tab or HTTP response body) speaks one of
// them, and the palette ranks it off-context elsewhere. The playground
// families reuse the routing lists from playgroundopen.go, so the gate and
// playKindFor never disagree.
func langCommand(c plugin.Command, langs []string) plugin.Command {
	c.Languages = langs
	return c
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
		appCommand("editor.tab.picker", "Switch Tab…", TabPickerMsg{}),
	}
	for i := 1; i <= 9; i++ {
		n := strconv.Itoa(i)
		cmds = append(cmds, appCommand("editor.tab.select"+n, "Go to Tab "+n, TabSelectMsg{Index: i - 1}))
		// The pane twins of the tab jumps (#2407): the number is the one the
		// pane's title bar shows.
		cmds = append(cmds, appCommand("pane.focus"+n, "Focus Pane "+n, PaneFocusIndexMsg{Index: i}))
		// The project twins (#2489): the number is the MRU rank the picker
		// and the Recent Projects column show in front of the row.
		cmds = append(cmds, appCommand("project.switchMRU"+n, "Switch to Recent Project "+n, SwitchProjectMRUMsg{Index: i}))
	}
	return plugin.Capabilities{
		// The [[elasticsearch.endpoints]] list editor (#1927): registered as a
		// plugin settings page — not appended in app.go like the Tools page —
		// so docgen's settings reference documents it too.
		SettingsPages: []settings.Page{
			{Title: "Elasticsearch", Custom: settings.NewESPage(config.Discover("."))},
		},
		// The built-in intention catalog (#2020): caret-dependent doorways
		// into the commands above, merged into alt+enter's popup.
		Intentions: append(intention.Builtins(), depsIntention()),
		Commands: append(append(cmds,
			appCommand("palette.keymapHelp", "Keymap Cheatsheet", ShowKeymapHelpMsg{}),
			appCommand("help.welcomeTour", "Welcome Tour", ShowWelcomeTourMsg{}),
			appCommand("pane.switcher", "Switch Pane Focus", CyclePaneFocusMsg{}),
			appCommand("pane.focusByIndex", "Focus Pane by Number…", PaneFocusByIndexMsg{}),
			appCommand("project.goToFile", "Go to File", GoToFileMsg{}),
			appCommand("palette.recentFiles", "Recent Files", ShowRecentFilesMsg{}),
			appCommand("palette.searchEverywhere", "Search Everywhere", ShowSearchEverywhereMsg{}),
			appCommand("project.findInPath", "Find in Path", OpenFindInPathMsg{}),
			appCommand("project.replaceInPath", "Replace in Path", OpenReplaceInPathMsg{}),
			appCommand("project.findInAllProjects", "Find in All Projects", OpenFindInAllProjectsMsg{}),
			appCommand("project.findInAllProjectsResults", "Show All-Projects Search Results", ShowAllFindResultsMsg{}),
			appCommand("todo.list", "TODO Index", OpenTodoIndexMsg{}),
			appCommand("search.nextMatch", "Next Search Match", MatchStepMsg{Delta: 1}),
			appCommand("search.prevMatch", "Previous Search Match", MatchStepMsg{Delta: -1}),
			appCommand("editor.saveAll", "Save All", SaveAllMsg{}),
			appCommand("editor.goToLine", "Go to Line…", GoToLineMsg{}),
			appCommand("nav.back", "Navigate Back", NavBackMsg{}),
			appCommand("nav.forward", "Navigate Forward", NavForwardMsg{}),
			appCommand("nav.pins", "Pinned Files", PinPickerMsg{}),
			appCommand("nav.bookmarks", "Bookmarks", ShowBookmarksMsg{}),
			appCommand("bookmark.toggle", "Toggle Bookmark", BookmarkToggleMsg{}),
			appCommand("bookmark.toggleMnemonic", "Toggle Bookmark with Mnemonic", BookmarkMnemonicMsg{}),
			appCommand("bookmark.jumpMnemonic", "Go to Bookmark by Mnemonic", BookmarkMnemonicMsg{Jump: true}),
			appCommand("bookmark.annotate", "Edit Bookmark Note", BookmarkNoteMsg{}),
			appCommand("bookmark.overview", "Bookmarks Overview", BookmarkOverviewMsg{}),
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
			appCommand("file.openAs", "Open File As…", ShowOpenAsMsg{}),
			appCommand("file.rename", "Rename File", RenameFileMsg{}),
			appCommand("file.move", "Move File", MoveFileMsg{}),
			appCommand("explorer.toggle", "Focus Explorer / Editor", ToggleExplorerFocusMsg{}),
			langCommand(appCommand("markdown.preview", "Markdown Preview", MarkdownPreviewMsg{}), []string{"markdown"}),
			appCommand("preview.rerenderDiagrams", "Re-render Preview Diagrams", RerenderDiagramsMsg{}),
			appCommand("editor.setBufferLanguage", "Treat Buffer as…", ShowBufferLangMsg{}),
			appCommand("editor.materializeBuffer", "Materialize Buffer to File", MaterializeBufferMsg{}),
			appCommand("diff.files", "Diff Two Files…", DiffFilesMsg{}),
			appCommand("diff.compareWithClipboard", "Compare with Clipboard", CompareClipboardMsg{}),
			appCommand("file.localHistory", "Show Local History", LocalHistoryMsg{}),
			appCommand("file.timeline", "Show Timeline", TimelineMsg{}),
			appCommand("history.projectTimeline", "Show Project History Timeline", ProjectHistoryMsg{}),
			appCommand("watch.changeFeed", "Show External Changes", ChangeFeedMsg{}),
			appCommand("file.copyPath", "Copy Path", CopyPathMsg{Kind: copyAbs}),
			appCommand("file.copyRelPath", "Copy Relative Path", CopyPathMsg{Kind: copyRel}),
			appCommand("file.copyReference", "Copy Reference", CopyPathMsg{Kind: copyRef}),
			appCommand("file.openInBrowser", "Open in Browser", OpenInBrowserMsg{}),
			appCommand("tools.setup", "Set Up Tool Panes", ShowToolSetupMsg{}),
			appCommand("tools.regexTester", "Regex Tester…", OpenRegexTesterMsg{}),
			// The dialect dispatcher (#2415): one command in front of the
			// per-dialect ones, resolving jq/yq/xmq from the buffer's
			// language. The dialect commands stay separate entries — bindable
			// and frecency-counted on their own (#2153).
			langCommand(appCommand("playground.open", "Open Playground for This File", OpenPlaygroundForBufferMsg{}), playgroundLangs()),
			langCommand(appCommand("json.jqPlayground", "jq Playground…", OpenPlaygroundMsg{}), jqLangs),
			langCommand(appCommand("json.jqPlaygroundAtPath", "jq Playground at Cursor Path…", OpenPlaygroundAtPathMsg{}), jqLangs),
			langCommand(appCommand("json.jqFilters", "Saved jq Filters…", ShowFiltersMsg{}), jqLangs),
			langCommand(appCommand("json.jqRenameFilter", "Rename Saved jq Filter…", ShowFiltersMsg{Rename: true}), jqLangs),
			// The yq playground (#2039) is the same mode over YAML, so it is
			// the same five commands under the yaml namespace. The save
			// prompt and the query-view toggle are not among them: both act
			// on whichever playground is open and would only be a second name
			// for one behavior.
			langCommand(appCommand("yaml.yqPlayground", "yq Playground…", OpenPlaygroundMsg{Dialect: jqplay.DialectYQ}), yqLangs),
			langCommand(appCommand("yaml.yqPlaygroundAtPath", "yq Playground at Cursor Path…", OpenPlaygroundAtPathMsg{Dialect: jqplay.DialectYQ}), yqLangs),
			langCommand(appCommand("yaml.yqFilters", "Saved yq Filters…", ShowFiltersMsg{Dialect: jqplay.DialectYQ}), yqLangs),
			langCommand(appCommand("yaml.yqRenameFilter", "Rename Saved yq Filter…", ShowFiltersMsg{Dialect: jqplay.DialectYQ, Rename: true}), yqLangs),
			// The xmq playground (#2414) — same mode over XML/HTML, engine
			// the external xmq CLI — under the xml namespace: the same five
			// commands as its siblings, with the at-path flavour seeding a
			// `select <xpath>` over the caret's element.
			langCommand(appCommand("xml.xmqPlayground", "xmq Playground…", OpenPlaygroundMsg{Dialect: jqplay.DialectXMQ}), xmqLangs),
			langCommand(appCommand("xml.xmqPlaygroundAtPath", "xmq Playground at Element XPath…", OpenPlaygroundAtPathMsg{Dialect: jqplay.DialectXMQ}), xmqLangs),
			langCommand(appCommand("xml.xmqFilters", "Saved xmq Filters…", ShowFiltersMsg{Dialect: jqplay.DialectXMQ}), xmqLangs),
			langCommand(appCommand("xml.xmqRenameFilter", "Rename Saved xmq Filter…", ShowFiltersMsg{Dialect: jqplay.DialectXMQ, Rename: true}), xmqLangs),
			// The language cheatsheet (#2382), one command per dialect: the
			// sheet's document-language rows and its wording differ, so
			// "jq cheatsheet" and "yq cheatsheet" are two different sheets
			// rather than one with a toggle.
			langCommand(appCommand("json.jqCheatsheet", "jq Cheatsheet…", ShowCheatsheetMsg{}), jqLangs),
			langCommand(appCommand("yaml.yqCheatsheet", "yq Cheatsheet…", ShowCheatsheetMsg{Dialect: jqplay.DialectYQ}), yqLangs),
			langCommand(appCommand("xml.xmqCheatsheet", "xmq Cheatsheet…", ShowCheatsheetMsg{Dialect: jqplay.DialectXMQ}), xmqLangs),
			// The save prompt and the query-view toggle act on whichever
			// playground is open, so their gate is the union of the families.
			langCommand(appCommand("json.jqSaveFilter", "Save Playground Filter…", SaveFilterPromptMsg{}), playgroundLangs()),
			langCommand(appCommand("json.jqQueryView", "Toggle Full Query View", TogglePlaygroundQueryViewMsg{}), playgroundLangs()),
			langCommand(appCommand("log.openRotatedSet", "Open Rotated Log Set (Merged Timeline)", OpenMergedLogMsg{}), []string{"log"}),
			appCommand("terminal.new", "New Terminal", TerminalNewMsg{}),
			appCommand("terminal.newTab", "New Terminal Tab", TerminalNewTabMsg{}),
			appCommand("terminal.ssh", "SSH Host…", SSHPickerMsg{}),
			appCommand("remote.browse", "Browse SSH Host (SFTP)…", RemoteBrowseMsg{}),
			appCommand("run.file", "Run File", RunFileMsg{}),
			appCommand("run.rerun", "Rerun Last", RunRerunMsg{}),
			appCommand("run.testAtCursor", "Run Test at Cursor", RunTestAtCursorMsg{}),
			appCommand("run.testsInFile", "Run Tests in File", RunTestsInFileMsg{}),
			appCommand("run.testsWithCoverage", "Run Tests in File with Coverage", RunTestsWithCoverageMsg{}),
			appCommand("coverage.toggle", "Toggle Coverage Marks", CoverageToggleMsg{}),
			appCommand("run.select", "Run/Debug Configurations…", RunSelectMsg{}),
			appCommand("run.editConfig", "Edit Run Configuration…", RunEditConfigMsg{}),
			appCommand("run.task", "Run Task…", TaskSelectMsg{}),
			appCommand("run.taskPromote", "Promote Task to Run Configuration…", TaskPromoteMsg{}),
			appCommand("http.run", "Run HTTP Request", HTTPRunMsg{}),
			appCommand("http.copyBody", "Copy HTTP Response Body", HTTPCopyBodyMsg{}),
			appCommand("http.copyResponse", "Copy HTTP Response Selection or Body", HTTPCopyResponseMsg{}),
			appCommand("http.copyHeaders", "Copy HTTP Response Headers", HTTPCopyHeadersMsg{}),
			appCommand("http.copyFold", "Copy Folded Range (HTTP Response)", HTTPCopyFoldMsg{}),
			appCommand("http.responseHistory", "Browse HTTP Response History", HTTPResponseHistoryMsg{}),
			appCommand("http.showResponse", "Show Stored HTTP Response", HTTPShowResponseMsg{}),
			appCommand("http.resend", "Re-send Stored HTTP Request", HTTPResendMsg{}),
			appCommand("http.rerun", "Re-run HTTP Request from History", HTTPRerunMsg{}),
			appCommand("http.diffResponses", "Compare Stored HTTP Responses", HTTPDiffResponsesMsg{}),
			appCommand("http.diffPreviousRun", "Diff HTTP Response Against Previous Run", HTTPDiffPreviousRunMsg{}),
			appCommand("http.selectEnvironment", "Select HTTP Environment", HTTPSelectEnvMsg{}),
			appCommand("http.importOpenAPI", "Import OpenAPI Spec…", ImportOpenAPIMsg{}),
			appCommand("http.importCurl", "Import curl Command…", ImportCurlMsg{}),
			appCommand("http.copyAsCurl", "Copy HTTP Request as curl", HTTPCopyAsCurlMsg{}),
			appCommand("http.copyShownAsCurl", "Copy Shown HTTP Request as curl", HTTPCopyShownAsCurlMsg{}),
			appCommand("http.copyAsHttpie", "Copy HTTP Request as httpie", HTTPCopyAsHttpieMsg{}),
			appCommand("http.copyShownAsHttpie", "Copy Shown HTTP Request as httpie", HTTPCopyShownAsHttpieMsg{}),
			appCommand("http.saveResponse", "Save HTTP Response Body to File…", HTTPSaveResponseMsg{}),
			// The two GraphQL schema commands (#2423): one asks the endpoint
			// what it offers, the other shows the answer. Both act on the
			// GRAPHQL block under the caret, so neither needs a prompt.
			appCommand("http.graphqlIntrospect", "Introspect GraphQL Schema", HTTPGraphQLIntrospectMsg{}),
			appCommand("http.graphqlSchema", "Open Cached GraphQL Schema (SDL)", HTTPGraphQLSchemaMsg{}),
			appCommand("archive.extractEntry", "Extract Selected Archive Entry…", ArchiveExtractEntryMsg{}),
			appCommand("archive.extractAll", "Extract Whole Archive…", ArchiveExtractAllMsg{}),
			appCommand("archive.reload", "Reload Archive Listing", ArchiveReloadMsg{}),
			appCommand("http.toggleRawBody", "Toggle Raw / Pretty HTTP Response Body", HTTPToggleRawBodyMsg{}),
			langCommand(appCommand("http.jqPlayground", "Open jq Playground on HTTP Response", HTTPJQPlaygroundMsg{}), jqLangs),
			appCommand("http.loadMoreBody", "Load More of the HTTP Response Body", HTTPLoadMoreBodyMsg{}),
			appCommand("http.openBodyFile", "Open Full HTTP Response Body as File", HTTPOpenBodyFileMsg{}),
			appCommand("http.insertCurlAsRequest", "Insert curl as HTTP Request", InsertCurlAsRequestMsg{}),
			appCommand("vault.treatAsFile", "Treat as Vault File", TreatAsVaultFileMsg{}),
			appCommand("http.cancel", "Cancel Running HTTP Request", HTTPCancelMsg{}),
			appCommand("debug.toggleBreakpoint", "Toggle Breakpoint", DebugToggleBreakpointMsg{}),
			appCommand("debug.breakpointProperties", "Breakpoint Properties…", DebugBreakpointPropertiesMsg{}),
			appCommand("debug.breakpoints", "Breakpoints", BreakpointsToggleMsg{}),
			appCommand("debug.start", "Debug File", DebugStartMsg{}),
			appCommand("debug.testAtCursor", "Debug Test at Cursor", DebugTestAtCursorMsg{}),
			appCommand("debug.listen", "Listen for PHP Debug Connections", DebugListenMsg{}),
			appCommand("debug.doctor", "Xdebug Doctor", DebugDoctorMsg{}),
			appCommand("debug.stop", "Stop Debug Session", DebugStopMsg{}),
			appCommand("debug.stepOver", "Step Over", DebugStepOverMsg{}),
			appCommand("debug.stepInto", "Step Into", DebugStepIntoMsg{}),
			appCommand("debug.stepOut", "Step Out", DebugStepOutMsg{}),
			appCommand("debug.continue", "Continue", DebugContinueMsg{}),
			appCommand("debug.runToCursor", "Run to Cursor", DebugRunToCursorMsg{}),
			appCommand("debug.runToLine", "Run to Line…", DebugRunToLineMsg{}),
			appCommand("debug.console", "Debug: Toggle Console/Variables View", DebugConsoleMsg{}),
			appCommand("debug.evaluate", "Evaluate Expression", DebugEvaluateMsg{}),
			// The pane-scoped copy/step chords of the #2400 audit: each one
			// only means something while its own pane has the focus, so they
			// are paneCommands rather than global ones.
			paneCommand("debug.copy", "Debug: Copy Selected Value", "debug", DebugCopyMsg{}),
			paneCommand("issues.copy", "Issues: Copy Issue Reference", "issues", IssuesCopyMsg{}),
			paneCommand("issues.selectNext", "Issues: Next Issue", "issues", IssuesStepMsg{Delta: 1}),
			paneCommand("issues.selectPrev", "Issues: Previous Issue", "issues", IssuesStepMsg{Delta: -1}),
			paneCommand("lsp.doctor.copy", "LSP Doctor: Copy Report", "lspdoctor", LSPDoctorCopyMsg{}),
			paneCommand("http.search", "Search in HTTP Response", "http", HTTPSearchMsg{}),
			appCommand("terminal.toggle", "Toggle Terminal", TerminalToggleMsg{}),
			appCommand("terminal.popup", "Popup Terminal", TerminalPopupMsg{}),
			appCommand("terminal.popup.pin", "Pin Popup Terminal", TerminalPopupPinMsg{}),
			appCommand("terminal.clear", "Clear Terminal", TerminalClearMsg{}),
			appCommand("notifications.history", "Notification History", ShowNotificationHistoryMsg{}),
			appCommand("menu.open", "Open Menu Bar", ToggleMenuMsg{}),
			appCommand("settings.open", "Settings", OpenSettingsMsg{}),
			appCommand("python.newEnvironment", "New Python Environment…", OpenPythonEnvWizardMsg{}),
			appCommand("keymap.importJetBrains", "Import JetBrains Keymap XML…", ImportJetBrainsKeymapMsg{}),
			appCommand("keymap.doctor", "Keymap Doctor: Probe Chord Delivery", KeymapDoctorMsg{}),
			appCommand("keymap.deadBindings", "Keymap Doctor: Dead Bindings", KeymapDeadBindingsMsg{}),
			appCommand("pane.splitDown", "Split Down", SplitFocusedMsg{Zone: layout.ZoneBottom}),
			appCommand("pane.splitUp", "Split Up", SplitFocusedMsg{Zone: layout.ZoneTop}),
			appCommand("pane.splitRight", "Split Right", SplitFocusedMsg{Zone: layout.ZoneRight}),
			appCommand("pane.splitLeft", "Split Left", SplitFocusedMsg{Zone: layout.ZoneLeft}),
			appCommand("editor.splitViewRight", "Split View Right", SplitViewMsg{Zone: layout.ZoneRight}),
			appCommand("editor.splitViewDown", "Split View Down", SplitViewMsg{Zone: layout.ZoneBottom}),
			appCommand("editor.pasteFromHistory", "Paste from History", ShowPasteHistoryMsg{}),
			appCommand("editor.forceCodeInsight", "Force Code Insight (Large File)", ForceCodeInsightMsg{}),
			appCommand("editor.largeFileDetails", "Large File Details", LargeFileDetailsMsg{}),
			appCommand("pane.maximize", "Maximize Pane", MaximizePaneMsg{}),
			appCommand("search.open", "Find in Pane", OpenSearchMsg{}),
			appCommand("pane.resizeMode", "Resize Pane (Keyboard Mode)", PaneResizeModeMsg{}),
			appCommand("pane.close", "Close Pane", ClosePaneMsg{}),
			appCommand("view.zenMode", "Zen Mode", ZenModeMsg{}),
			appCommand("view.exportScreenshot", "Export Screenshot (Pane)", ExportScreenshotMsg{}),
			appCommand("view.exportWindowScreenshot", "Export Screenshot (Window)", ExportScreenshotMsg{Whole: true}),
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
			appCommand("deps.toggle", "Dependencies", DepsToggleMsg{}),
			appCommand("time.toggle", "Project Time Report", TimeToggleMsg{}),
			appCommand("time.refresh", "Project Time: Reload Usage Log", TimeRefreshMsg{}),
			appCommand("deps.refresh", "Dependencies: Refresh Scan", DepsRefreshMsg{}),
			appCommand("deps.audit", "Dependencies: Audit Vulnerabilities", DepsAuditMsg{}),
			appCommand("deps.updateLatest", "Update Dependency to Latest", DepsUpdateLatestMsg{}),
			appCommand("structure.toggle", "Structure", StructureToggleMsg{}),
			appCommand("dom.toggle", "DOM Inspector", DOMToggleMsg{}),
			appCommand("scratch.panel", "Scratch Files", ScratchSectionFocusMsg{}),
			appCommand("usages.toggle", "Usages", UsagesToggleMsg{}),
			appCommand(findPanelCommand, "Open Results in Find Window", OpenInFindPanelMsg{}),
			appCommand("tests.toggle", "Test Results", TestsToggleMsg{}),
			appCommand("issues.toggle", "GitHub Issues", IssuesToggleMsg{}),
			paneCommand("data.columnProfile", "Data: Column Profile", "data", DataColumnProfileMsg{}),
			paneCommand("data.sortColumn", "Data: Sort Column", "data", DataSortColumnMsg{}),
			paneCommand("data.export", "Data: Export Rows…", "data", DataExportMsg{}),
			langCommand(paneCommand("csv.columnProfile", "CSV: Column Profile", "editor", CSVColumnProfileMsg{}), []string{"csv", "tsv", "psv"}),
			appCommand("diff.nextChange", "Next Change (Diff)", DiffStepMsg{Delta: 1}),
			appCommand("diff.prevChange", "Previous Change (Diff)", DiffStepMsg{Delta: -1}),
		), append(append(append(append(scratchCommands(), toolCommands()...), memoryCommands()...), perfCommands()...), esCommands()...)...),
	}
}

func init() { registry.Register(appCommands{}) }
