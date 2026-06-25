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
	fragile bool
}

// jetbrainsRows is the JetBrains-flavoured default set (Roadmap 0080's table).
// Each row binds a chord to a command id owned by another roadmap; commands not
// yet registered make the binding inert until their owner lands. Chords use
// logical Cmd; platform.go maps Cmd→Ctrl off macOS at build time.
var jetbrainsRows = []row{
	{"cmd+k", "vcs.commit", "Commit", Global, "VCS (future)", false},
	{"cmd+t", "vcs.updateProject", "Update Project", Global, "VCS (future)", true},
	{"cmd+d", "editor.duplicateLine", "Duplicate line(s)", Editor, "Editor (06)", false},
	{"cmd+shift+a", "palette.searchEverywhere", "Search everywhere", Global, "Palette (07)", false},
	{"shift shift", "palette.searchEverywhere", "Search everywhere (double-shift)", Global, "Palette (07)", true},
	{"cmd+shift+o", "project.goToFile", "Go to file", Global, "Project (09)", false},
	{"cmd+o", "project.goToClass", "Go to symbol/class", Global, "Project (09)/LSP (10)", false},
	{"cmd+e", "palette.recentFiles", "Recent files", Global, "Palette (07)", false},
	{"alt+f7", "editor.findUsages", "Find usages", Editor, "Editor (06)/LSP (10)", false},
	{"shift+f6", "editor.rename", "Rename symbol", Editor, "Editor (06)/LSP (10)", false},
	{"cmd+/", "editor.commentLine", "Comment line", Editor, "Editor (06)", false},
	{"cmd+shift+/", "editor.commentBlock", "Comment block", Editor, "Editor (06)", false},
	{"cmd+s", "editor.save", "Save", Editor, "Editor (06)", false},
	{"cmd+shift+s", "editor.saveAll", "Save all", Global, "Editor (06)", false},
	{"cmd+c", "editor.copy", "Copy", Editor, "Editor (06)", false},
	{"cmd+x", "editor.cut", "Cut", Editor, "Editor (06)", false},
	{"cmd+v", "editor.paste", "Paste", Editor, "Editor (06)", false},
	// Undo binds to ctrl+z, not cmd+z: macOS terminals never forward the Cmd
	// modifier to a TUI, so a cmd+z chord is undeliverable there. ctrl+z arrives
	// as a normal key (raw mode disables the suspend signal) on every platform.
	{"ctrl+z", "editor.undo", "Undo", Editor, "Editor (06)", false},
	{"ctrl+z", "explorer.undo", "Undo file operation", Explorer, "Explorer (05)", false},
	{"cmd+shift+z", "editor.redo", "Redo", Editor, "Editor (06)", false},
	{"cmd+f", "editor.find", "Find in file", Editor, "Editor (06)", false},
	{"cmd+r", "editor.replace", "Replace in file", Editor, "Editor (06)", false},
	{"cmd+shift+f", "project.findInPath", "Find in path", Global, "Project (09)", false},
	{"cmd+shift+r", "project.replaceInPath", "Replace in path", Global, "Project (09)", false},
	{"cmd+left-bracket", "nav.back", "Navigate back", Global, "Editor (06)/app (01)", false},
	{"cmd+right-bracket", "nav.forward", "Navigate forward", Global, "Editor (06)/app (01)", false},
	{"cmd+b", "editor.gotoDeclaration", "Go to declaration", Editor, "Editor (06)/LSP (10)", false},
	{"cmd+1", "explorer.toggle", "Toggle project tree", Global, "Explorer (05)", true},
	{"ctrl+tab", "pane.switcher", "Switch pane focus", Global, "App (01)", true},
	{"cmd+w", "editor.closeTab", "Close active tab", Global, "Editor (06)", false},
	{"cmd+shift+t", "vcs.revertFile", "Revert file", Global, "VCS (future)", false},
	{"cmd+k cmd+c", "editor.commentLine", "Comment (chord example)", Editor, "Editor (06)", false},
	{"cmd+k cmd+s", "palette.keymapHelp", "Show keymap cheatsheet", Global, "Keymap (08)", false},
	{"f1", "palette.keymapHelp", "Help / cheatsheet", Global, "Keymap (08)", false},
}

// Defaults returns the default binding set for the named preset. Unknown presets
// fall back to JetBrains. Chords are parsed but not yet platform-normalised;
// BuildTable normalises them for the target goos.
func Defaults(preset string) []Binding {
	// Only one preset exists today; reserved for future presets (vscode, etc.).
	rows := jetbrainsRows
	out := make([]Binding, 0, len(rows))
	for _, r := range rows {
		out = append(out, Binding{
			Chord:   MustParseChord(r.chord),
			Command: r.command,
			Context: r.ctx,
			Title:   r.title,
			Owner:   r.owner,
			Fragile: r.fragile,
			Layer:   LayerDefault,
		})
	}
	return out
}
