package deeplink

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// resolve.go turns a parsed Link into a local project, in the fixed order the
// scheme promises: recent-projects history first, then one level of the
// configured projects directory, then — for remote links only — the clone
// fallback. Only switching to an already known local project runs without a
// prompt; the clone path always goes through the user-confirmed dialog.

// Candidate is one local project a link can resolve to — a history entry or a
// projects-directory child.
type Candidate struct {
	Path       string
	Name       string
	LastOpened time.Time
	// Remotes holds the repository's canonical remote keys. Empty means "not
	// recorded" (a pre-#2396 history entry) — the resolver then reads them
	// live from the checkout.
	Remotes []string
}

// Resolution is the pipeline's verdict.
type Resolution struct {
	Kind ResolutionKind
	// Path is the project root to switch to (KindSwitch).
	Path string
	// Choices are the matching projects, most recently opened first
	// (KindChoose; the first entry is the dialog default).
	Choices []Candidate
}

// ResolutionKind enumerates what a link resolves to.
type ResolutionKind int

const (
	// KindSwitch — exactly one local project matched; switch to Path.
	KindSwitch ResolutionKind = iota
	// KindChoose — several local projects matched (clones, worktrees); ask.
	KindChoose
	// KindClone — nothing local; offer the clone dialog for the link's remote.
	KindClone
	// KindNotFound — nothing local and nothing to clone (project= links).
	KindNotFound
)

// Resolve runs the pipeline: history → projectsDir scan → clone/not-found.
// history arrives most-recent-first (the stored order); projectsDir may be ""
// (unresolvable setting), which skips the scan. Entries whose path no longer
// exists are skipped.
func Resolve(link Link, history []Candidate, projectsDir string) Resolution {
	if hits := matchHistory(link, history); len(hits) > 0 {
		return verdict(hits)
	}
	if hits := scanProjectsDir(link, projectsDir); len(hits) > 0 {
		return verdict(hits)
	}
	if link.RemoteKey != "" {
		return Resolution{Kind: KindClone}
	}
	return Resolution{Kind: KindNotFound}
}

// verdict wraps hits into a switch (one) or a chooser (several, most recently
// opened first — scan hits carry the zero time and sort last).
func verdict(hits []Candidate) Resolution {
	if len(hits) == 1 {
		return Resolution{Kind: KindSwitch, Path: hits[0].Path}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].LastOpened.After(hits[j].LastOpened) })
	return Resolution{Kind: KindChoose, Choices: hits}
}

// matchHistory compares the link against the recent-projects history. Remote
// links match on the canonical remote key; entries without recorded remotes
// (pre-#2396) are read live from the checkout. Project links match on the
// directory name. Stale paths are skipped either way.
func matchHistory(link Link, history []Candidate) []Candidate {
	var hits []Candidate
	seen := map[string]bool{}
	for _, c := range history {
		if c.Path == "" || seen[c.Path] || !dirExists(c.Path) {
			continue
		}
		seen[c.Path] = true
		if matches(link, c) {
			hits = append(hits, c)
		}
	}
	return hits
}

// matches reports whether one candidate answers the link.
func matches(link Link, c Candidate) bool {
	if link.Project != "" {
		name := c.Name
		if name == "" {
			name = filepath.Base(c.Path)
		}
		return strings.EqualFold(name, link.Project)
	}
	remotes := c.Remotes
	if len(remotes) == 0 {
		remotes = Remotes(c.Path)
	}
	for _, key := range remotes {
		if key == link.RemoteKey {
			return true
		}
	}
	return false
}

// scanProjectsDir checks every direct child of the projects directory — one
// level only, no recursion, matching the setting's contract of "my checkouts
// live here". Symlinked children resolve through dirExists like any path.
func scanProjectsDir(link Link, dir string) []Candidate {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var hits []Candidate
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if !dirExists(path) {
			continue
		}
		c := Candidate{Path: path, Name: e.Name()}
		if matches(link, c) {
			hits = append(hits, c)
		}
	}
	return hits
}

// dirExists reports whether path is (or resolves to) a directory.
func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
