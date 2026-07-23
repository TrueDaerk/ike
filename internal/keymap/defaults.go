package keymap

// PresetJetBrains is the default binding preset name.
const PresetJetBrains = "jetbrains"

// row is the compact source form of a default binding before it is parsed.
type row struct {
	chord   string
	command string
	title   string
	ctx     Context
	owner   string
}

// jetbrainsRows is the JetBrains-flavoured default set (Roadmap 0080's table).
// Each row binds a chord to a command id owned by another roadmap; commands not
// yet registered make the binding inert until their owner lands. Chords use
// logical Cmd; platform.go maps Cmd→Ctrl off macOS at build time.
var jetbrainsRows = []row{
	{"cmd+k", "vcs.commit", "Commit", Global, "VCS (future)"},
	{"cmd+t", "vcs.updateProject", "Update Project", Global, "VCS (future)"},
	{"cmd+d", "editor.duplicateLine", "Duplicate line(s)", Editor, "Editor (06)"},
	{"cmd+shift+a", "palette.searchEverywhere", "Search everywhere", Global, "Palette (07)"},
	{"shift shift", "palette.searchEverywhere", "Search everywhere (double-shift)", Global, "Palette (07)"},
	{"cmd+shift+o", "project.goToFile", "Go to file", Global, "Project (09)"},
	// cmd+shift+p mirrors JetBrains' Recent Projects popup (macOS keymap
	// export); ctrl+shift+p is the delivered secondary. The chord table
	// resolves modified chords even in a capturing editor, which the registry
	// keymap layer does not.
	{"cmd+shift+p", "project.switch", "Switch project", Global, "Project (0090)"},
	{"ctrl+shift+p", "project.switch", "Switch project", Global, "Project (0090)"},
	{"cmd+o", "project.goToClass", "Go to symbol/class", Global, "Project (09)/LSP (10)"},
	{"cmd+e", "palette.recentFiles", "Recent files", Global, "Palette (07)"},
	// Reconciled (#5): the LSP plugin registers find-usages as lsp.references;
	// the table uses the registered id (mirroring lsp.definition below).
	{"alt+f7", "lsp.references", "Find usages", Editor, "LSP (0100)"},
	// JetBrains next/previous difference in the diff viewer (0340, #495);
	// n/N remain the vim-flavored equivalents inside the pane.
	{"f7", "diff.nextChange", "Next change (diff)", Diff, "Diff (0340)"},
	{"shift+f7", "diff.prevChange", "Previous change (diff)", Diff, "Diff (0340)"},
	// JetBrains' call-hierarchy chord (#173).
	{"ctrl+alt+h", "lsp.callHierarchy", "Call hierarchy", Editor, "LSP (0100)"},
	// shift+f6 is JetBrains' context-aware refactor-rename (0082 sheet 13):
	// with an editor focused it renames the *symbol* at the cursor (LSP #6);
	// everywhere else the Global file.rename row owns the chord (explorer
	// selection, #175) — Lookup prefers the more specific context. File
	// rename with an editor focused stays reachable through the palette.
	{"shift+f6", "lsp.rename", "Rename symbol", Editor, "LSP (0100)"},
	{"shift+f6", "file.rename", "Rename file", Global, "App (#175)"},
	{"f6", "file.move", "Move file", Global, "App (#175)"},
	// Comment toggling binds cmd+7, not the JetBrains cmd+/: on a German layout
	// "/" lives on shift+7, so a cmd+/ chord is untypable there (idea #48).
	{"cmd+7", "editor.commentLine", "Comment line", Editor, "Editor (idea #48)"},
	{"cmd+shift+7", "editor.commentBlock", "Comment block", Editor, "Editor (idea #48)"},
	// Save gets both chords, mirroring the redo story below: cmd+s matches
	// JetBrains where the terminal can deliver it, ctrl+s is the
	// everywhere-deliverable fallback (raw mode disables XOFF flow control, so
	// ctrl+s arrives as a normal key).
	{"cmd+s", "editor.write", "Save", Editor, "Editor (06)"},
	{"ctrl+s", "editor.write", "Save", Editor, "Editor (06)"},
	{"cmd+shift+s", "editor.saveAll", "Save all", Global, "Editor (06)"},
	{"cmd+c", "editor.copy", "Copy", Editor, "Editor (06)"},
	{"cmd+x", "editor.cut", "Cut", Editor, "Editor (06)"},
	{"cmd+v", "editor.paste", "Paste", Editor, "Editor (06)"},
	// Undo binds to ctrl+z, not cmd+z: macOS terminals never forward the Cmd
	// modifier to a TUI, so a cmd+z chord is undeliverable there. ctrl+z arrives
	// as a normal key (raw mode disables the suspend signal) on every platform.
	{"ctrl+z", "editor.undo", "Undo", Editor, "Editor (06)"},
	{"ctrl+z", "explorer.undo", "Undo file operation", Explorer, "Explorer (05)"},
	// Redo gets both chords: cmd+shift+z matches JetBrains where the terminal
	// can deliver it, ctrl+shift+z is the everywhere-deliverable fallback
	// (mirroring the ctrl+z story above).
	{"cmd+shift+z", "editor.redo", "Redo", Editor, "Editor (06)"},
	{"ctrl+shift+z", "editor.redo", "Redo", Editor, "Editor (06)"},
	{"cmd+shift+z", "explorer.redo", "Redo file operation", Explorer, "Explorer (05)"},
	{"ctrl+shift+z", "explorer.redo", "Redo file operation", Explorer, "Explorer (05)"},
	{"cmd+f", "editor.find", "Find in file", Editor, "Editor (06)"},
	{"cmd+r", "editor.replace", "Replace in file", Editor, "Editor (06)"},
	{"cmd+shift+f", "project.findInPath", "Find in path", Global, "Project (09)"},
	{"cmd+shift+r", "project.replaceInPath", "Replace in path", Global, "Project (09)"},
	// Retained find-in-path match stepping (0150, #242): the JetBrains
	// next/previous-occurrence keys.
	{"f3", "search.nextMatch", "Next search match", Global, "Search (0150)"},
	{"shift+f3", "search.prevMatch", "Previous search match", Global, "Search (0150)"},
	// JetBrains "Select in Project View" (#242). Alt+F-key delivery depends on
	// the terminal (fragile); the palette stays the delivered fallback.
	{"alt+f1", "explorer.reveal", "Reveal open file in explorer", Global, "Explorer (05)"},
	{"cmd+left", "editor.lineStart", "Move to line start", Editor, "Editor (06)"},
	{"home", "editor.lineStart", "Move to line start", Editor, "Editor (06)"},
	{"cmd+right", "editor.lineEnd", "Move to line end", Editor, "Editor (06)"},
	{"cmd+left-bracket", "nav.back", "Navigate back", Global, "Editor (06)/app (01)"},
	{"cmd+right-bracket", "nav.forward", "Navigate forward", Global, "Editor (06)/app (01)"},
	// Mouse back/forward buttons (#816): synthetic single-step chords fed
	// through the resolver by the root model, so they rebind like keys.
	// Terminals without SGR extended buttons simply never deliver them.
	{"mouse-back", "nav.back", "Navigate back (mouse button 4)", Global, "Editor (06)/app (01)"},
	{"mouse-forward", "nav.forward", "Navigate forward (mouse button 5)", Global, "Editor (06)/app (01)"},
	// Reconciled (0081/20): the LSP plugin registers goto-definition as
	// lsp.definition; the table uses the registered id rather than forking an
	// editor.gotoDeclaration alias. f4 — JetBrains' jump-to-source — is the
	// delivered primary (0082 sheet 11); cmd+b stays as the JetBrains chord
	// for terminals that can deliver Cmd (macOS terminals never forward it).
	{"f4", "lsp.definition", "Go to declaration", Editor, "LSP (0100)"},
	{"cmd+b", "lsp.definition", "Go to declaration", Editor, "LSP (0100)"},
	// JetBrains quick documentation (#378). ctrl+q is the Windows/Linux
	// JetBrains chord and delivered everywhere: raw mode disables XON flow
	// control, so ctrl+q arrives as a normal key (mirroring the ctrl+s story
	// above). f1 — the macOS JetBrains quick-doc key — is taken by the
	// cheatsheet.
	{"ctrl+q", "lsp.hover", "Quick documentation", Editor, "LSP (0100)"},
	// JetBrains error description (#739): ctrl+f1 shows the caret line's
	// diagnostics — message, severity, source, rule code. Modified F-keys
	// deliver under the Kitty keyboard protocol (the 0081 reality probe).
	{"ctrl+f1", "lsp.diagnosticInfo", "Diagnostic under caret", Editor, "LSP (0100)"},
	// JetBrains parameter info (#523). cmd+p matches JetBrains where the
	// terminal can deliver Cmd; ctrl+p is the everywhere-deliverable fallback
	// (the palette's former default toggle chord — palette.toggle_key now
	// defaults to empty; esc-esc, "@" and search-everywhere stay). Off macOS
	// both rows collapse to one ctrl+p binding.
	{"cmd+p", "lsp.parameterInfo", "Parameter info", Editor, "LSP (0100)"},
	{"ctrl+p", "lsp.parameterInfo", "Parameter info", Editor, "LSP (0100)"},
	// JetBrains next/previous highlighted error (#369). f2 and shift+f2 are
	// both delivered (shift+fN carries its modifier in the CSI parameter).
	{"f2", "lsp.nextDiagnostic", "Next diagnostic", Editor, "LSP (#369)"},
	{"shift+f2", "lsp.prevDiagnostic", "Previous diagnostic", Editor, "LSP (#369)"},
	// JetBrains reformat-code. The L is layout-safe on QWERTZ; the selection
	// variant keys off the active visual selection inside lsp.formatRange.
	{"cmd+alt+l", "lsp.format", "Reformat file", Editor, "LSP (0100)"},
	// JetBrains intention actions. Alt+enter delivery depends on the
	// terminal's option-as-meta setting, hence fragile; 0081 owns the final
	// reachability call.
	{"alt+enter", "lsp.codeAction", "Show intention actions", Editor, "LSP (0100)"},
	{"cmd+1", "explorer.toggle", "Toggle project tree", Global, "Explorer (05)"},
	// Pinned file slots (#788), the IntelliJ mnemonic-bookmark spirit.
	// ctrl+digit is unavailable (cmd+digit tool-window chords fold onto it on
	// Linux), so jumps sit on ctrl+shift+digit — digits are identical on
	// QWERTZ; delivery needs the Kitty protocol like the other ctrl+shift
	// chords, with the palette as the documented escape. cmd+2 mirrors
	// JetBrains' Bookmarks tool window for the picker; pinning itself goes
	// through the palette or the picker's `p` key.
	{"ctrl+shift+1", "nav.pinGoto1", "Go to pinned file 1", Global, "Pinned files (#788)"},
	{"ctrl+shift+2", "nav.pinGoto2", "Go to pinned file 2", Global, "Pinned files (#788)"},
	{"ctrl+shift+3", "nav.pinGoto3", "Go to pinned file 3", Global, "Pinned files (#788)"},
	{"ctrl+shift+4", "nav.pinGoto4", "Go to pinned file 4", Global, "Pinned files (#788)"},
	{"cmd+2", "nav.pins", "Pinned files", Global, "Pinned files (#788)"},
	// JetBrains Hide All Tool Windows (#791).
	{"cmd+shift+f12", "window.hideAllTools", "Hide all tool windows", Global, "Windowing (#791)"},
	{"ctrl+tab", "pane.switcher", "Switch pane focus", Global, "App (01)"},
	{"cmd+w", "editor.closeTab", "Close active tab", Global, "Editor (06)"},
	// Editor tabs (0190, #158). Alt+digits jump straight to a tab (digits sit
	// identically on QWERTZ). Tab cycling mirrors JetBrains' macOS keymap
	// export: Next/Previous Tab = ctrl+cmd+arrow (primary) and ctrl+alt+arrow
	// (secondary). Delivery to a TUI needs a terminal that forwards Cmd/Option
	// (Ghostty with the Kitty protocol) — accepted per user preference; the
	// ctrl+shift+pgup/pgdn move-tab pair stays for tab reordering.
	{"ctrl+cmd+right", "editor.tab.next", "Next tab", Global, "Editor tabs (0190)"},
	{"ctrl+alt+right", "editor.tab.next", "Next tab", Global, "Editor tabs (0190)"},
	{"ctrl+cmd+left", "editor.tab.prev", "Previous tab", Global, "Editor tabs (0190)"},
	{"ctrl+alt+left", "editor.tab.prev", "Previous tab", Global, "Editor tabs (0190)"},
	{"ctrl+shift+pgdown", "editor.tab.moveRight", "Move tab right", Global, "Editor tabs (0190)"},
	{"ctrl+shift+pgup", "editor.tab.moveLeft", "Move tab left", Global, "Editor tabs (0190)"},
	// Reopen closed tab: cmd+shift+t is the JetBrains chord; alt+shift+t stays
	// as the secondary. (vcs.revertFile moved to JetBrains' rollback chord
	// cmd+alt+z to free the primary, #711.)
	{"cmd+shift+t", "editor.tab.reopenClosed", "Reopen closed tab", Global, "Editor tabs (0190)"},
	{"alt+shift+t", "editor.tab.reopenClosed", "Reopen closed tab", Global, "Editor tabs (0190)"},
	{"alt+1", "editor.tab.select1", "Go to tab 1", Global, "Editor tabs (0190)"},
	{"alt+2", "editor.tab.select2", "Go to tab 2", Global, "Editor tabs (0190)"},
	{"alt+3", "editor.tab.select3", "Go to tab 3", Global, "Editor tabs (0190)"},
	{"alt+4", "editor.tab.select4", "Go to tab 4", Global, "Editor tabs (0190)"},
	{"alt+5", "editor.tab.select5", "Go to tab 5", Global, "Editor tabs (0190)"},
	{"alt+6", "editor.tab.select6", "Go to tab 6", Global, "Editor tabs (0190)"},
	{"alt+7", "editor.tab.select7", "Go to tab 7", Global, "Editor tabs (0190)"},
	{"alt+8", "editor.tab.select8", "Go to tab 8", Global, "Editor tabs (0190)"},
	{"alt+9", "editor.tab.select9", "Go to tab 9", Global, "Editor tabs (0190)"},
	// JetBrains' rollback chord (cmd+alt+z); cmd+shift+t went to reopen-closed
	// above (#711).
	{"cmd+alt+z", "vcs.revertFile", "Revert file", Global, "VCS (future)"},
	// JetBrains Version Control tool window (#711).
	{"cmd+9", "vcs.panel", "Toggle VCS tool window", Global, "VCS (0320)"},
	// The cmd+k sequence family below is the deliberate multi-step exception
	// set (#711): pane splits plus maximize, five sequences total. Everything
	// else binds a single modifier chord.
	{"cmd+k down", "pane.splitDown", "Split down", Global, "App (01)"},
	{"cmd+k up", "pane.splitUp", "Split up", Global, "App (01)"},
	{"cmd+k right", "pane.splitRight", "Split right", Global, "App (01)"},
	{"cmd+k left", "pane.splitLeft", "Split left", Global, "App (01)"},
	{"cmd+k z", "pane.maximize", "Maximize pane", Global, "Zen & maximize (#358)"},
	// Distraction-free toggle (#934): a single delivered chord so zen also
	// works from a focused terminal/tool pane (multi-step sequences cannot be
	// intercepted there — see terminalGlobalChord).
	{"ctrl+alt+f", "view.zenMode", "Zen mode", Global, "Zen & maximize (#358)"},
	{"cmd+shift+v", "editor.pasteFromHistory", "Paste from history", Editor, "Paste history (#57)"},
	// Multi-caret (#145): JetBrains' ctrl+g occurrence walk plus a deliverable
	// select-all-occurrences chord (the JetBrains original needs alt).
	{"ctrl+g", "editor.caret.addNext", "Add caret at next occurrence", Editor, "Multi-caret (#145)"},
	{"ctrl+shift+g", "editor.caret.addAll", "Add carets at all occurrences", Editor, "Multi-caret (#145)"},
	// Rendered markdown preview (#62): single chord since #711 (was cmd+k m).
	{"cmd+alt+m", "markdown.preview", "Markdown preview", Editor, "Markdown preview (#62)"},
	// TODO index (#61): cmd+6 is JetBrains' TODO tool-window chord.
	{"cmd+6", "todo.list", "TODO index", Global, "TODO index (#61)"},
	{"cmd+alt+shift+right", "editor.splitViewRight", "Split view right", Global, "Split view (#147)"},
	{"cmd+alt+shift+down", "editor.splitViewDown", "Split view down", Global, "Split view (#147)"},
	{"f1", "palette.keymapHelp", "Help / cheatsheet", Global, "Keymap (08)"},
	// JetBrains terminal toggle. Alt+F-key delivery depends on the terminal,
	// hence fragile; inside a focused terminal the reserved-set handler picks
	// it up before the chord layer (raw pass-through).
	{"alt+f12", "terminal.toggle", "Toggle terminal", Global, "Terminal (0170)"},
	// New terminal session and notification history: single chords since the
	// leader layer retired (#711); JetBrains has no defaults for either.
	{"cmd+alt+t", "terminal.new", "New terminal", Global, "Terminal (0170)"},
	{"cmd+alt+n", "notifications.history", "Notification history", Global, "Notifications (#242)"},
	// JetBrains Run (Windows keymap's shift+f10; macOS ctrl+r would shadow
	// vim's redo in the editor, so the F-key is the delivered primary, 0350).
	{"shift+f10", "run.file", "Run file", Global, "Run (0350)"},
	// JetBrains toggle breakpoint (ctrl+f8 on every platform's keymap).
	{"ctrl+f8", "debug.toggleBreakpoint", "Toggle breakpoint", Global, "Run (0350)"},
	// JetBrains debug chords, identical across platforms: shift+f9 debug,
	// F8/F7/shift+F8/F9 stepping (no-ops without a paused session; the diff
	// pane's context-scoped f7 stays more specific and wins there).
	{"shift+f9", "debug.start", "Debug file", Global, "Run (0350)"},
	{"f8", "debug.stepOver", "Step over", Global, "Run (0350)"},
	{"f7", "debug.stepInto", "Step into", Global, "Run (0350)"},
	{"shift+f8", "debug.stepOut", "Step out", Global, "Run (0350)"},
	{"f9", "debug.continue", "Continue (debug)", Global, "Run (0350)"},
	{"f10", "menu.open", "Open menu bar", Global, "Menu (0160)"},
	{"cmd+,", "settings.open", "Settings", Global, "Menu (0160)"},
}

// Defaults returns the default binding set for the named preset. Unknown presets
// fall back to JetBrains. Chords are parsed but not yet platform-normalised;
// BuildTable normalises them for the target goos.
func Defaults(preset string) []Binding {
	// Only one preset exists today; reserved for future presets (vscode, etc.).
	rows := jetbrainsRows
	out := make([]Binding, 0, len(rows))
	for _, r := range rows {
		chord := MustParseChord(r.chord)
		out = append(out, Binding{
			Chord:   chord,
			Command: r.command,
			Context: r.ctx,
			Title:   r.title,
			Owner:   r.owner,
			// Honest by construction (0081/10+30): fragility derives from the
			// reachability table instead of hand-maintained flags.
			Fragile: Classify(chord) != Delivered,
			Layer:   LayerDefault,
		})
	}
	return out
}
