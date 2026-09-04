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

// GlobalEssentialsLabel is the group label of the curated Global section the
// context view shows (#2483); groupTitle renders it "Global (essentials)".
const GlobalEssentialsLabel = "global-essentials"

// globalEssentialIDs is the hand-curated Global section of the context view
// (#2483): the global commands a user objectively needs from anywhere —
// navigate, save, find, switch panes, open the palette / help / settings —
// small enough (≤ 20 rows) that the focused context's own section plus this
// one fit a 40-line screen together. Like Essentials, curation is deliberate:
// no metadata ranks the ~290 global commands by importance. A drift test in
// internal/app asserts every ID resolves against the real registry, and a
// budget test holds the one-screen promise.
var globalEssentialIDs = []string{
	"palette.searchEverywhere",
	"project.goToFile",
	"palette.recentFiles",
	"project.findInPath",
	"search.open",
	"editor.saveAll",
	"nav.back",
	"pane.switcher",
	"explorer.toggle",
	"terminal.toggle",
	"pane.maximize",
	"palette.keymapHelp",
	"menu.open",
	"settings.open",
}

// GlobalEssentialIDs returns the curated global IDs, for the drift test.
func GlobalEssentialIDs() []string { return append([]string(nil), globalEssentialIDs...) }

// GlobalEssentials builds the curated Global group of the context view (#2483)
// from the live registry, in curated (not sorted) order — the list is short
// enough that hand-ordering beats alphabet. Unregistered IDs drop silently,
// like Essentials.
func GlobalEssentials(src CommandSource, res BindingResolver) Group {
	byID := commandIndex(src, res)
	g := Group{Label: GlobalEssentialsLabel}
	for _, id := range globalEssentialIDs {
		if e, ok := byID[id]; ok {
			g.Entries = append(g.Entries, e)
		}
	}
	return g
}

// commandIndex joins every registered command with its shortcut, keyed by ID —
// the lookup the curated views (Essentials, the context view's Global section)
// resolve their hand-picked IDs against.
func commandIndex(src CommandSource, res BindingResolver) map[string]Entry {
	byID := map[string]Entry{}
	for _, c := range src.Commands() {
		byID[c.ID] = entryFor(c, res)
	}
	return byID
}

// EssentialsSnapshot builds the curated groups from the live registry,
// joining each curated ID with its command title and resolved shortcut the
// same way the full Snapshot does. Unregistered IDs are dropped; groups left
// empty drop out. Essentials ignores the focus context on purpose — the
// starter set is the same everywhere.
func EssentialsSnapshot(src CommandSource, res BindingResolver) []Group {
	byID := commandIndex(src, res)
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
