package app

import (
	"ike/internal/complexitypanel"
	"ike/internal/help"
)

// complexity_help.go documents the Complexity window's keyboard for the
// cheatsheet (#3031). The pane grew a large single-letter keyboard of which
// only the commands bound in the `complexity` context (b, t, D, v, space,
// m, i, A, x, X, H) reach the help overlay through the registry; the fixed
// pane keys — the tab cycle, s / S sort, enter, e / E export, y copy, p pin,
// d diff, a agent, r rescan, R base revision, the filter row — belong to no
// command and were documented nowhere in the UI. This is the playground
// pattern (playhelp.go): tables of {Key, Title} rows grouped by surface,
// flagged Focused so help.withExtraLeading puts them ahead of the global
// bindings while the window is focused, and nothing at all otherwise.
//
// Keys whose chord is configurable are resolved from the live keymap
// (complexityChord), so a rebind is reflected; the fixed keys are listed
// verbatim.

// complexityHelpKey is one row of a key table: a fixed key, or a command
// whose chord is looked up live (Key is then the fallback shown when the
// command has no binding).
type complexityHelpKey struct {
	Key, Title string
	Command    string
}

// complexityListHelpKeys documents the list — the Project and File tabs,
// grouped or not, and the tree view.
var complexityListHelpKeys = []complexityHelpKey{
	{Key: "tab / l / →  ·  shift+tab / h / ←", Title: "Next / previous tab: Project, File, Changes, Trend (a click on the tab bar too)"},
	{Key: "j / k · ↑ / ↓ · pgup / pgdn · home / end · g / G", Title: "Move the cursor; the mouse wheel scrolls"},
	{Key: "enter · double click", Title: "Open the function's file at its header (a group: its first unit; a site: its line; in the tree: open a function, fold / unfold a node)"},
	{Key: "s", Title: "Cycle the sort column (a click on a column header sorts by it, again reverses)"},
	{Key: "S", Title: "Reverse the sort order"},
	{Key: "b", Command: "complexity.group", Title: "Cycle the grouping: none → class → file (Changes tab: none → file)"},
	{Key: "v", Command: "complexity.view", Title: "Project tab: switch between the grid and the directory tree — l / → / space unfold, h / ← fold or climb"},
	{Key: "t", Command: "complexity.toggleTests", Title: "Hide / show the test files on every tab"},
	{Key: "D", Command: "complexity.histogram", Title: "Show / hide the distribution row; a click on a bucket narrows the list to it"},
	{Key: "space", Command: "complexity.sites", Title: "Expand / collapse the selected function into its complexity sites and extraction candidates"},
	{Key: "m", Command: "complexity.showSites", Title: "Paint the selected function's site marks in the editor once"},
	{Key: "i", Command: "complexity.baseline", Title: "Accept the selected function (a group: its over-threshold members) into the accepted-debt baseline, or drop it again"},
	{Key: "p", Title: "Pin / unpin the selected function for an optimisation session (Project tab: the Pinned section)"},
	{Key: "x", Command: "complexity.hide", Title: "Hide the selected function from every Complexity surface; on a group, file or tree-directory row hide the file (complexity.exclude); on a ✕ row shown by hidden:yes un-hide it"},
	{Key: "X", Command: "complexity.hideFile", Title: "Hide the selected row's whole file (complexity.exclude), from a function row too"},
	{Key: "H", Command: "complexity.showHidden", Title: "Toggle the hidden:yes view: the hidden functions, muted and marked ✕, so one can be un-hidden with x"},
	{Key: "e", Title: "Export the current tab as a CSV scratch file"},
	{Key: "E", Title: "Export the current tab as a Markdown scratch file (pull-request ready)"},
	{Key: "y", Title: "Copy the selected row as one line: path:line: name ccn=.. cog=.."},
	{Key: "r", Title: "Rescan the project (rebuild the index)"},
	{Key: "/", Title: "Focus the filter row"},
}

// complexityFilterHelpKeys documents the filter row.
var complexityFilterHelpKeys = []complexityHelpKey{
	{Key: "(typing)", Title: "Narrow the rows as you type; bare words fuzzy-match the function names, -word drops its matches"},
	{Key: "tab", Title: "Accept the ghost completion of a field or value"},
	{Key: "enter", Title: "Apply the filter and return the keyboard to the list"},
	{Key: "esc", Title: "Clear the filter and return the keyboard to the list"},
	{Key: "n / N (the match-step chords)", Title: "Step through the filtered rows while the row is focused"},
}

