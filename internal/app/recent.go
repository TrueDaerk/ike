package app

import "path/filepath"

// recentFiles is the MRU store behind the recent-files palette mode (Roadmap
// 0230, #235): the files most recently opened or activated, most recent
// first. It is held by pointer on the model so touches from value-receiver
// update paths land in the one shared list, and it persists as part of the
// session state (runtime UI state, like the rest of session.json).
type recentFiles struct {
	paths []string
}

// maxRecentFiles caps the MRU list; JetBrains' default popup depth is in the
// same range, and the palette query narrows long histories anyway.
const maxRecentFiles = 50

// Touch moves path to the front of the list, deduplicated by cleaned path.
func (r *recentFiles) Touch(path string) {
	if path == "" {
		return
	}
	key := filepath.Clean(path)
	out := make([]string, 0, len(r.paths)+1)
	out = append(out, path)
	for _, p := range r.paths {
		if filepath.Clean(p) == key {
			continue
		}
		out = append(out, p)
	}
	if len(out) > maxRecentFiles {
		out = out[:maxRecentFiles]
	}
	r.paths = out
}

// List returns a copy of the MRU paths, most recent first.
func (r *recentFiles) List() []string {
	return append([]string(nil), r.paths...)
}

// Set replaces the list (session restore), deduplicating and capping so a
// hand-edited or stale session file cannot grow the list unboundedly.
func (r *recentFiles) Set(paths []string) {
	r.paths = nil
	seen := make(map[string]bool, len(paths))
	for _, p := range paths {
		key := filepath.Clean(p)
		if p == "" || seen[key] {
			continue
		}
		seen[key] = true
		r.paths = append(r.paths, p)
		if len(r.paths) >= maxRecentFiles {
			break
		}
	}
}
