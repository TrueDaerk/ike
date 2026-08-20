package changefeed

import (
	"strings"
	"testing"
	"time"
)

// at builds a deterministic timestamp n seconds into the test's fake session.
func at(n int) time.Time {
	return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC).Add(time.Duration(n) * time.Second)
}

// entry is a Changed entry with pre-change content from the buffer.
func entry(path string, n int, before string) Entry {
	return Entry{Path: path, Time: at(n), Kind: Changed, Before: before, Origin: FromBuffer}
}

// TestAddOrdersNewestFirst: the feed is a review queue — the file touched last
// is the one the user wants to look at first.
func TestAddOrdersNewestFirst(t *testing.T) {
	f := New()
	f.Add(entry("a.go", 1, "a"))
	f.Add(entry("b.go", 2, "b"))
	f.Add(entry("c.go", 3, "c"))

	got := f.Entries()
	want := []string{"c.go", "b.go", "a.go"}
	if len(got) != len(want) {
		t.Fatalf("Entries = %d, want %d", len(got), len(want))
	}
	for i, path := range want {
		if got[i].Path != path {
			t.Fatalf("Entries[%d] = %q, want %q", i, got[i].Path, path)
		}
	}
}

// TestAddCoalescesPerPath: repeated writes to one file are one row that moves
// back to the front, counts up, and keeps the content from *before the first*
// write — reverting an agent's fifth rewrite must restore what the file held
// before it started, not before the last one.
func TestAddCoalescesPerPath(t *testing.T) {
	f := New()
	f.Add(entry("a.go", 1, "original"))
	f.Add(entry("b.go", 2, "b"))
	f.Add(entry("a.go", 3, "after the first write"))

	got := f.Entries()
	if len(got) != 2 {
		t.Fatalf("Entries = %d rows, want 2 (one per file)", len(got))
	}
	if got[0].Path != "a.go" {
		t.Fatalf("front row = %q, want a.go (the re-touched file moves up)", got[0].Path)
	}
	if got[0].Count != 2 {
		t.Fatalf("Count = %d, want 2", got[0].Count)
	}
	if !got[0].Time.Equal(at(3)) {
		t.Fatalf("Time = %v, want the newest event %v", got[0].Time, at(3))
	}
	if got[0].Before != "original" {
		t.Fatalf("Before = %q, want the first-captured content %q", got[0].Before, "original")
	}
}

// TestCoalesceAdoptsLaterBefore: the first event found nothing to compare
// against (the file had never been opened or saved), a later one did — the
// entry adopts it, because some pre-change content beats none.
func TestCoalesceAdoptsLaterBefore(t *testing.T) {
	f := New()
	f.Add(Entry{Path: "a.go", Time: at(1), Kind: Changed})
	if e, _ := f.Get("a.go"); e.HasBefore() {
		t.Fatal("entry claims pre-change content it was never given")
	}
	f.Add(entry("a.go", 2, "snapshot"))

	e, _ := f.Get("a.go")
	if e.Before != "snapshot" || e.Origin != FromBuffer {
		t.Fatalf("Before/Origin = %q/%v, want %q/FromBuffer", e.Before, e.Origin, "snapshot")
	}
	if f.Bytes() != len("snapshot") {
		t.Fatalf("Bytes = %d, want %d (the adopted content is accounted for)", f.Bytes(), len("snapshot"))
	}
}

// TestCoalesceMergesKinds: a removal wins over an earlier change, and a file
// created this session stays "created" however often it is rewritten after.
func TestCoalesceMergesKinds(t *testing.T) {
	f := New()
	f.Add(Entry{Path: "new.go", Time: at(1), Kind: Created})
	f.Add(Entry{Path: "new.go", Time: at(2), Kind: Changed})
	if e, _ := f.Get("new.go"); e.Kind != Created {
		t.Fatalf("Kind = %v after create+write, want Created", e.Kind)
	}
	f.Add(Entry{Path: "new.go", Time: at(3), Kind: Removed})
	if e, _ := f.Get("new.go"); e.Kind != Removed {
		t.Fatalf("Kind = %v after the delete, want Removed", e.Kind)
	}
}

