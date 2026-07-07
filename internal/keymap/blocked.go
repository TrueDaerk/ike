package keymap

// blockedDefaults is the audit ledger for Roadmap 0081/20: every default
// binding whose command id is intentionally unregistered because the feature
// behind it does not exist yet. Each entry records the dependency that
// unblocks it (issue number or work stream), so the coverage test in
// internal/app can tell a documented gap from a typo'd or silently-dead
// binding. Remove an entry the moment its command registers — a stale entry
// (blocked and registered) fails the same test.
var blockedDefaults = map[string]string{
	"vcs.commit":               "VCS integration (idea #28)",
	"vcs.updateProject":        "VCS integration (idea #28)",
	"vcs.revertFile":           "VCS integration (idea #28)",
	"editor.commentLine":       "comment toggling via the language registry (epic #70, sub #75)",
	"editor.commentBlock":      "comment toggling via the language registry (epic #70, sub #76)",
	"editor.replace":           "in-file replace UI (idea #49)",
	"editor.findUsages":        "LSP find references (#5)",
	"editor.rename":            "LSP rename (#6)",
	"palette.searchEverywhere": "search-everywhere palette mode (idea #50)",
	"palette.recentFiles":      "recent-files palette mode (idea #50)",
	"project.goToClass":        "document symbols / structure view (idea #31)",
	"project.findInPath":       "project-wide search & replace (epic #73, sub #85)",
	"project.replaceInPath":    "project-wide search & replace (epic #73, sub #86)",
	"nav.back":                 "editor navigation history (idea #51)",
	"nav.forward":              "editor navigation history (idea #51)",
}

// BlockedReason reports whether a command id is a documented blocked default
// binding, and the dependency that unblocks it.
func BlockedReason(id string) (string, bool) {
	r, ok := blockedDefaults[id]
	return r, ok
}
