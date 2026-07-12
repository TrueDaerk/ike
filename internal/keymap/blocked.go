package keymap

// blockedDefaults is the audit ledger for Roadmap 0081/20: every default
// binding whose command id is intentionally unregistered because the feature
// behind it does not exist yet. Each entry records the dependency that
// unblocks it (issue number or work stream), so the coverage test in
// internal/app can tell a documented gap from a typo'd or silently-dead
// binding. Remove an entry the moment its command registers — a stale entry
// (blocked and registered) fails the same test.
var blockedDefaults = map[string]string{
	"vcs.updateProject": "VCS integration (epic #461)",
	"vcs.revertFile":    "VCS integration (epic #461)",
}

// BlockedReason reports whether a command id is a documented blocked default
// binding, and the dependency that unblocks it.
func BlockedReason(id string) (string, bool) {
	r, ok := blockedDefaults[id]
	return r, ok
}
