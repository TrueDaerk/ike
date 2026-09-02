// Package project owns project switching (Roadmap 0090). This first slice is
// the data layer: the ProjectEntry type, project-root path validation, and the
// recent-projects history content rules (upsert-by-path, move-to-front dedupe,
// cap at project.max_history). Persistence is delegated to internal/config's
// typed setter — this package decides *what* is stored and *when*, config owns
// the write mechanism. Later sub-issues add the picker, the switch
// orchestration msgs, and the `project.switch` command on top of it.
package project

import (
	"path/filepath"
	"time"

	"ike/internal/config"
	"ike/internal/deeplink"
)

// Entry is one recently opened project. Path is absolute and cleaned, Name is
// the display name shown by the picker (default: base directory name),
// LastOpened orders the history most-recent-first, and Remotes carries the
// repository's canonical git-remote keys ("host/owner/repo", #2396) so an
// ike:// link can find the project without touching the checkout.
type Entry struct {
	Path       string
	Name       string
	LastOpened time.Time
	Remotes    []string
}

// NewEntry builds the entry recorded when the project at path (already
// validated and absolute) is opened at openedAt. The remotes are read from the
// checkout's git config here — recording an open is the natural moment to keep
// the link index fresh, and the read is a couple of file stats.
func NewEntry(path string, openedAt time.Time) Entry {
	return Entry{Path: path, Name: filepath.Base(path), LastOpened: openedAt,
		Remotes: deeplink.Remotes(path)}
}

// fromConfig decodes a persisted history entry. A missing or unparseable
// last_opened yields the zero time (the entry keeps its stored list position);
// a missing name falls back to the base directory name.
func fromConfig(e config.ProjectHistoryEntry) Entry {
	t, _ := time.Parse(time.RFC3339, e.LastOpened)
	name := e.Name
	if name == "" {
		name = filepath.Base(e.Path)
	}
	return Entry{Path: e.Path, Name: name, LastOpened: t, Remotes: e.Remotes}
}

// toConfig encodes the entry into the persisted [[project.history]] shape,
// with last_opened as RFC3339 in UTC.
func (e Entry) toConfig() config.ProjectHistoryEntry {
	return config.ProjectHistoryEntry{
		Path:       e.Path,
		Name:       e.Name,
		LastOpened: e.LastOpened.UTC().Format(time.RFC3339),
		Remotes:    e.Remotes,
	}
}
