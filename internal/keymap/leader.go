package keymap

// leader.go is the leader layer of Roadmap 0081/30: a reachable alternative
// for every action whose primary chord is terminal-fragile. Two prefixes,
// both driven by the existing multi-step resolver (0080's engine, no new
// machinery):
//
//   - the leader key (default "space", `[keymap] leader` tunable) — plain
//     keys never reach the chord layer inside a capturing editor, so the
//     space leader is automatically scoped to "outside the editor";
//   - `ctrl+k <mnemonic>` — ctrl-modified chords are eligible everywhere,
//     making it the universal variant that also works mid-edit.
//
// Actions without a curated mnemonic stay reachable through the palette
// (ctrl+p, delivered everywhere); the completeness test in
// reachability_test.go enforces that every fragile default has one of the
// two escape routes.

// DefaultLeader is the leader prefix when [keymap] leader is unset.
const DefaultLeader = "space"

// leaderMnemonic is one curated leader continuation.
type leaderMnemonic struct {
	key     string
	command string
	title   string
}

// leaderMnemonics maps single follow-up keys to the fragile-primary actions
// worth a two-keystroke path. Curated, not generated: mnemonics collide
// easily and the palette already covers the long tail.
var leaderMnemonics = []leaderMnemonic{
	{"f", "project.goToFile", "Go to file"},
	{"g", "project.findInPath", "Find in path (grep)"},
	{"r", "project.replaceInPath", "Replace in path"},
	// R (shift+r) mirrors the fragile cmd+r primary for the in-file replace
	// (0240 phase 1, #282).
	{"R", "editor.replace", "Replace in file"},
	// S (shift+s) mirrors the fragile cmd+o primary for the workspace-symbol
	// search (0250 phase 1, #294) — off macOS ctrl+o is vim jump-back.
	{"S", "project.goToClass", "Go to symbol"},
	{"p", "project.switch", "Switch project"},
	// P (shift+p) mirrors the fragile cmd+k m primary for the rendered
	// markdown preview (#62) — lowercase p is the project switch.
	{"P", "markdown.preview", "Markdown preview"},
	{"t", "terminal.toggle", "Toggle terminal"},
	// T (shift+t) opens a fresh session next to lowercase-t's toggle (#242).
	{"T", "terminal.new", "New terminal"},
	{"h", "notifications.history", "Notification history"},
	{"e", "explorer.toggle", "Focus explorer / editor"},
	{"s", "editor.saveAll", "Save all"},
	{"w", "editor.write", "Save file"},
	{"d", "lsp.definition", "Go to definition"},
	// k mirrors vim's K (keyword lookup) for the hover / quick-doc popup (#378).
	{"k", "lsp.hover", "Quick documentation (hover)"},
	{"u", "lsp.references", "Find usages"},
	// H (shift+h) mirrors the fragile ctrl+alt+h primary for the call
	// hierarchy (#173) — lowercase h is the notification history.
	{"H", "lsp.callHierarchy", "Call hierarchy"},
	{"a", "lsp.codeAction", "Show intention actions"},
	// G (shift+g) mirrors the fragile ctrl+shift+g primary for select-all-
	// occurrences (#145) — lowercase g is find-in-path.
	{"G", "editor.caret.addAll", "Add carets at all occurrences"},
	// D (shift+d) mirrors the fragile cmd+6 primary for the TODO index (#61)
	// — "toDo"; lowercase t is the terminal toggle, lowercase d go-to-definition.
	{"D", "todo.list", "TODO index"},
	{"n", "lsp.rename", "Rename symbol"},
	{"l", "lsp.format", "Reformat file"},
	{"c", "editor.commentLine", "Comment line"},
	{"x", "editor.closeTab", "Close tab"},
	{"m", "palette.recentFiles", "Recent files (MRU)"},
	// A (shift+a) mirrors the fragile cmd+shift+a primary; double-shift needs
	// key-up reporting, so the leader path is the universal escape (#236).
	{"A", "palette.searchEverywhere", "Search everywhere"},
	// A doubled leader is the terminal stand-in for JetBrains' double-shift
	// (0082 sheet 17, #263): space space opens search-everywhere.
	{"space", "palette.searchEverywhere", "Search everywhere (double-tap)"},
	{"o", "editor.tab.reopenClosed", "Reopen closed tab"},
	// Navigation history (Roadmap 0220): the primary cmd+bracket chords are
	// fragile and awkward on QWERTZ; b(ack) and i (vim's ctrl+i forward
	// association) are the delivered path.
	{"b", "nav.back", "Navigate back"},
	{"i", "nav.forward", "Navigate forward"},
	{",", "settings.open", "Settings"},
	// VCS actions (Roadmap 0320) live under a "v" sub-prefix: "space g" is
	// taken by grep, so the #22 sheets' "space g c" family lands on "space v
	// c/u/x" instead — v for VCS, then the JetBrains mnemonic.
	{"v c", "vcs.commit", "Commit"},
	{"v u", "vcs.updateProject", "Update project"},
	{"v x", "vcs.revertFile", "Revert file"},
	{"v b", "vcs.branches", "Switch branch"},
	{"v d", "vcs.diff", "Diff file against HEAD"},
	{"v a", "vcs.blameLine", "Toggle inline blame (annotate)"},
	{"v v", "vcs.panel", "Toggle VCS tool window"},
	{"1", "editor.tab.select1", "Go to tab 1"},
	{"2", "editor.tab.select2", "Go to tab 2"},
	{"3", "editor.tab.select3", "Go to tab 3"},
	{"4", "editor.tab.select4", "Go to tab 4"},
	{"5", "editor.tab.select5", "Go to tab 5"},
	{"6", "editor.tab.select6", "Go to tab 6"},
	{"7", "editor.tab.select7", "Go to tab 7"},
	{"8", "editor.tab.select8", "Go to tab 8"},
	{"9", "editor.tab.select9", "Go to tab 9"},
}

// LeaderRows expands the mnemonic table into bindings under the given leader
// prefix plus the universal ctrl+k variant. An empty leader falls back to
// DefaultLeader; both prefixes resolve through the ordinary chord engine
// (pending step + timeout).
func LeaderRows(leader string) []Binding {
	if leader == "" {
		leader = DefaultLeader
	}
	out := make([]Binding, 0, len(leaderMnemonics)*2)
	for _, m := range leaderMnemonics {
		for _, prefix := range []string{leader, "ctrl+k"} {
			chord, err := ParseChord(prefix + " " + m.key)
			if err != nil {
				continue // an unparseable configured leader skips that variant
			}
			out = append(out, Binding{
				Chord:   chord,
				Command: m.command,
				Context: Global,
				Title:   m.title,
				Owner:   "Leader (0081)",
				Layer:   LayerDefault,
			})
		}
	}
	return out
}

// LeaderCommands reports the command ids covered by the leader layer, for
// the completeness check (every fragile default needs leader or palette).
func LeaderCommands() map[string]bool {
	out := make(map[string]bool, len(leaderMnemonics))
	for _, m := range leaderMnemonics {
		out[m.command] = true
	}
	return out
}