// TestLimitDropsOldest: the cap is respected and the tail — the changes the
// user already scrolled past — is what goes.
func TestLimitDropsOldest(t *testing.T) {
	f := &Feed{Limit: 3}
	for i := 1; i <= 5; i++ {
		f.Add(entry(string(rune('a'+i))+".go", i, "x"))
	}
	got := f.Entries()
	if len(got) != 3 {
		t.Fatalf("Entries = %d, want the cap 3", len(got))
	}
	if got[0].Path != "f.go" || got[2].Path != "d.go" {
		t.Fatalf("kept %q…%q, want f.go…d.go (the three newest)", got[0].Path, got[2].Path)
	}
	if f.Bytes() != 3 {
		t.Fatalf("Bytes = %d, want 3 — dropped rows must release their content", f.Bytes())
	}
}

// TestSetLimitTrimsAndDisables: lowering the setting takes effect on the
// existing list, and zero clears it — the off switch must not leave a stale
// feed behind.
func TestSetLimitTrimsAndDisables(t *testing.T) {
	f := New()
	for i := 1; i <= 5; i++ {
		f.Add(entry(string(rune('a'+i))+".go", i, "x"))
	}
	f.SetLimit(2)
	if f.Len() != 2 {
		t.Fatalf("Len = %d after SetLimit(2), want 2", f.Len())
	}
	f.SetLimit(0)
	if f.Len() != 0 || f.Bytes() != 0 {
		t.Fatalf("Len/Bytes = %d/%d after SetLimit(0), want 0/0", f.Len(), f.Bytes())
	}
	if f.Add(entry("z.go", 9, "x")) {
		t.Fatal("a disabled feed recorded an entry")
	}
}

// TestMaxBytesReleasesOldestContent: a batch touching large files bounds the
// retained content — the oldest rows survive as rows but lose their diff.
func TestMaxBytesReleasesOldestContent(t *testing.T) {
	big := strings.Repeat("x", 100)
	f := &Feed{Limit: 10, MaxBytes: 250}
	for i := 1; i <= 4; i++ {
		f.Add(entry(string(rune('a'+i))+".go", i, big))
	}
	if f.Bytes() > 250 {
		t.Fatalf("Bytes = %d, want <= the 250 budget", f.Bytes())
	}
	got := f.Entries()
	if len(got) != 4 {
		t.Fatalf("Entries = %d, want all 4 rows kept", len(got))
	}
	if !got[0].HasBefore() || !got[1].HasBefore() {
		t.Fatal("the newest rows lost their content first; the oldest should go")
	}
	if got[3].Origin != Dropped || got[3].Before != "" {
		t.Fatalf("oldest row = %v/%q, want Dropped and no content", got[3].Origin, got[3].Before)
	}
}

// TestSingleOversizeContentIsReleased: one file larger than the whole budget
// must not blow it — the row lands, its content does not stay.
func TestSingleOversizeContentIsReleased(t *testing.T) {
	f := &Feed{Limit: 10, MaxBytes: 10}
	f.Add(entry("huge.go", 1, strings.Repeat("x", 100)))
	if f.Len() != 1 {
		t.Fatalf("Len = %d, want the row to land anyway", f.Len())
	}
	e, _ := f.Get("huge.go")
	if e.HasBefore() || e.Origin != Dropped {
		t.Fatalf("Origin/Before = %v/%d bytes, want Dropped and none", e.Origin, len(e.Before))
	}
	if f.Bytes() != 0 {
		t.Fatalf("Bytes = %d, want 0", f.Bytes())
	}
}

