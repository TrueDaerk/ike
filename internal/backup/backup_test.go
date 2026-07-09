package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixedClock returns a clock function pinned to t.
func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestSnapshotAndListRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	s := New(dir, fixedClock(ts))
	base := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	doc := Doc{Key: "/proj/a.go", Path: "/proj/a.go", Text: "line1\n\nline3\n", BaseMTime: base, BaseHash: "deadbeef"}
	if err := s.Snapshot(doc); err != nil {
		t.Fatal(err)
	}
	got, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("List len = %d, want 1", len(got))
	}
	snap := got[0]
	if snap.Key != doc.Key || snap.Path != doc.Path || !snap.HasBase {
		t.Fatalf("header mismatch: %+v", snap)
	}
	if snap.Text != doc.Text {
		t.Fatalf("text = %q want %q (blank lines must survive)", snap.Text, doc.Text)
	}
	if !snap.BaseMTime.Equal(base) || snap.BaseHash != "deadbeef" {
		t.Fatalf("base info mismatch: %v / %q", snap.BaseMTime, snap.BaseHash)
	}
	if !snap.Timestamp.Equal(ts) {
		t.Fatalf("timestamp = %v want %v", snap.Timestamp, ts)
	}
}

func TestSnapshotOverwritesSameKey(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, fixedClock(time.Unix(0, 0)))
	_ = s.Snapshot(Doc{Key: "k", Path: "/f", Text: "v1"})
	_ = s.Snapshot(Doc{Key: "k", Path: "/f", Text: "v2"})
	got, _ := s.List()
	if len(got) != 1 {
		t.Fatalf("want one file for one key, got %d", len(got))
	}
	if got[0].Text != "v2" {
		t.Fatalf("latest text = %q", got[0].Text)
	}
}

func TestUntitledMarkedNoBase(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, fixedClock(time.Unix(0, 0)))
	if err := s.Snapshot(Doc{Key: "untitled:1", Path: "", Text: "scratch"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.List()
	if len(got) != 1 || got[0].HasBase || got[0].Path != "" {
		t.Fatalf("untitled buffer should have no base: %+v", got)
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, fixedClock(time.Unix(0, 0)))
	_ = s.Snapshot(Doc{Key: "k", Path: "/f", Text: "x"})
	if err := s.Remove("k"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.List(); len(got) != 0 {
		t.Fatalf("snapshot should be gone, got %d", len(got))
	}
	// Removing a missing snapshot is a no-op, not an error.
	if err := s.Remove("nope"); err != nil {
		t.Fatalf("removing missing snapshot: %v", err)
	}
}

func TestAtomicWriteLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, fixedClock(time.Unix(0, 0)))
	_ = s.Snapshot(Doc{Key: "k", Path: "/f", Text: "x"})
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ext) {
			t.Fatalf("unexpected leftover file: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Fatalf("want exactly one snapshot file, got %d", len(entries))
	}
}

func TestListSkipsJunkAndMalformed(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, fixedClock(time.Unix(0, 0)))
	_ = s.Snapshot(Doc{Key: "good", Path: "/f", Text: "ok"})
	// A non-snapshot file and a malformed snapshot must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad"+ext), []byte("garbage no header"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "good" {
		t.Fatalf("List should skip junk/malformed, got %+v", got)
	}
}

func TestListEmptyOrMissingDir(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "does-not-exist"), fixedClock(time.Unix(0, 0)))
	got, err := s.List()
	if err != nil || got != nil {
		t.Fatalf("missing dir should yield (nil, nil), got (%v, %v)", got, err)
	}
}

func TestListOrdersByTimestamp(t *testing.T) {
	dir := t.TempDir()
	older := New(dir, fixedClock(time.Unix(100, 0)))
	newer := New(dir, fixedClock(time.Unix(200, 0)))
	_ = newer.Snapshot(Doc{Key: "b", Path: "/b", Text: "b"})
	_ = older.Snapshot(Doc{Key: "a", Path: "/a", Text: "a"})
	got, _ := older.List()
	if len(got) != 2 || got[0].Key != "a" || got[1].Key != "b" {
		t.Fatalf("want oldest-first [a,b], got %+v", got)
	}
}

func TestBaseInfo(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(f, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	mtime, hash, ok := BaseInfo(f)
	if !ok || hash == "" || mtime.IsZero() {
		t.Fatalf("BaseInfo(existing) = %v %q %v", mtime, hash, ok)
	}
	// sha256("hello")
	const wantHash = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if hash != wantHash {
		t.Fatalf("hash = %q want %q", hash, wantHash)
	}
	if _, _, ok := BaseInfo(""); ok {
		t.Fatal("empty path should be ok=false")
	}
	if _, _, ok := BaseInfo(filepath.Join(dir, "missing")); ok {
		t.Fatal("missing file should be ok=false")
	}
}

func TestPruneRemovesOnlyOldSnapshots(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)

	old := New(dir, fixedClock(now.Add(-8*24*time.Hour)))
	if err := old.Snapshot(Doc{Key: "old", Text: "stale"}); err != nil {
		t.Fatal(err)
	}
	fresh := New(dir, fixedClock(now.Add(-time.Hour)))
	if err := fresh.Snapshot(Doc{Key: "fresh", Text: "recent"}); err != nil {
		t.Fatal(err)
	}

	svc := New(dir, fixedClock(now))
	pruned, err := svc.Prune(7 * 24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 1 {
		t.Fatalf("pruned = %d, want 1", pruned)
	}
	snaps, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 || snaps[0].Key != "fresh" {
		t.Fatalf("prune must keep the fresh snapshot only, got %+v", snaps)
	}
}

func TestPurgeRemovesEverySnapshot(t *testing.T) {
	dir := t.TempDir()
	svc := New(dir, fixedClock(time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)))
	for _, k := range []string{"a", "b"} {
		if err := svc.Snapshot(Doc{Key: k, Text: k}); err != nil {
			t.Fatal(err)
		}
	}
	// A malformed .ikebak must go too — purge means nothing left behind.
	if err := os.WriteFile(filepath.Join(dir, "junk.ikebak"), []byte("not a snapshot"), 0o644); err != nil {
		t.Fatal(err)
	}

	purged, err := svc.Purge()
	if err != nil {
		t.Fatal(err)
	}
	if purged != 3 {
		t.Fatalf("purged = %d, want 3", purged)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".ikebak") {
			t.Fatalf("purge left %s behind", e.Name())
		}
	}
}

func TestPruneAndPurgeOnMissingDir(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "nope"), nil)
	if n, err := svc.Prune(time.Hour); err != nil || n != 0 {
		t.Fatalf("prune on missing dir: n=%d err=%v", n, err)
	}
	if n, err := svc.Purge(); err != nil || n != 0 {
		t.Fatalf("purge on missing dir: n=%d err=%v", n, err)
	}
}
