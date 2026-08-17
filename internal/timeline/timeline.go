// Package timeline merges the two histories IKE keeps for a single file into
// one chronological axis (#1916, building on Local History #35/#1023): the
// local-history snapshots recorded on every save, and the git commits that
// touched the file. VS Code's Timeline answers "what happened to this file,
// committed or not" in one list; this package is that list's data layer —
// pure merge/ordering/filtering, no I/O, so both halves stay testable without
// a repository.
package timeline

import (
	"sort"
	"time"

	"ike/internal/localhistory"
	"ike/internal/vcs"
)

// Source tags which history an entry came from.
type Source uint8

const (
	// Local is a local-history snapshot (an editor save).
	Local Source = iota
	// Git is a commit touching the file.
	Git
)

// Icon returns the one-cell source marker used in the Timeline list: a filled
// diamond for a save snapshot, a graph-node dot for a commit.
func (s Source) Icon() string {
	if s == Local {
		return "◆"
	}
	return "●"
}

// Entry is one point on the merged axis. Which fields carry data depends on
// Source: a Local entry has Hash (the content address) and possibly Label, a
// Git entry has Hash/ShortHash, Author, Subject and Path.
type Entry struct {
	Source Source
	Time   time.Time
	// Hash addresses the content: the snapshot's blob hash for Local, the full
	// commit sha for Git.
	Hash      string
	ShortHash string // Git: the abbreviated sha
	Label     string // Local: the snapshot's label, when one was recorded
	Author    string // Git: commit author name
	Subject   string // Git: commit subject
	// Path is the file's repo-relative path in that commit (Git only) — it
	// differs from the current path for commits older than a rename, and is
	// what `git show <hash>:<path>` needs.
	Path string
}

// Filter selects which sources a Timeline shows. The values double as the
// history.timeline_source config values.
type Filter string

const (
	// Both shows snapshots and commits (the default).
	Both Filter = "both"
	// LocalOnly hides commits.
	LocalOnly Filter = "local"
	// GitOnly hides snapshots.
	GitOnly Filter = "git"
)

// ParseFilter maps a config value onto a Filter, defaulting to Both for
// anything unknown (the config validation reports it separately).
func ParseFilter(s string) Filter {
	switch Filter(s) {
	case LocalOnly:
		return LocalOnly
	case GitOnly:
		return GitOnly
	default:
		return Both
	}
}

// Next cycles the filter for the Timeline's filter key: both → local → git.
func (f Filter) Next() Filter {
	switch f {
	case Both:
		return LocalOnly
	case LocalOnly:
		return GitOnly
	default:
		return Both
	}
}

// Label names the filter for the list footer.
func (f Filter) Label() string {
	switch f {
	case LocalOnly:
		return "local only"
	case GitOnly:
		return "git only"
	default:
		return "local + git"
	}
}

// Shows reports whether the filter admits entries of source s.
func (f Filter) Shows(s Source) bool {
	switch f {
	case LocalOnly:
		return s == Local
	case GitOnly:
		return s == Git
	default:
		return true
	}
}

// FromSnapshots converts local-history entries into timeline entries.
func FromSnapshots(entries []localhistory.Entry) []Entry {
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, Entry{Source: Local, Time: e.Time, Hash: e.Hash, Label: e.Label})
	}
	return out
}

// FromCommits converts a file's commit history into timeline entries.
func FromCommits(entries []vcs.FileLogEntry) []Entry {
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, Entry{
			Source:    Git,
			Time:      e.Time,
			Hash:      e.Hash,
			ShortHash: e.ShortHash,
			Author:    e.Author,
			Subject:   e.Subject,
			Path:      e.Path,
		})
	}
	return out
}

// Merge returns the entries the filter admits, newest first. At an identical
// timestamp the snapshot ranks as the later event and comes first: a commit
// and the save of its content share a second only when the save produced what
// was committed, so the snapshot is what the user edited last. Entries of the
// same source and timestamp keep their input order (both input lists arrive
// newest-first), so the ordering is stable and never reshuffles between loads.
func Merge(local, git []Entry, f Filter) []Entry {
	out := make([]Entry, 0, len(local)+len(git))
	if f.Shows(Local) {
		out = append(out, local...)
	}
	if f.Shows(Git) {
		out = append(out, git...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.Time.Equal(b.Time) {
			return a.Time.After(b.Time)
		}
		return a.Source == Local && b.Source == Git
	})
	return out
}
