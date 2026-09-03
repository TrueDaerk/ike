package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// RecentEntry is one MRU record: the file path and when it was last opened or
// activated. LastOpened is zero for entries migrated from a pre-#1113 session
// file (bare path lists carried no timestamps).
type RecentEntry struct {
	Path       string
	LastOpened time.Time
}

// recentFiles is the MRU store behind the recent-files palette mode (Roadmap
// 0230, #235): the files most recently opened or activated, most recent
// first. It is held by pointer on the model so touches from value-receiver
// update paths land in the one shared list, and it persists as part of the
// session state (runtime UI state, like the rest of session.json). Since
// #1113 every entry carries its last-opened time.
type recentFiles struct {
	entries []RecentEntry
	// now overrides the clock for Touch timestamps; tests only. Nil means
	// the wall clock.
	now func() time.Time
}

// maxRecentFiles caps the MRU list; JetBrains' default popup depth is in the
// same range, and the palette query narrows long histories anyway.
const maxRecentFiles = 50

// clock returns the injectable now source, defaulting to the wall clock.
func (r *recentFiles) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// Touch moves path to the front of the list with a fresh last-opened time,
// deduplicated by cleaned path.
func (r *recentFiles) Touch(path string) {
	if path == "" {
		return
	}
	key := filepath.Clean(path)
	out := make([]RecentEntry, 0, len(r.entries)+1)
	out = append(out, RecentEntry{Path: path, LastOpened: r.clock()})
	for _, e := range r.entries {
		if filepath.Clean(e.Path) == key {
			continue
		}
		out = append(out, e)
	}
	if len(out) > maxRecentFiles {
		out = out[:maxRecentFiles]
	}
	r.entries = out
}

// Remove deletes the entry matching path (compared by cleaned path, like the
// Touch dedupe); a path not in the list is a no-op (#1113).
func (r *recentFiles) Remove(path string) {
	key := filepath.Clean(path)
	out := r.entries[:0]
	for _, e := range r.entries {
		if filepath.Clean(e.Path) == key {
			continue
		}
		out = append(out, e)
	}
	r.entries = out
}

// List returns a copy of the MRU paths, most recent first.
func (r *recentFiles) List() []string {
	out := make([]string, len(r.entries))
	for i, e := range r.entries {
		out[i] = e.Path
	}
	return out
}

// Entries returns a copy of the MRU records, most recent first.
func (r *recentFiles) Entries() []RecentEntry {
	return append([]RecentEntry(nil), r.entries...)
}

// Set replaces the list (session restore), deduplicating and capping so a
// hand-edited or stale session file cannot grow the list unboundedly.
func (r *recentFiles) Set(entries []RecentEntry) {
	r.entries = nil
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		key := filepath.Clean(e.Path)
		if e.Path == "" || seen[key] {
			continue
		}
		seen[key] = true
		r.entries = append(r.entries, e)
		if len(r.entries) >= maxRecentFiles {
			break
		}
	}
}

// recentPick is the recent-files dialog's preselection memory (#2399): the
// row the dialog was last used to activate in this project, so the next open
// comes back up on it and a repeated cmd+e + enter bounces between the two
// targets one is switching between instead of walking the list down.
//
// It is held by pointer on the model for the same reason recentFiles is —
// value-receiver update paths must land in the one shared record — and it
// persists in its own small per-project file rather than in session.json: a
// pick is recorded from inside the palette, which has no model to snapshot the
// rest of the session from, and the memory is worthless if a kill loses it.
// Key is a frecency.Key-normalized file path, or a project path when Project
// is set (the pick came from the Recent Projects column).
type recentPick struct {
	path    string
	Key     string `json:"key,omitempty"`
	Project bool   `json:"project,omitempty"`
}

// loadRecentPick reads the memory at path, tolerating a missing or malformed
// file (no preselection). A ranking aid must never disrupt the session.
func loadRecentPick(path string) *recentPick {
	p := &recentPick{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return p
	}
	var stored recentPick
	if json.Unmarshal(data, &stored) == nil {
		p.Key, p.Project = stored.Key, stored.Project
	}
	return p
}

// Set records one activation and persists it. Errors are swallowed, like the
// usage counter's: failing to persist must never disrupt the session.
func (r *recentPick) Set(key string, project bool) {
	if r == nil {
		return
	}
	r.Key, r.Project = key, project
	if r.path == "" {
		return
	}
	data, err := json.Marshal(r)
	if err != nil {
		return
	}
	if dir := filepath.Dir(r.path); dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	_ = os.WriteFile(r.path, data, 0o644)
}

// Get returns the remembered pick; the zero value means "none", which
// preselects nothing.
func (r *recentPick) Get() (string, bool) {
	if r == nil {
		return "", false
	}
	return r.Key, r.Project
}