// complexityChangesHelpKeys documents the Changes tab's own keys; the list
// keys above apply there as well.
var complexityChangesHelpKeys = []complexityHelpKey{
	{Key: "d", Title: "Diff the selected function: base revision against the working tree, side by side"},
	{Key: "R", Title: "Prompt for the base revision (branch, tag, sha, HEAD~3 or A..B): enter applies, esc cancels"},
	{Key: "b", Command: "complexity.group", Title: "Group the changes per file (changed / added / removed counts, before → after totals)"},
	{Key: "x", Command: "complexity.hide", Title: "Hide the selected changed or added function (its working-tree side); on a file row hide the file"},
}

// complexityAgentHelpKeys documents the simplify agent's keys.
var complexityAgentHelpKeys = []complexityHelpKey{
	{Key: "a", Title: "Ask the coding agent to simplify the selected function (function rows only); the answer opens as a scratch and, with complexity.agent_apply, as an undoable edit"},
	{Key: "A · esc", Command: "complexity.cancelAgent", Title: "Cancel the agent's run on the selected row"},
}

// complexityHelpGroups returns the cheatsheet groups for the focused
// Complexity window — none when another pane holds the keyboard, in which
// case the pane's keys do not apply. The filter vocabulary joins as its own
// group, read from the pane's Schema so a new field documents itself.
func (m Model) complexityHelpGroups() []help.Group {
	if !m.complexityPaneFocused() {
		return nil
	}
	groups := []help.Group{
		m.complexityHelpGroup("Complexity — list", "complexity.help.list.", complexityListHelpKeys),
		m.complexityHelpGroup("Complexity — filter row", "complexity.help.filter.", complexityFilterHelpKeys),
		m.complexityFilterVocabulary(),
		m.complexityHelpGroup("Complexity — Changes tab", "complexity.help.changes.", complexityChangesHelpKeys),
		m.complexityHelpGroup("Complexity — agent", "complexity.help.agent.", complexityAgentHelpKeys),
	}
	// The shared find chord focuses the filter row like "/" does; it is
	// resolved live, as the playground does (#2383).
	if chord, ok := m.playChordFor("editor.find"); ok {
		groups[0].Entries = append(groups[0].Entries, help.Entry{ID: "complexity.help.list.find", Title: "Focus the filter row (the shared find chord)", Shortcut: chord})
	}
	return groups
}

// complexityHelpGroup turns one key table into a Focused help group; a row
// naming a command shows the chord bound to it, its fixed key otherwise.
func (m Model) complexityHelpGroup(label, prefix string, keys []complexityHelpKey) help.Group {
	g := help.Group{Label: label, Focused: true}
	for _, k := range keys {
		g.Entries = append(g.Entries, help.Entry{ID: prefix + k.Key, Title: k.Title, Shortcut: m.complexityChord(k)})
	}
	return g
}

// complexityChord is the chord shown for a key table row: the live binding
// of its command when it has one, else the fixed key as written.
func (m Model) complexityChord(k complexityHelpKey) string {
	if k.Command != "" {
		if chord, ok := m.playChordFor(k.Command); ok {
			return chord
		}
	}
	return k.Key
}

// complexityFilterVocabulary lists every filter field of the pane's Schema
// with its documentation — baseline:, hidden:, test:, metric: and the rest.
func (m Model) complexityFilterVocabulary() help.Group {
	g := help.Group{Label: "Complexity — filter vocabulary", Focused: true}
	for _, f := range complexitypanel.Schema.Fields {
		key := f.Name + ":"
		if len(f.Values) > 0 {
			key += "<" + joinValues(f.Values) + ">"
		} else if f.ValueDoc != "" {
			key += "<" + f.ValueDoc + ">"
		}
		g.Entries = append(g.Entries, help.Entry{ID: "complexity.help.vocab." + f.Name, Title: f.Doc, Shortcut: key})
	}
	return g
}

// joinValues renders a field's enumerated values as "a|b|c".
func joinValues(vals []string) string {
	out := ""
	for i, v := range vals {
		if i > 0 {
			out += "|"
		}
		out += v
	}
	return out
}
