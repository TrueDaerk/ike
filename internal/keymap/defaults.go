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
	// vcs.commit (cmd+k) and vcs.updateProject (cmd+t) were removed in #750:
	// git workflow is delegated to custom tool panes (lazygit). cmd+k lives
	// on solely as the prefix of the pane-split sequence family below.
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
	// Switch to last project (#2398): the alt+tab between the two projects a
	// session ping-pongs between — cmd+shift+e with the delivered ctrl
	// secondary, same pattern as project.switch above. It sits next to cmd+e
	// (recent files), the palette detour it replaces.
	{"cmd+shift+e", "project.switchLast", "Switch to last project", Global, "Project (#2398)"},
	{"ctrl+shift+e", "project.switchLast", "Switch to last project", Global, "Project (#2398)"},
	// Close Project (#1355/#1358): cmd+shift+w with the delivered ctrl
	// secondary, same pattern as project.switch above.
	{"cmd+shift+w", "project.close", "Close project", Global, "Project (#1355)"},
	{"ctrl+shift+w", "project.close", "Close project", Global, "Project (#1355)"},
	// Return from a quick-peek (#2136): cmd+shift+b ("back") with the
	// delivered ctrl secondary, same pattern as project.switch above.
	{"cmd+shift+b", "project.peek.return", "Return from peek", Global, "Project (#2136)"},
	{"ctrl+shift+b", "project.peek.return", "Return from peek", Global, "Project (#2136)"},
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
	// Inheritance navigation (#1455), JetBrains-macOS chords verbatim: cmd+u
	// go to super, cmd+alt+b go to implementations, ctrl+h type hierarchy.
	{"cmd+u", "lsp.goToSuper", "Go to super", Editor, "LSP (#1455)"},
	{"cmd+alt+b", "lsp.implementations", "Go to implementations", Editor, "LSP (#1455)"},
	{"ctrl+h", "lsp.typeHierarchy", "Type hierarchy", Editor, "LSP (#1455)"},
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
	// The copy chord outside the editor (#2315): telemetry recorded cmd+c
	// unbound in the response viewer and the explorer, where the panes either
	// handled it silently (http) or did nothing (explorer). Binding it makes
	// the meaning explicit, listed and rebindable. http.copyResponse is the
	// pane's own copy key — the selection when there is one, else the whole
	// body; the explorer has no file-copy/paste pair, so its copy is the
	// selected entry's path. No ctrl+c secondary: on macOS ctrl+c stays the
	// global quit chord (a selection-less press must keep quitting, #2062),
	// and off macOS these rows already normalise to ctrl+c anyway, exactly
	// like the editor's cmd+c row above.
	{"cmd+c", "http.copyResponse", "Copy response selection or body", HTTP, "HTTP client (#2315)"},
	{"cmd+c", "file.copyPath", "Copy path of the selected entry", Explorer, "Explorer (#2315)"},
	// The same story one audit later (#2400): cmd+c in the debug panel and in
	// the issues window. Neither pane could copy at all (the issues window
	// only copied a mouse selection), so the chord was recorded unbound; the
	// commands name what "copy" means in each pane. No ctrl+c secondary, for
	// the reason above — it stays the global quit chord on macOS.
	{"cmd+c", "debug.copy", "Copy selected value", Debug, "Debugger (#2400)"},
	{"cmd+c", "issues.copy", "Copy issue reference", Issues, "GitHub issues (#2400)"},
	// JetBrains' Copy Reference chord, applied to the position *inside* a
	// JSON/YAML document rather than to the file (#1660). The jq and yq
	// flavours stay palette-only — one chord for the everyday form.
	{"cmd+alt+shift+c", "editor.copyDocPath", "Copy JSON/YAML path", Editor, "Editor (#1660)"},
	{"cmd+x", "editor.cut", "Cut", Editor, "Editor (06)"},
	{"cmd+v", "editor.paste", "Paste", Editor, "Editor (06)"},
	// Select All (#1861), JetBrains chord verbatim: selects the whole buffer
	// as a linewise visual selection (the "ggVG" equivalent).
	{"cmd+a", "editor.selectAll", "Select All", Editor, "Editor (06)"},
	// Undo gets both chords (#1117): cmd+z matches JetBrains/macOS muscle
	// memory where the terminal delivers Cmd (Kitty keyboard protocol —
	// Ghostty, kitty, WezTerm …), ctrl+z is the everywhere-deliverable
	// fallback (raw mode disables the suspend signal on every platform) —
	// the same dual-chord pattern save and redo already use.
	{"cmd+z", "editor.undo", "Undo", Editor, "Editor (06)"},
	{"ctrl+z", "editor.undo", "Undo", Editor, "Editor (06)"},
	{"cmd+z", "explorer.undo", "Undo file operation", Explorer, "Explorer (05)"},
	{"ctrl+z", "explorer.undo", "Undo file operation", Explorer, "Explorer (05)"},
	// Redo gets both chords: cmd+shift+z matches JetBrains where the terminal
	// can deliver it, ctrl+shift+z is the everywhere-deliverable fallback
	// (mirroring the ctrl+z story above).
	{"cmd+shift+z", "editor.redo", "Redo", Editor, "Editor (06)"},
	{"ctrl+shift+z", "editor.redo", "Redo", Editor, "Editor (06)"},
	{"cmd+shift+z", "explorer.redo", "Redo file operation", Explorer, "Explorer (05)"},
	{"ctrl+shift+z", "explorer.redo", "Redo file operation", Explorer, "Explorer (05)"},
	{"cmd+f", "editor.find", "Find in file", Editor, "Editor (06)"},
	// Find in the focused pane (#2409): the same chord opens whatever the
	// focused pane calls its search — the explorer speed search, a list
	// pane's filter row, the response viewer's prompt. It is Global, so the
	// Editor-scoped editor.find above shadows it inside an editor.
	//
	// ctrl+f is deliberately NOT bound here: it is vim's page-forward
	// motion in the editor (including operator-pending "d ctrl+f"), and a
	// Global binding would resolve ahead of the editor and swallow it.
	// The panes accept ctrl+f themselves instead (ui.FindChord), which
	// reaches them intact precisely because the chord stays unbound.
	{"cmd+f", "search.open", "Find in pane", Global, "Search (#2409)"},
	{"cmd+r", "editor.replace", "Replace in file", Editor, "Editor (06)"},
	{"cmd+shift+f", "project.findInPath", "Find in path", Global, "Project (09)"},
	{"cmd+shift+r", "project.replaceInPath", "Replace in path", Global, "Project (09)"},
	// Find in All Projects (#2394): one modifier layer above find-in-path,
	// like copyDocPath sits above copy. The results chord mirrors it on r.
	{"cmd+alt+shift+f", "project.findInAllProjects", "Find in all projects", Global, "Project (#2394)"},
	{"cmd+alt+shift+r", "project.findInAllProjectsResults", "Show all-projects search results", Global, "Project (#2394)"},
	// Retained find-in-path match stepping (0150, #242): the JetBrains
	// next/previous-occurrence keys.
	{"f3", "search.nextMatch", "Next search match", Global, "Search (0150)"},
	{"shift+f3", "search.prevMatch", "Previous search match", Global, "Search (0150)"},
	// IntelliJ macOS Find Next/Previous aliases (#2359).
	{"cmd+g", "search.nextMatch", "Next search match", Global, "Search (0150)"},
	{"cmd+shift+g", "search.prevMatch", "Previous search match", Global, "Search (0150)"},
	// JetBrains "Select in Project View" (#242). Alt+F-key delivery depends on
	// the terminal (fragile); the palette stays the delivered fallback.
	{"alt+f1", "explorer.reveal", "Reveal open file in explorer", Global, "Explorer (05)"},
	{"cmd+left", "editor.lineStart", "Move to line start", Editor, "Editor (06)"},
	{"home", "editor.lineStart", "Move to line start", Editor, "Editor (06)"},
	{"cmd+right", "editor.lineEnd", "Move to line end", Editor, "Editor (06)"},
	// The #2400 line/document family: chords telemetry recorded as unbound in
	// the editor because the commands did not exist at all (lineops.go).
	//
	// Move Line Up/Down keeps JetBrains' cmd+shift+arrow as the darwin
	// primary and adds ctrl+shift+arrow, which is delivered everywhere (the
	// legacy CSI encoding carries the modifier bitset on arrows, so no
	// ctrl+shift collapse). JetBrains' *other* chord for it, alt+shift+up /
	// down, stays with caret cloning (#1481) — it has been bound there since
	// before this audit, and one gesture cannot mean two things.
	{"cmd+shift+up", "editor.moveLineUp", "Move line up", Editor, "Editor (#2400)"},
	{"cmd+shift+down", "editor.moveLineDown", "Move line down", Editor, "Editor (#2400)"},
	{"ctrl+shift+up", "editor.moveLineUp", "Move line up", Editor, "Editor (#2400)"},
	{"ctrl+shift+down", "editor.moveLineDown", "Move line down", Editor, "Editor (#2400)"},
	// Delete Line and the backward word kill, IntelliJ's chords verbatim.
	// Both already worked inside an open insert session (#955, #246); the
	// rows make them commands, so they work in normal mode too and are
	// listed, rebindable and palette-reachable. vim dd / db stay the
	// delivered fallback (matrix.go).
	{"cmd+backspace", "editor.deleteLine", "Delete line", Editor, "Editor (#2400)"},
	{"alt+backspace", "editor.deleteWordBackward", "Delete word backward", Editor, "Editor (#2400)"},
	// Document start/end: the cmd form is the macOS convention (and what the
	// telemetry saw pressed), the ctrl form is JetBrains' Windows/Linux
	// scheme and delivered everywhere. The macOS text-field twins cmd+up /
	// cmd+down are deliberately left alone: off macOS they fold onto
	// ctrl+up / ctrl+down, which are the editor's paragraph jumps.
	{"cmd+home", "editor.docStart", "Go to document start", Editor, "Editor (#2400)"},
	{"cmd+end", "editor.docEnd", "Go to document end", Editor, "Editor (#2400)"},
	{"ctrl+home", "editor.docStart", "Go to document start", Editor, "Editor (#2400)"},
	{"ctrl+end", "editor.docEnd", "Go to document end", Editor, "Editor (#2400)"},
	// Select to the line edge on shift+home / shift+end, both delivered. The
	// cmd+shift+left/right pair is deliberately *not* bound: it is JetBrains'
	// macOS chord for the same pair, but here the navigation family already
	// layers on the cmd+shift arrows' neighbourhood, and nav.back/forward
	// keep their chords (#2361).
	{"shift+home", "editor.selectLineStart", "Select to line start", Editor, "Editor (#2400)"},
	{"shift+end", "editor.selectLineEnd", "Select to line end", Editor, "Editor (#2400)"},
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
	// JetBrains error description (#739): shows the caret line's diagnostics —
	// message, severity, source, rule code. cmd+f1 is the macOS-keymap chord
	// and the darwin primary (#1374: macOS system shortcuts swallow plain
	// ctrl+F-keys before the terminal sees them); ctrl+f1 is the Windows-scheme
	// form and the delivered chord off macOS (both fold together there).
	{"cmd+f1", "lsp.diagnosticInfo", "Diagnostic under caret", Editor, "LSP (0100)"},
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
	// JetBrains reformat-code. The L is layout-safe on QWERTZ. The command is
	// context-sensitive (#1603): an active visual selection reformats only the
	// selected range, no selection reformats the whole file.
	// The id predates the formatter registry (0470): the command now resolves
	// config override → external tool → LSP → built-in, not only LSP.
	{"cmd+alt+l", "lsp.format", "Reformat file or selection", Editor, "Format (0470)"},
	// cmd+shift+l is what a JetBrains user with a customised keymap reached
	// for (#2400) — the chord was free, and reformatting is the only thing
	// "L" means in this table, so it aliases the same command rather than
	// growing a second meaning.
	{"cmd+shift+l", "lsp.format", "Reformat file or selection", Editor, "Format (#2400)"},
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
	{"shift+f12", "window.restoreLayout", "Restore default layout", Global, "Windowing (#1175)"},
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
	// Tab picker (#2151): the pane's tabs most-recently-used first, JetBrains'
	// Switcher role. cmd+e is taken by the recent-files palette, so the free
	// alt+e carries it; the palette is the delivered fallback.
	{"alt+e", "editor.tab.picker", "Switch tab (recent tabs)", Global, "Editor tabs (0190)"},
	{"cmd+shift+t", "editor.tab.reopenClosed", "Reopen closed tab", Global, "Editor tabs (0190)"},
	{"alt+shift+t", "editor.tab.reopenClosed", "Reopen closed tab", Global, "Editor tabs (0190)"},
	// Follow mode (#1928): tail -f for the open file, less-F style.
	{"alt+shift+f", "view.toggleFollow", "Toggle follow (tail -f)", Editor, "Follow mode (#1928)"},
	// Live filter over the tailed output (#2255); highlight-only mode and the
	// clear command stay palette-reachable.
	{"alt+shift+g", "view.followFilter", "Filter followed output", Editor, "Follow mode (#2255)"},
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
	// Keyboard pane resize mode (#2150): a single delivered-style chord, not
	// a cmd+k sequence — the sequence family is capped at five (#711), and a
	// single chord also reaches the mode from a focused terminal/tool pane
	// (see terminalGlobalChord). r for resize, joining the ctrl+alt family.
	{"ctrl+alt+r", "pane.resizeMode", "Resize pane (keyboard mode)", Global, "Pane resize mode (#2150)"},
	// Distraction-free toggle (#934): a single delivered chord so zen also
	// works from a focused terminal/tool pane (multi-step sequences cannot be
	// intercepted there — see terminalGlobalChord).
	{"ctrl+alt+f", "view.zenMode", "Zen mode", Global, "Zen & maximize (#358)"},
	// Performance HUD (#1999): JetBrains has no equivalent, so the chord is
	// IKE's own — p for performance, next to the other ctrl+alt view toggles.
	// Global so it also opens from a focused terminal or tool pane, which is
	// exactly where an "is this pane burning CPU" question comes up.
	{"ctrl+alt+p", "perf.hud", "Performance HUD", Global, "Performance HUD (#1999)"},
	// The playground's multi-line view (#2032, edited in since #2038; both
	// dialects since #2039). The playground owns the
	// keyboard while its pane is focused and resolves the keys it does not
	// claim against the Global scope (#1983), so a Global row is what makes the
	// toggle reachable from both the query line and the result buffer — and
	// rebindable, unlike the mode's hard-wired ctrl+s/ctrl+l. ctrl+alt+e joins
	// the ctrl+alt view-toggle family above; e for expand.
	{"ctrl+alt+e", "json.jqQueryView", "Full query view", Global, "jq/yq playground (#2032)"},
	{"cmd+shift+v", "editor.pasteFromHistory", "Paste from history", Editor, "Paste history (#57)"},
	// Multi-caret (#145): JetBrains' ctrl+g occurrence walk plus a deliverable
	// select-all-occurrences chord (the JetBrains original needs alt).
	{"ctrl+g", "editor.caret.addNext", "Add caret at next occurrence", Editor, "Multi-caret (#145)"},
	{"ctrl+shift+g", "editor.caret.addAll", "Add carets at all occurrences", Editor, "Multi-caret (#145)"},
	// Caret column cloning (#1481): the JetBrains clone-caret gesture. Arrows
	// carry the alt+shift modifiers in the legacy CSI encoding, so delivery is
	// broad despite the fragile alt class; the palette stays the fallback.
	{"alt+shift+up", "editor.caret.addAbove", "Clone caret above", Editor, "Multi-caret (#1481)"},
	{"alt+shift+down", "editor.caret.addBelow", "Clone caret below", Editor, "Multi-caret (#1481)"},
	// Syntax-aware extend/shrink selection (#1912), the JetBrains alt+up /
	// alt+down pair: grow the selection to the enclosing syntax node, shrink
	// back down the same ladder.
	{"alt+up", "editor.selection.extend", "Extend selection", Editor, "Selection (#1912)"},
	{"alt+down", "editor.selection.shrink", "Shrink selection", Editor, "Selection (#1912)"},
	// Escape / unescape the selection (#2338): "e" escapes, "u" unescapes,
	// sharing the cmd+alt+shift layer of the other selection-rewriting
	// commands. The plain cmd+alt+e/u layer is left free — it folds onto
	// ctrl+alt+e off macOS, where the jq/yq query view already sits. Both are
	// editor-scoped and act on the selection or the literal under the caret;
	// the palette is the delivered fallback (matrix.go).
	{"cmd+alt+shift+e", "editor.escapeSelection", "Escape selection as unicode", Editor, "Escapes (#2338)"},
	{"cmd+alt+shift+u", "editor.unescapeSelection", "Unescape selection", Editor, "Escapes (#2338)"},
	// Rendered markdown preview (#62): single chord since #711 (was cmd+k m).
	{"cmd+alt+m", "markdown.preview", "Markdown preview", Editor, "Markdown preview (#62)"},
	// TODO index (#61): cmd+6 is JetBrains' TODO tool-window chord.
	{"cmd+6", "todo.list", "TODO index", Global, "TODO index (#61)"},
	// Editor-scoped since the #1794 context audit: splitting the focused
	// editor's view is meaningless in every other pane.
	{"cmd+alt+shift+right", "editor.splitViewRight", "Split view right", Editor, "Split view (#147)"},
	{"cmd+alt+shift+down", "editor.splitViewDown", "Split view down", Editor, "Split view (#147)"},
	{"f1", "palette.keymapHelp", "Help / cheatsheet", Global, "Keymap (08)"},
	// JetBrains terminal toggle. Alt+F-key delivery depends on the terminal,
	// hence fragile; inside a focused terminal the reserved-set handler picks
	// it up before the chord layer (raw pass-through).
	{"alt+f12", "terminal.toggle", "Toggle terminal", Global, "Terminal (0170)"},
	// Popup terminal (#1398): the floating tab-host terminal overlay. cmd+alt+t
	// took this chord from terminal.new, which moved to cmd+alt+shift+t —
	// the quick toggle earns the shorter chord. Inside the popup (and inside
	// focused pane terminals) the reserved-set handlers intercept it before
	// raw pass-through.
	{"cmd+alt+t", "terminal.popup", "Popup terminal", Global, "Terminal (#1398)"},
	// Per-context ctrl+t (#1794), the showcase of one chord doing the
	// pane-appropriate thing per context: a new terminal tab with a terminal
	// focused, a new empty editor tab with an editor focused. Disjoint
	// contexts, so neither row conflicts with nor shadows the other; in every
	// other pane the chord stays unbound. The terminal row is deliberately
	// carved out of the shell forwarding (readline's rarely-used
	// transpose-chars loses to the tab chord — iTerm and JetBrains both spend
	// this position on new-tab); `keymap.bindings."terminal.ctrl+t" = ""`
	// hands it back to the shell.
	{"ctrl+t", "terminal.newTab", "New terminal tab", Terminal, "Terminal (#1794)"},
	{"ctrl+t", "editor.tab.new", "New empty editor tab", Editor, "Editor tabs (#1794)"},
	// New terminal session and notification history: single chords since the
	// leader layer retired (#711); JetBrains has no defaults for either.
	{"cmd+alt+shift+t", "terminal.new", "New terminal", Global, "Terminal (0170)"},
	{"cmd+alt+n", "notifications.history", "Notification history", Global, "Notifications (#242)"},
	// New file / scratch (#1145): cmd+n mirrors JetBrains' New-in-project-view
	// — the prompt targets the explorer selection and works with an editor
	// focused since #374; cmd+shift+n is JetBrains' New Scratch File verbatim.
	{"cmd+n", "explorer.newFile", "New file", Global, "Explorer (05)"},
	{"cmd+shift+n", "scratch.new", "New scratch file", Global, "Scratch files (#151)"},
	// JetBrains Run (Windows keymap's shift+f10; macOS ctrl+r would shadow
	// vim's redo in the editor, so the F-key is the delivered primary, 0350).
	{"shift+f10", "run.file", "Run file", Global, "Run (0350)"},
	// JetBrains toggle breakpoint: cmd+f8 is the macOS-keymap chord and the
	// darwin primary (#1374 — plain ctrl+F-keys are macOS system shortcuts and
	// never reach the terminal); ctrl+f8 is the Windows-scheme form, delivered
	// off macOS (both fold together there).
	{"cmd+f8", "debug.toggleBreakpoint", "Toggle breakpoint", Global, "Run (0350)"},
	{"ctrl+f8", "debug.toggleBreakpoint", "Toggle breakpoint", Global, "Run (0350)"},
	// JetBrains Breakpoints dialog (cmd+shift+f8 on the macOS keymap),
	// mirrored verbatim (#1377): the breakpoints list tool window.
	{"cmd+shift+f8", "debug.breakpoints", "Breakpoints", Global, "Run (0350)"},
	// Breakpoint properties (#2245): the toggle chord plus alt, in both the
	// macOS and the Windows scheme like the toggle itself — condition, hit
	// count and log message of the cursor line's breakpoint.
	{"cmd+alt+f8", "debug.breakpointProperties", "Breakpoint properties", Global, "Run (0350)"},
	{"ctrl+alt+f8", "debug.breakpointProperties", "Breakpoint properties", Global, "Run (0350)"},
	// JetBrains debug chords, identical across platforms: shift+f9 debug,
	// F8/F7/shift+F8/F9 stepping (no-ops without a paused session; the diff
	// pane's context-scoped f7 stays more specific and wins there).
	{"shift+f9", "debug.start", "Debug file", Global, "Run (0350)"},
	{"f8", "debug.stepOver", "Step over", Global, "Run (0350)"},
	{"f7", "debug.stepInto", "Step into", Global, "Run (0350)"},
	{"shift+f8", "debug.stepOut", "Step out", Global, "Run (0350)"},
	{"f9", "debug.continue", "Continue (debug)", Global, "Run (0350)"},
	// JetBrains Evaluate Expression (alt+f8 on both keymaps): the selection,
	// or a typed expression, evaluated in the paused frame (#2174).
	{"alt+f8", "debug.evaluate", "Evaluate expression", Global, "Run (0350)"},
	// JetBrains Windows-scheme Rerun and Stop (#1048), staying in the same
	// scheme as the run/debug F-key family above; both deliver everywhere.
	// JetBrains HTTP client run (#1250): cmd+enter dispatches the .http
	// request under the cursor, editor-scoped like the run marker it mirrors.
	{"cmd+enter", "http.run", "Run HTTP request", Editor, "HTTP client (0450)"},
	// ctrl+f9 is the everywhere-deliverable fallback (modified F-keys
	// deliver under the Kitty protocol; cmd+enter needs a Cmd-forwarding
	// terminal).
	{"ctrl+f9", "http.run", "Run HTTP request", Editor, "HTTP client (0450)"},
	// http.showResponse default keybinding (#1831): shift sibling of
	// http.run's chord above, same primary/fallback split, so looking at a
	// stored response without dispatching is one modifier away from running
	// the request.
	{"cmd+shift+enter", "http.showResponse", "Show stored HTTP response", Editor, "HTTP client (0450)"},
	{"ctrl+shift+f9", "http.showResponse", "Show stored HTTP response", Editor, "HTTP client (0450)"},
	// http.diffPreviousRun default keybinding (#2060): "d" for diff, mirroring
	// the response pane's own "D" single-key story one level up. Both chords
	// are Fragile (ctrl+shift+letter collapses without the Kitty protocol,
	// Cmd needs it outright) like several other letter-mnemonic commands
	// above; the palette is the documented, always-delivered fallback
	// (reachableAlternatives in matrix.go).
	{"cmd+shift+d", "http.diffPreviousRun", "Diff HTTP response against previous run", Editor, "HTTP client (0450)"},
	{"ctrl+shift+d", "http.diffPreviousRun", "Diff HTTP response against previous run", Editor, "HTTP client (0450)"},
	// ctrl+r is JetBrains' Rerun, and that is what users press in a focused
	// response pane (#2314): the pane already answered it with a re-send of
	// the shown response's stored request, but only as a pane-local key —
	// invisible in the keymap listing, the help and the settings page, and
	// logged as an unbound chord. The binding makes the same action the
	// table's, scoped to the response pane, where it is the only meaning
	// ctrl+r has. Delivered everywhere (plain ctrl+letter), so no fallback.
	{"ctrl+r", "http.resend", "Re-send stored HTTP request", HTTP, "HTTP client (#1832)"},
	// Search inside the response, the same story as ctrl+r above (#2400):
	// cmd+f was the single most-pressed unbound chord in the telemetry
	// export, always in a focused response viewer, where the pane had been
	// opening its own search prompt all along. ctrl+f is the delivered twin
	// (both are pane keys already, #1265/#1830).
	{"cmd+f", "http.search", "Search in response", HTTP, "HTTP client (#2400)"},
	{"ctrl+f", "http.search", "Search in response", HTTP, "HTTP client (#2400)"},
	// Walking the issues list from the keyboard (#2400): ctrl+up / ctrl+down
	// are what the telemetry saw pressed there. Both are delivered (arrows
	// carry their modifiers in the legacy CSI encoding) and scoped to the
	// issues window, so the editor's ctrl+arrow paragraph jumps are untouched.
	{"ctrl+up", "issues.selectPrev", "Previous issue", Issues, "GitHub issues (#2400)"},
	{"ctrl+down", "issues.selectNext", "Next issue", Issues, "GitHub issues (#2400)"},
	// The same muscle memory in the archive viewer (#2314): "rerun" reads as
	// "read the archive again" there, which is the listing reload.
	{"ctrl+r", "archive.reload", "Reload archive listing", Archive, "Archive viewer (#1762)"},
	// Rerun and Stop (#1048, #1374): JetBrains' macOS Rerun (cmd+r) is taken by
	// editor.replace, so rerun keeps the Windows-scheme F5 position with a cmd
	// primary on darwin; stop's cmd+f2 is the macOS keymap verbatim. The ctrl
	// forms stay as the delivered chords off macOS (both fold together there).
	{"cmd+f5", "run.rerun", "Rerun last", Global, "Run (0350)"},
	{"ctrl+f5", "run.rerun", "Rerun last", Global, "Run (0350)"},
	{"cmd+f2", "debug.stop", "Stop debug session", Global, "Run (0350)"},
	{"ctrl+f2", "debug.stop", "Stop debug session", Global, "Run (0350)"},
	// Problems and Structure tool windows (#1048): JetBrains' cmd+6/cmd+7 are
	// taken (TODO index, comment toggle on the German layout), so the free
	// neighbours keep the numeric tool-window family; the palette is the
	// delivered fallback, like the other Cmd-primary tool windows.
	{"cmd+8", "problems.toggle", "Problems tool window", Global, "Problems (#1024)"},
	{"cmd+3", "structure.toggle", "Structure tool window", Global, "Structure (#1025)"},
	{"f10", "menu.open", "Open menu bar", Global, "Menu (0160)"},
	{"cmd+,", "settings.open", "Settings", Global, "Menu (0160)"},
	// Unbound-command audit (#1378): JetBrains chords for palette-only
	// commands where one exists and is conflict-free on both platforms.
	// cmd+f12 is JetBrains' File Structure popup (macOS keymap verbatim);
	// the cmd+3 Structure tool window stays the persistent counterpart.
	// Editor-scoped since the #1794 audit: the popup lists the focused
	// document's symbols, like its cmd+y/cmd+alt+f7 siblings.
	{"cmd+f12", "lsp.documentSymbols", "File structure", Editor, "LSP (#1153)"},
	// cmd+y is JetBrains' Quick Definition on the macOS keymap.
	{"cmd+y", "lsp.peekDefinition", "Peek definition", Editor, "LSP (#1154)"},
	// cmd+alt+f7 is JetBrains' Show Usages; the persistent panel variant of
	// the alt+f7 popup above.
	{"cmd+alt+f7", "lsp.referencesPanel", "Find usages (panel)", Editor, "LSP (#1155)"},
	// JetBrains' "Open in Find Window" (#2055): from search everywhere or the
	// find-usages popup the current hits move into the persistent bottom
	// panel. Palette-scoped — the overlay owns the keyboard while it is up —
	// with cmd+enter as the JetBrains-macOS primary and ctrl+enter as the
	// chord delivered everywhere (both fold together off macOS).
	{"cmd+enter", "find.openInPanel", "Open results in Find window", Palette, "Find panel (#2055)"},
	{"ctrl+enter", "find.openInPanel", "Open results in Find window", Palette, "Find panel (#2055)"},
	// JetBrains' run-context-configuration chord from the Windows scheme
	// (ctrl+shift+f10); the macOS ctrl+shift+r would collide with
	// project.replaceInPath's cmd+shift+r once folded onto Ctrl off macOS.
	// Modified F-keys deliver everywhere (CSI parameter encoding). Editor-
	// scoped since the #1794 audit: the command runs the test at the caret,
	// which only exists with an editor focused.
	{"ctrl+shift+f10", "run.testAtCursor", "Run test at cursor", Editor, "Run (#1150)"},
	// cmd+f3 is JetBrains' Show Bookmarks on the macOS keymap.
	{"cmd+f3", "nav.bookmarks", "Bookmarks", Global, "Marks (#1151)"},
	// Project bookmarks (#55). JetBrains' macOS F3 (Toggle Bookmark) is
	// taken here by search.nextMatch, so the family uses the JetBrains
	// Windows chord F11 — delivered everywhere — with alt+f3, the macOS
	// mnemonic chord, for the mnemonic flavour. Both are editor-scoped:
	// they bookmark the caret's line.
	{"f11", "bookmark.toggle", "Toggle bookmark", Editor, "Bookmarks (#55)"},
	{"alt+f3", "bookmark.toggleMnemonic", "Toggle bookmark with mnemonic", Editor, "Bookmarks (#55)"},
	{"shift+f11", "bookmark.next", "Next bookmark", Editor, "Bookmarks (#55)"},
	{"ctrl+shift+f11", "bookmark.previous", "Previous bookmark", Editor, "Bookmarks (#55)"},
	// Second unbound-command audit (#2305): commands are driven by keybind far
	// more often than by the palette, so an everyday action that ships
	// palette-only is effectively invisible. These are the palette-only
	// commands that earned a chord — JetBrains' own where one exists and is
	// free on both platforms, the ctrl+alt tool/view family otherwise. The
	// remaining unbound commands are keybind-less on purpose; the audit ledger
	// with their reasons lives in cmd/ike/keybind_audit_test.go.
	// JetBrains' Copy Path (cmd+shift+c on the macOS keymap) verbatim; the
	// relative-path and reference flavours stay palette-only, one chord for
	// the everyday form like editor.copyDocPath's jq/yq siblings.
	{"cmd+shift+c", "file.copyPath", "Copy path", Global, "App (#2305)"},
	// JetBrains Optimize Imports (ctrl+alt+o in both keymaps).
	{"ctrl+alt+o", "lsp.organizeImports", "Organize imports", Editor, "LSP (#2305)"},
	// The playgrounds (#1936/#2039) join the ctrl+alt family next to their own
	// ctrl+alt+e query-view toggle: j for jq, y for yq. Global for the same
	// reason as that row — the playground owns the keyboard while its pane is
	// focused and resolves unclaimed keys against the Global scope. The
	// at-path variants stay doorway-only: alt+enter's intention popup offers
	// them exactly where they apply (the caret on a JSON/YAML path).
	{"ctrl+alt+j", "json.jqPlayground", "jq playground", Global, "jq/yq playground (#2305)"},
	{"ctrl+alt+y", "yaml.yqPlayground", "yq playground", Global, "jq/yq playground (#2305)"},
	// Test-data wizard (#2134): the scratch family's third chord, one modifier
	// beyond cmd+shift+n's new scratch file — the wizard writes one too.
	{"cmd+alt+shift+n", "scratch.generate", "Generate test data", Global, "Scratch files (#2305)"},
	// The scratch store's two connections to the everyday work flow (#2339).
	// Both keep the family's cmd+alt+shift layer but spell out what they act
	// on rather than continuing the "…+n" run: n is "new scratch file" twice
	// over already, and these two are "from the selection" and "promote".
	{"cmd+alt+shift+s", "scratch.newFromSelection", "New scratch from selection", Global, "Scratch files (#2339)"},
	{"cmd+alt+shift+p", "scratch.promote", "Promote scratch to file", Global, "Scratch files (#2339)"},
	// JetBrains' Show Diff is cmd+d, taken here by editor.duplicateLine, so
	// the diff-against-HEAD lands one modifier away on the free cmd+alt+d.
	{"cmd+alt+d", "vcs.diff", "Diff file against HEAD", Global, "VCS (#2305)"},
	// JetBrains' numeric tool-window family, continued on its own numbers:
	// cmd+4 is the Run window (test results here), cmd+5 the Debug window
	// (console/variables here).
	{"cmd+4", "tests.toggle", "Test results tool window", Global, "Run (#2305)"},
	{"cmd+5", "debug.console", "Debug console tool window", Global, "Run (#2305)"},
	// JetBrains' Run… / Debug… popups (alt+shift+f10 / alt+shift+f9). The
	// run-configuration picker takes the first verbatim; the second goes to
	// debugging the test at the caret — the debug twin of run.testAtCursor,
	// whose JetBrains chord (ctrl+shift+f9) is taken by http.showResponse.
	{"alt+shift+f10", "run.select", "Run/Debug configurations", Global, "Run (#2305)"},
	{"alt+shift+f9", "debug.testAtCursor", "Debug test at cursor", Editor, "Run (#2305)"},
	// Closing the focused pane whole, next to cmd+w's close-tab: w for close
	// in the ctrl+alt pane family (ctrl+alt+r resize, ctrl+alt+f zen).
	{"ctrl+alt+w", "pane.close", "Close pane", Global, "App (#2305)"},
	// Soft wrap is the one view toggle flipped by the hour; the other
	// view.toggle* commands stay palette/View-menu affairs.
	{"alt+shift+w", "view.toggleWrap", "Toggle soft wrap", Editor, "Editor (#2305)"},
	// Sort Lines (#2417) has no JetBrains chord to mirror; cmd+alt+shift+s is
	// already scratch.newFromSelection (#2339), so the plain sort takes the
	// free alt+shift+s next to the other editor-scoped alt+shift keys. Its
	// variants stay palette-only (keybind_audit_test.go).
	{"alt+shift+s", "editor.sortLines", "Sort lines", Editor, "Editor (#2417)"},
	// The layout family's third F12 chord: shift+f12 restores the default
	// layout, cmd+shift+f12 hides all tool windows, alt+shift+f12 opens the
	// saved-layout picker.
	{"alt+shift+f12", "window.layouts", "Window layouts", Global, "Windowing (#2305)"},
	// Open in Browser (#2365): shipped menu-only (#1429/#2298), but telemetry
	// showed it invoked several times a day, always through the menu — the
	// detour costs real time in HTML-preview work. JetBrains' own chord for
	// WebOpenInAction is alt+f2 in both keymaps and it is free here (f2 and
	// shift+f2 are the diagnostic steps, cmd/ctrl+f2 stops the debugger), so it
	// ships verbatim; no bracket or slash key, so QWERTZ types it unchanged.
	// Global, like file.copyPath: the command resolves its subject from the
	// focused pane, so one row serves the editor and the explorer — exactly the
	// two places the menu offers the action.
	{"alt+f2", "file.openInBrowser", "Open in browser", Global, "App (#2365)"},
}