// TestIgnoreRejectsNoise: the noise predicate keeps ignored paths out
// entirely, so the feed lists only what the user could care about.
func TestIgnoreRejectsNoise(t *testing.T) {
	f := New()
	f.Ignore = func(path string) bool { return strings.Contains(path, "node_modules") }
	if f.Add(entry("node_modules/x/index.js", 1, "x")) {
		t.Fatal("an ignored path was recorded")
	}
	if !f.Add(entry("src/main.go", 2, "x")) {
		t.Fatal("a normal path was rejected")
	}
	if f.Len() != 1 {
		t.Fatalf("Len = %d, want 1", f.Len())
	}
}

// TestRemoveAndClear: the panel's dismiss and clear actions, including the
// byte accounting they have to unwind.
func TestRemoveAndClear(t *testing.T) {
	f := New()
	f.Add(entry("a.go", 1, "aaa"))
	f.Add(entry("b.go", 2, "bb"))
	if !f.Remove("a.go") {
		t.Fatal("Remove reported no entry for a recorded path")
	}
	if f.Len() != 1 || f.Bytes() != 2 {
		t.Fatalf("Len/Bytes = %d/%d after Remove, want 1/2", f.Len(), f.Bytes())
	}
	if f.Remove("gone.go") {
		t.Fatal("Remove reported an entry for an unrecorded path")
	}
	f.Clear()
	if f.Len() != 0 || f.Bytes() != 0 {
		t.Fatalf("Len/Bytes = %d/%d after Clear, want 0/0", f.Len(), f.Bytes())
	}
}

// TestEntriesIsACopy: the panel iterates the returned slice across renders; a
// watcher event landing meanwhile must not rewrite it underneath.
func TestEntriesIsACopy(t *testing.T) {
	f := New()
	f.Add(entry("a.go", 1, "a"))
	got := f.Entries()
	f.Add(entry("a.go", 2, "later"))
	if got[0].Count != 1 {
		t.Fatalf("the returned slice aliased the feed: Count = %d, want the 1 it was taken with", got[0].Count)
	}
}

// TestNoBeforeOriginCarriesNoContent: an origin that promises nothing must not
// retain bytes, so the memory budget cannot be gamed by a bad caller.
func TestNoBeforeOriginCarriesNoContent(t *testing.T) {
	f := New()
	f.Add(Entry{Path: "a.go", Time: at(1), Kind: Created, Before: "leftover", Origin: NoBefore})
	if e, _ := f.Get("a.go"); e.Before != "" {
		t.Fatalf("Before = %q, want it dropped with the NoBefore origin", e.Before)
	}
	if f.Bytes() != 0 {
		t.Fatalf("Bytes = %d, want 0", f.Bytes())
	}
}

// TestNilFeedIsInert: the app holds the feed as a pointer; every method has to
// survive a nil one rather than panicking a whole session.
func TestNilFeedIsInert(t *testing.T) {
	var f *Feed
	if f.Add(entry("a.go", 1, "x")) || f.Len() != 0 || f.Bytes() != 0 || f.Remove("a.go") {
		t.Fatal("a nil feed did something")
	}
	if _, ok := f.Get("a.go"); ok {
		t.Fatal("a nil feed returned an entry")
	}
	f.SetLimit(5)
	f.Clear()
	if f.Entries() != nil {
		t.Fatal("a nil feed returned entries")
	}
}

// TestKindAndOriginLabels: the panel renders these verbatim, so a renamed
// constant must not silently produce a blank column.
func TestKindAndOriginLabels(t *testing.T) {
	for _, k := range []Kind{Changed, Created, Removed} {
		if k.Label() == "" || k.Icon() == "" {
			t.Fatalf("kind %d renders blank", k)
		}
	}
	for _, o := range []Origin{NoBefore, FromBuffer, FromSnapshot, Dropped} {
		if o.Label() == "" {
			t.Fatalf("origin %d renders blank", o)
		}
	}
}
