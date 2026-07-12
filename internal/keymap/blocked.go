package keymap

// blockedDefaults is the audit ledger for Roadmap 0081/20: every default
// binding whose command id is intentionally unregistered because the feature
// behind it does not exist yet. Each entry records the dependency that
// unblocks it (issue number or work stream), so the coverage test in
// internal/app can tell a documented gap from a typo'd or silently-dead
// binding. Remove an entry the moment its command registers — a stale entry
// (blocked and registered) fails the same test.
var blockedDefaults = map[string]string{}

// StubBlockedForTest adds a temporary ledger entry and returns its remover.
// The blocked-binding machinery (labels, toasts, cheatsheet group) stays
// test-covered while the real ledger is empty — Roadmap 0320 delivered the
// last VCS ids, but future work streams will park bindings here again.
func StubBlockedForTest(id, reason string) func() {
	blockedDefaults[id] = reason
	return func() { delete(blockedDefaults, id) }
}

// BlockedReason reports whether a command id is a documented blocked default
// binding, and the dependency that unblocks it.
func BlockedReason(id string) (string, bool) {
	r, ok := blockedDefaults[id]
	return r, ok
}
