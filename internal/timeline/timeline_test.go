package timeline

import (
	"testing"
	"time"

	"ike/internal/localhistory"
	"ike/internal/vcs"
)

// timeline_test.go covers the merge layer of the per-file Timeline (#1916):
// chronological ordering across the two sources, the equal-timestamp
// tie-break, the source filter and the converters.

// at returns a fixed base time offset by n minutes, so cases read as an axis.
func at(n int) time.Time {
	return time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC).Add(time.Duration(n) * time.Minute)
}

func snap(n int, hash string) Entry { return Entry{Source: Local, Time: at(n), Hash: hash} }

func commit(n int, hash string) Entry {
	return Entry{Source: Git, Time: at(n), Hash: hash, ShortHash: hash, Subject: "s" + hash}
}

// hashes names the merged order compactly for assertions.
func hashes(entries []Entry) string {
	out := ""
	for _, e := range entries {
		out += e.Hash
	}
	return out
}

func TestMergeOrdersBothSourcesNewestFirst(t *testing.T) {
	// Interleaved timestamps: two snapshots around two commits.
	local := []Entry{snap(40, "d"), snap(10, "b")}
	git := []Entry{commit(30, "c"), commit(0, "a")}
	if got := hashes(Merge(local, git, Both)); got != "dcba" {
		t.Fatalf("merged order = %q, want dcba", got)
	}
	// Both halves arrive newest-first; a git window appended later must not
	// change the ordering of what was already shown.
	git = append(git, commit(-10, "z"))
	if got := hashes(Merge(local, git, Both)); got != "dcbaz" {
		t.Fatalf("after loading an older window = %q, want dcbaz", got)
	}
}

func TestMergeTieBreaksSnapshotAfterCommit(t *testing.T) {
	// Identical timestamps: the snapshot is the later event, so it comes first
	// in the newest-first list — the save produced what was committed.
	local := []Entry{snap(10, "s")}
	git := []Entry{commit(10, "c")}
	if got := hashes(Merge(local, git, Both)); got != "sc" {
		t.Fatalf("tie order = %q, want sc (snapshot after the commit)", got)
	}
	// Input order must not decide it: the same pair, git listed first.
	if got := hashes(Merge(nil, git, Both)); got != "c" {
		t.Fatalf("git-only tie = %q", got)
	}
	// Same source, same timestamp: input order is preserved (stable sort).
	local = []Entry{snap(10, "1"), snap(10, "2"), snap(10, "3")}
	if got := hashes(Merge(local, nil, Both)); got != "123" {
		t.Fatalf("equal-time snapshots reshuffled: %q", got)
	}
}

func TestMergeFilters(t *testing.T) {
	local := []Entry{snap(20, "s")}
	git := []Entry{commit(10, "c")}
	if got := hashes(Merge(local, git, LocalOnly)); got != "s" {
		t.Fatalf("local filter = %q", got)
	}
	if got := hashes(Merge(local, git, GitOnly)); got != "c" {
		t.Fatalf("git filter = %q", got)
	}
	// An untracked file has no git half; a file without snapshots no local one.
	if got := hashes(Merge(local, nil, Both)); got != "s" {
		t.Fatalf("untracked file = %q", got)
	}
	if got := hashes(Merge(nil, git, Both)); got != "c" {
		t.Fatalf("file without snapshots = %q", got)
	}
	if got := Merge(nil, nil, Both); len(got) != 0 {
		t.Fatalf("empty merge = %+v", got)
	}
}

func TestFilterParsingAndCycle(t *testing.T) {
	for value, want := range map[string]Filter{
		"both": Both, "local": LocalOnly, "git": GitOnly, "": Both, "nonsense": Both,
	} {
		if got := ParseFilter(value); got != want {
			t.Fatalf("ParseFilter(%q) = %q, want %q", value, got, want)
		}
	}
	f := Both
	for _, want := range []Filter{LocalOnly, GitOnly, Both} {
		if f = f.Next(); f != want {
			t.Fatalf("cycle reached %q, want %q", f, want)
		}
	}
	if Both.Label() == "" || LocalOnly.Label() == "" || GitOnly.Label() == "" {
		t.Fatal("every filter needs a footer label")
	}
	if !Both.Shows(Local) || !Both.Shows(Git) || LocalOnly.Shows(Git) || GitOnly.Shows(Local) {
		t.Fatal("Shows disagrees with the filter")
	}
	if Local.Icon() == Git.Icon() {
		t.Fatal("the two sources must be distinguishable by icon")
	}
}

func TestConvertersCarryTheirFields(t *testing.T) {
	got := FromSnapshots([]localhistory.Entry{{Time: at(0), Hash: "abc", Label: "before refactor"}})
	if len(got) != 1 || got[0].Source != Local || got[0].Hash != "abc" || got[0].Label != "before refactor" {
		t.Fatalf("snapshot conversion = %+v", got)
	}
	commits := FromCommits([]vcs.FileLogEntry{{
		LogEntry: vcs.LogEntry{Hash: "f00", ShortHash: "f0", Author: "Ada", Subject: "fix", Time: at(1)},
		Path:     "old/name.go",
	}})
	if len(commits) != 1 {
		t.Fatalf("commit conversion = %+v", commits)
	}
	c := commits[0]
	if c.Source != Git || c.Hash != "f00" || c.ShortHash != "f0" || c.Author != "Ada" ||
		c.Subject != "fix" || c.Path != "old/name.go" || !c.Time.Equal(at(1)) {
		t.Fatalf("commit entry = %+v", c)
	}
}
