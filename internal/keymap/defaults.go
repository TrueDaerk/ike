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
	// alt+shift+p mirrors the project plugin's default Keymap slot: the chord
	// table resolves modified chords even in a capturing editor, which the
	// registry keymap layer does not.
	{"alt+shift+p", "project.switch", "Switch project", Global, "Project (0090)"},
	{"cmd+o", "project.goToClass", "Go to symbol/class", Global, "Project (09)/LSP (10)"},
	{"cmd+e", "palette.recentFiles", "Recent files", Global, "Palette (07)"},
	// Reconciled (#5): the LSP plugin registers find-usages as lsp.references;
	// the table uses the registered id (mirroring lsp.definition below).
	{"alt+f7", "lsp.references", "Find usages", Editor, "LSP (0100)"},
	// shift+f6 renames the *file* (explorer selection or focused editor's
	// file, #175). LSP rename-symbol (#6) needs its own chord or a
	// context-aware dispatch when it lands.
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
	{"cmd+right", "editor.lineEnd", "Move to line end", Editor, "Editor (06)"},
	{"cmd+left-bracket", "nav.back", "Navigate back", Global, "Editor (06)/app (01)"},
	{"cmd+right-bracket", "nav.forward", "Navigate forward", Global, "Editor (06)/app (01)"},
	// Reconciled (0081/20): the LSP plugin registers goto-definition as
	// lsp.definition; the table uses the registered id rather than forking an
	// editor.gotoDeclaration alias.
	{"cmd+b", "lsp.definition", "Go to declaration", Editor, "LSP (0100)"},
	// JetBrains reformat-code. The L is layout-safe on QWERTZ; the selection
	// variant keys off the active visual selection inside lsp.formatRange.
	{"cmd+alt+l", "lsp.format", "Reformat file", Editor, "LSP (0100)"},
	// JetBrains intention actions. Alt+enter delivery depends on the
	// terminal's option-as-meta setting, hence fragile; 0081 owns the final
	// reachability call.
	{"alt+enter", "lsp.codeAction", "Show intention actions", Editor, "LSP (0100)"},
	{"cmd+1", "explorer.toggle", "Toggle project tree", Global, "Explorer (05)"},
	{"ctrl+tab", "pane.switcher", "Switch pane focus", Global, "App (01)"},
	{"cmd+w", "editor.closeTab", "Close active tab", Global, "Editor (06)"},
	// Editor tabs (0190, #158). Alt+digits jump straight to a tab (digits sit
	// identically on QWERTZ). The delivered tab-cycling primaries are
	// ctrl+pgup/pgdn (#248): on macOS Option is a composition key (QWERTZ needs
	// it for brackets), so alt chords never arrive there; the page keys carry
	// modifiers in their CSI parameter and follow the terminal-tab-cycling
	// convention. The alt+arrow secondaries were freed for word-wise cursor
	// motion in the editor (#303) — they now fall through the chord table to
	// the editor's vim layer.
	{"ctrl+pgdown", "editor.tab.next", "Next tab", Global, "Editor tabs (0190)"},
	{"ctrl+pgup", "editor.tab.prev", "Previous tab", Global, "Editor tabs (0190)"},
	{"ctrl+shift+pgdown", "editor.tab.moveRight", "Move tab right", Global, "Editor tabs (0190)"},
	{"ctrl+shift+pgup", "editor.tab.moveLeft", "Move tab left", Global, "Editor tabs (0190)"},
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
	{"cmd+shift+t", "vcs.revertFile", "Revert file", Global, "VCS (future)"},
	{"cmd+k cmd+c", "editor.commentLine", "Comment (chord example)", Editor, "Editor (06)"},
	{"cmd+k cmd+s", "palette.keymapHelp", "Show keymap cheatsheet", Global, "Keymap (08)"},
	{"cmd+k down", "pane.splitDown", "Split down", Global, "App (01)"},
	{"cmd+k up", "pane.splitUp", "Split up", Global, "App (01)"},
	{"cmd+k right", "pane.splitRight", "Split right", Global, "App (01)"},
	{"cmd+k left", "pane.splitLeft", "Split left", Global, "App (01)"},
	{"f1", "palette.keymapHelp", "Help / cheatsheet", Global, "Keymap (08)"},
	// JetBrains terminal toggle. Alt+F-key delivery depends on the terminal,
	// hence fragile; inside a focused terminal the reserved-set handler picks
	// it up before the chord layer (raw pass-through).
	{"alt+f12", "terminal.toggle", "Toggle terminal", Global, "Terminal (0170)"},
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
