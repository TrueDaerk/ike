package httphistory

import (
	"os"
	"testing"
	"time"
)

// session_test.go covers the in-memory history of `# @no-log` exchanges
// (#3115).

func TestSessionRingAndNil(t *testing.T) {
	var none *Session
	none.Append("a.http", "k", Entry{}) // must not panic
	if none.List("a.http", "k") != nil {
		t.Fatal("nil session lists nothing")
	}
	s := NewSession()
	base := time.Unix(1000, 0)
	for i := 0; i < MaxPerRequest+2; i++ {
		s.Append("a.http", "k", Entry{Time: base.Add(time.Duration(i) * time.Second), StatusCode: i})
	}
	got := s.List("a.http", "k")
	if len(got) != MaxPerRequest || got[0].StatusCode != MaxPerRequest+1 {
		t.Fatalf("ring = %d entries, newest %d", len(got), got[0].StatusCode)
	}
	if s.List("a.http", "other") != nil || s.List("b.http", "k") != nil {
		t.Error("entries must be keyed by source and request")
	}
}

func TestMergeNewestFirst(t *testing.T) {
	at := func(sec int64) Entry { return Entry{Time: time.Unix(sec, 0), StatusCode: int(sec)} }
	got := Merge([]Entry{at(5), at(3), at(1)}, []Entry{at(4), at(2)})
	want := []int{5, 4, 3, 2, 1}
	if len(got) != len(want) {
		t.Fatalf("merged %d entries", len(got))
	}
	for i, w := range want {
		if got[i].StatusCode != w {
			t.Errorf("entry %d = %d, want %d", i, got[i].StatusCode, w)
		}
	}
	if len(Merge(nil, []Entry{at(1)})) != 1 || len(Merge([]Entry{at(1)}, nil)) != 1 {
		t.Error("one empty side returns the other")
	}
}

// A session entry's capture is read like a stored one's, and the newest value
// wins across both.
func TestCapturedInMergesSession(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	sess := NewSession()
	store.Append("a.http", "k", Entry{Time: time.Unix(10, 0), Captured: map[string]string{"token": "old", "id": "1"}})
	sess.Append("a.http", "k", Entry{Time: time.Unix(20, 0), Captured: map[string]string{"token": "new"}})
	got := CapturedIn([]string{"k"}, func(key string) []Entry {
		return Merge(store.List("a.http", key), sess.List("a.http", key))
	})
	if got["token"] != "new" || got["id"] != "1" {
		t.Errorf("captured = %v", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("only the stored entry is on disk, got %d files", len(entries))
	}
}
