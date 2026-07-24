package help

// essentials.go holds the hand-curated Essentials view (#656): the ~25
// commands a new user needs first, in 4–5 hand-named feature groups. The full
// registry dump stays one `tab` away; this is the first-orientation surface.
// Curation is deliberate — Binding.Owner values are internal roadmap tags and
// unusable as user-facing groups, and no metadata ranks commands by
// importance. Keep each group at ≤6 entries so the view fits one screen.
//
// IDs are joined against the live registry: entries whose command is not
// registered are silently dropped, so the list tolerates stub registries in
// tests and commands that land later (e.g. the welcome tour). A test asserts
// every ID here resolves against the real registry, so renames surface in CI
// rather than as silently missing rows.

// essentialGroup names one curated cluster and the command IDs it shows, in
// display order.
type essentialGroup struct {
	label string
	ids   []string
}

// essentialGroups is the curated Essentials spec.
var essentialGroups = []essentialGroup{
	{"Get around", []string{
		"palette.searchEverywhere",
		"project.goToFile",
		"palette.recentFiles",
		"project.switch",
		"nav.back",
		"palette.keymapHelp",
	}},
	{"Edit", []string{
		"editor.write",
		"editor.saveAll",
		"editor.undo",
		"editor.find",
		"editor.commentLine",
	}},
	{"Panes & tabs", []string{
		"explorer.toggle",
		"pane.switcher",
		"pane.splitRight",
		"editor.tab.next",
		"editor.closeTab",
		"pane.maximize",
	}},
	{"Project & tools", []string{
		"project.findInPath",
		"terminal.toggle",
		"run.file",
		"debug.start",
		"vcs.panel",
		"vcs.diff",
	}},
	{"Customize", []string{
		"settings.open",
		"menu.open",
		"scratch.new",
		"help.welcomeTour",
	}},
}

// EssentialIDs returns every curated command ID, for the curation-drift test.
func EssentialIDs() []string {
	var ids []string
	for _, g := range essentialGroups {
		ids = append(ids, g.ids...)
	}
	return ids
}

// EssentialsSnapshot builds the curated groups from the live registry,
// joining each curated ID with its command title and resolved shortcut the
// same way the full Snapshot does. Unregistered IDs are dropped; groups left
// empty drop out. Essentials ignores the focus context on purpose — the
// starter set is the same everywhere.
func EssentialsSnapshot(src CommandSource, res BindingResolver) []Group {
	byID := map[string]Entry{}
	for _, c := range src.Commands() {
		e := Entry{ID: c.ID, Title: c.Title}
		if res != nil {
			if s, ok := res.Binding(c.ID); ok {
				e.Shortcut = s
			}
		}
		if e.Shortcut == "" {
			e.Shortcut = c.Shortcut
		}
		byID[c.ID] = e
	}
	var groups []Group
	for _, cg := range essentialGroups {
		g := Group{Label: cg.label}
		for _, id := range cg.ids {
			if e, ok := byID[id]; ok {
				g.Entries = append(g.Entries, e)
			}
		}
		if len(g.Entries) > 0 {
			groups = append(groups, g)
		}
	}
	return groups
}
