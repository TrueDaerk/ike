package palette

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// usage.go implements most-used command ranking (#773): a persisted
// per-command selection counter. Only choices confirmed from the palette
// window bump it — the root model increments on palette.RunCommandMsg, a path
// keybind invocations never take — so shortcut users don't push their
// commands up the palette listing.

// Usage is the persisted per-command selection counter. The zero value (and
// nil) is inert: Count returns 0 and Bump is a no-op without a path.
type Usage struct {
	path   string
	counts map[string]int
}

// LoadUsage reads the counter file at path, tolerating a missing or malformed
// file (fresh counts). Failing to read must never disrupt the palette.
func LoadUsage(path string) *Usage {
	u := &Usage{path: path, counts: map[string]int{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return u
	}
	var counts map[string]int
	if json.Unmarshal(data, &counts) == nil && counts != nil {
		u.counts = counts
	}
	return u
}

// Count returns how often the command was chosen from the palette.
func (u *Usage) Count(id string) int {
	if u == nil {
		return 0
	}
	return u.counts[id]
}

// Bump records one palette selection of the command and persists the counts.
// Errors are swallowed: failing to persist must never disrupt the session.
func (u *Usage) Bump(id string) {
	if u == nil || id == "" {
		return
	}
	if u.counts == nil {
		u.counts = map[string]int{}
	}
	u.counts[id]++
	if u.path == "" {
		return
	}
	data, err := json.Marshal(u.counts)
	if err != nil {
		return
	}
	if dir := filepath.Dir(u.path); dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	_ = os.WriteFile(u.path, data, 0o644)
}
