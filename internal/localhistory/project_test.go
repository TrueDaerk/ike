package localhistory

import (
	"path/filepath"
	"testing"
	"time"
)

// seeded returns a store on a frozen clock plus the knob that moves it, so a
// test decides exactly which timestamp each Record writes (Record reads the
// clock more than once — pruning consults it too).
func seeded(t *testing.T) (*Store, func(time.Time)) {
	t.Helper()
	s := New(t.TempDir())
	at := time.Now()
	s.now = func() time.Time { return at }
	return s, func(next time.Time) { at = next }
}

// TestScanOrdersAcrossFilesNewestFirst: the project-wide scan mixes every
// file's snapshots into one newest-first list carrying the file each row
// belongs to.
func TestScanOrdersAcrossFilesNewestFirst(t *testing.T) {
	base := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	s, clock := seeded(t)
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")

	clock(base.Add(time.Minute))
	s.Record(a, []byte("a1\n"))
	clock(base.Add(2 * time.Minute))
	s.Record(b, []byte("b1\n"))
	clock(base.Add(3 * time.Minute))
	s.Record(a, []byte("a2\n"))

	snaps, truncated := s.Scan(0)
	if truncated {
		t.Fatal("Scan reported truncation for three snapshots")
	}
	if len(snaps) != 3 {
		t.Fatalf("Scan = %d snapshots, want 3", len(snaps))
	}
	wantPaths := []string{a, b, a}
	wantTimes := []time.Time{base.Add(3 * time.Minute), base.Add(2 * time.Minute), base.Add(time.Minute)}
	for i, sn := range snaps {
		if sn.Path != wantPaths[i] {
			t.Fatalf("row %d path = %s, want %s", i, sn.Path, wantPaths[i])
		}
		if !sn.Time.Equal(wantTimes[i]) {
			t.Fatalf("row %d time = %v, want %v", i, sn.Time, wantTimes[i])
		}
	}
	// The rows still address their content, so the panel can read a blob.
	data, err := s.Read(snaps[0].Hash)
	if err != nil || string(data) != "a2\n" {
		t.Fatalf("newest row reads %q (err %v), want %q", data, err, "a2\n")
	}
}

// TestScanCapsToNewest: the scan is bounded — it keeps the newest window and
// says that it cut the tail, so a huge store cannot stall the timeline.
func TestScanCapsToNewest(t *testing.T) {
	base := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	s, clock := seeded(t)
	path := filepath.Join(t.TempDir(), "a.txt")
	for i := range 6 {
		clock(base.Add(time.Duration(i+1) * time.Minute))
		s.Record(path, []byte{byte('0' + i), '\n'})
	}

	snaps, truncated := s.Scan(2)
	if !truncated {
		t.Fatal("Scan(2) over six snapshots did not report truncation")
	}
	if len(snaps) != 2 {
		t.Fatalf("Scan(2) = %d snapshots, want 2", len(snaps))
	}
	if !snaps[0].Time.Equal(base.Add(6*time.Minute)) || !snaps[1].Time.Equal(base.Add(5*time.Minute)) {
		t.Fatalf("Scan(2) kept %v / %v, want the two newest", snaps[0].Time, snaps[1].Time)
	}
}

// TestScanEmptyStore: a store that never recorded anything scans to nothing
// instead of erroring.
func TestScanEmptyStore(t *testing.T) {
	snaps, truncated := New(t.TempDir()).Scan(0)
	if len(snaps) != 0 || truncated {
		t.Fatalf("Scan of an empty store = %d snapshots (truncated %v), want 0/false", len(snaps), truncated)
	}
}

// TestGroupByDayBuckets: an ordered list buckets into Today / Yesterday /
// dated days, in that order, without reordering the rows inside a bucket.
func TestGroupByDayBuckets(t *testing.T) {
	now := time.Date(2026, 8, 24, 14, 30, 0, 0, time.UTC)
	snaps := []Snapshot{
		{Path: "a", Entry: Entry{Time: now.Add(-time.Hour)}},      // today
		{Path: "b", Entry: Entry{Time: now.Add(-2 * time.Hour)}},  // today
		{Path: "c", Entry: Entry{Time: now.Add(-20 * time.Hour)}}, // yesterday 18:30
		{Path: "d", Entry: Entry{Time: now.Add(-72 * time.Hour)}}, // 2026-08-21
	}
	days := GroupByDay(snaps, now)
	if len(days) != 3 {
		t.Fatalf("GroupByDay = %d buckets, want 3: %+v", len(days), days)
	}
	want := []struct {
		label string
		paths []string
	}{
		{"Today", []string{"a", "b"}},
		{"Yesterday", []string{"c"}},
		{"Fri 2026-08-21", []string{"d"}},
	}
	for i, w := range want {
		if days[i].Label != w.label {
			t.Fatalf("bucket %d label = %q, want %q", i, days[i].Label, w.label)
		}
		if len(days[i].Snapshots) != len(w.paths) {
			t.Fatalf("bucket %q = %d rows, want %d", w.label, len(days[i].Snapshots), len(w.paths))
		}
		for j, p := range w.paths {
			if days[i].Snapshots[j].Path != p {
				t.Fatalf("bucket %q row %d = %s, want %s", w.label, j, days[i].Snapshots[j].Path, p)
			}
		}
	}
}

// TestDayLabelBoundaries: the buckets are calendar days, not 24-hour windows —
// one minute before midnight is "Yesterday", not "Today", and a timestamp in
// the future (a clock jump) groups under "Today" rather than a negative day.
func TestDayLabelBoundaries(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 30, 0, 0, time.UTC)
	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"same day", now.Add(-time.Minute), "Today"},
		{"one minute before midnight", now.Add(-31 * time.Minute), "Yesterday"},
		{"two days back", now.Add(-48 * time.Hour), "Sat 2026-08-22"},
		{"in the future", now.Add(time.Hour), "Today"},
	}
	for _, c := range cases {
		if got := DayLabel(c.at, now); got != c.want {
			t.Fatalf("%s: DayLabel(%v) = %q, want %q", c.name, c.at, got, c.want)
		}
	}
}
