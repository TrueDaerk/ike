package localhistory

import (
	"sort"
	"time"
)

// project.go is the project-wide read side of the store (#2171): the per-file
// List answers "what happened to this file", Scan answers "what did I change
// today" across every file the index knows. It is the data layer only —
// collecting, ordering and day-grouping snapshots — so the aggregation is
// testable without a UI.

// DefaultScanCap bounds one project-wide scan. The index is already pruned
// (50 entries per file, 30 days), but a long-lived project with hundreds of
// touched files can still hold tens of thousands of entries, and a timeline
// nobody scrolls to the end of must not pay for them: the scan keeps the
// newest DefaultScanCap snapshots and reports that it cut the tail.
const DefaultScanCap = 2000

// Snapshot is one entry together with the file it belongs to — the row type
// of the project-wide timeline, where a list mixes files.
type Snapshot struct {
	Path string
	Entry
}

// Day is one day bucket of a project-wide timeline: the snapshots of that
// calendar day (newest first) under their heading.
type Day struct {
	Label     string
	Snapshots []Snapshot
}

// Scan returns snapshots across every file in the index, newest first, at
// most limit of them (<=0 selects DefaultScanCap). The second result reports
// whether the store held more than the returned window — the tail was cut,
// not the head, so the newest history is always complete.
//
// Ties break on path so the order is total and a re-scan never reshuffles
// rows under the cursor (two files saved in the same "Save All" share a
// timestamp down to the nanosecond only rarely, but a Save All that writes
// from a cached clock does exactly that).
func (s *Store) Scan(limit int) ([]Snapshot, bool) {
	if s == nil || s.Dir == "" {
		return nil, false
	}
	if limit <= 0 {
		limit = DefaultScanCap
	}
	idx := s.load()
	out := make([]Snapshot, 0, 64)
	for path, entries := range idx.Files {
		for _, e := range entries {
			out = append(out, Snapshot{Path: path, Entry: e})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.Time.Equal(b.Time) {
			return a.Time.After(b.Time)
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Hash < b.Hash
	})
	if len(out) > limit {
		return out[:limit], true
	}
	return out, false
}

// GroupByDay buckets an already-ordered (newest-first) snapshot list into
// calendar days in that same order, so the timeline reads "Today" first. The
// bucket boundary is the local midnight of now's location, matching how the
// rows render their times.
func GroupByDay(snaps []Snapshot, now time.Time) []Day {
	var out []Day
	for _, sn := range snaps {
		label := DayLabel(sn.Time, now)
		if n := len(out); n > 0 && out[n-1].Label == label {
			out[n-1].Snapshots = append(out[n-1].Snapshots, sn)
			continue
		}
		out = append(out, Day{Label: label, Snapshots: []Snapshot{sn}})
	}
	return out
}

// DayLabel names the day bucket t falls into, relative to now: "Today" and
// "Yesterday" for the two days a user thinks of by name, the weekday and date
// ("Mon 2026-08-24") for everything older — a bare date says little about how
// long ago it was, and a bare relative age ("3d ago") does not group.
func DayLabel(t, now time.Time) string {
	day := t.In(now.Location())
	switch daysBetween(day, now) {
	case 0:
		return "Today"
	case 1:
		return "Yesterday"
	default:
		return day.Format("Mon 2006-01-02")
	}
}

// daysBetween counts calendar days from t's midnight to now's, both in now's
// location. A future timestamp (a clock jump, a restored index) counts as 0,
// so it groups under "Today" rather than under a negative label.
func daysBetween(t, now time.Time) int {
	loc := now.Location()
	t = t.In(loc)
	a := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	b := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	d := int(b.Sub(a).Hours() / 24)
	if d < 0 {
		return 0
	}
	return d
}
