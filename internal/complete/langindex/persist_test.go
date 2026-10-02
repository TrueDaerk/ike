package langindex

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// TestPersistSeedSkipsUnchangedFiles (#2885): a seeded scan takes a file
// whose stamp still matches from the seed without extracting it, re-reads a
// file whose stamp moved, never resurrects a seeded file that is gone or
// that the walk skips, and records stamps for the next session.
func TestPersistSeedSkipsUnchangedFiles(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.foo", "foo-a")
	b := write(t, dir, "b.foo", "foo-b")

	// Cold run: everything is read and every value carries a stamp.
	var calls int32
	cold := newIndex(t, dir, &calls)
	saved := make(chan map[string]Seeded[string], 1)
	cold.SetPersist(Persist[string]{Saved: func(id string) {
		out := map[string]Seeded[string]{}
		cold.EachStamped(id, func(p string, st Stamp, v string) { out[p] = Seeded[string]{Stamp: st, Value: v} })
		saved <- out
	}})
	cold.Ensure("foo")
	waitDone(t, cold, "foo")
	seed := <-saved
	if len(seed) != 2 || seed[a].Stamp.Size != int64(len("foo-a")) || seed[a].Stamp.ModTime == 0 {
		t.Fatalf("cold run saved %+v, want both files stamped", seed)
	}
	if got := cold.Cached("foo"); got != 0 {
		t.Fatalf("cold run cached = %d, want 0", got)
	}

	// Between sessions: b changes (size moves), c is new, and the seed
	// carries a file that is gone and one under a skipped directory.
	write(t, dir, "b.foo", "foo-b-changed")
	write(t, dir, "c.foo", "foo-c")
	seed[filepath.Join(dir, "gone.foo")] = Seeded[string]{Value: "ghost"}
	skipped := write(t, dir, "node_modules/s.foo", "skipped")
	info, err := os.Stat(skipped)
	if err != nil {
		t.Fatal(err)
	}
	seed[skipped] = Seeded[string]{Stamp: stampOf(info), Value: "skipped"}

	calls = 0
	warm := newIndex(t, dir, &calls)
	warm.SetPersist(Persist[string]{Load: func(string) map[string]Seeded[string] { return seed }})
	warm.Ensure("foo")
	waitDone(t, warm, "foo")
	got := collect(warm, "foo")
	want := map[string]string{"a.foo": "foo-a", "b.foo": "foo-b-changed", "c.foo": "foo-c"}
	if len(got) != len(want) {
		t.Fatalf("warm index = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("warm index = %v, want %v", got, want)
		}
	}
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Fatalf("warm run extracted %d files, want 2 (b changed, c new)", n)
	}
	if got := warm.Cached("foo"); got != 1 {
		t.Fatalf("warm run cached = %d, want 1", got)
	}
	warm.EachStamped("foo", func(p string, st Stamp, _ string) {
		if p == b && st.Size != int64(len("foo-b-changed")) {
			t.Errorf("b's stamp = %+v, want the new size", st)
		}
	})
}