// darwinRows are default bindings that only ship on macOS: chords whose
// logical Cmd has no useful folded form elsewhere, because platform.go's
// Cmd→Ctrl fold would land them on a chord another default already owns.
var darwinRows = []row{
	// IntelliJ's classic macOS Back/Forward chords (#2361): the bracket chords
	// above need option-combos on a German QWERTZ layout, so these aliases keep
	// navigation reachable there. They are free of macOS system shortcuts
	// (unlike plain ctrl+left/right, which Mission Control takes). macOS only —
	// elsewhere they would fold onto ctrl+alt+left/right, which editor tab
	// cycling owns.
	{"cmd+alt+left", "nav.back", "Navigate back", Global, "Editor (06)/app (01)"},
	{"cmd+alt+right", "nav.forward", "Navigate forward", Global, "Editor (06)/app (01)"},
}

// Defaults returns the default binding set for the named preset. Unknown presets
// fall back to JetBrains. Chords are parsed but not yet platform-normalised;
// BuildTable normalises them for the target goos.
func Defaults(preset string) []Binding {
	return DefaultsFor(preset, GOOS)
}

// DefaultsFor is Defaults for an explicit target platform: the macOS-only rows
// (darwinRows) ship only when goos is darwin. Tests and doc generators that
// build the table for a platform other than the running one use it, so a
// macOS-only chord is never judged under the Cmd→Ctrl fold.
func DefaultsFor(preset, goos string) []Binding {
	// Only one preset exists today; reserved for future presets (vscode, etc.).
	rows := jetbrainsRows
	if goos == "darwin" {
		rows = append(append([]row{}, rows...), darwinRows...)
	}
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
