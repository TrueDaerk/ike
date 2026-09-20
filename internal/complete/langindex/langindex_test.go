package langindex

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// langindex_test.go covers the index's own promises (#2652): a language is
// scanned once, on demand, other languages are not read for it, a watcher
// invalidation refreshes one file, and the caps hold.

// fakeLangOf classifies by extension without a registry: a.foo → "foo".
func fakeLangOf(path string) string {
	return strings.TrimPrefix(filepath.Ext(path), ".")
}

func newIndex(t *testing.T, dir string, calls *int32) *Index[string] {
	t.Helper()
	ext := func(path, host, text string, only func(string) bool) map[string]string {
		atomic.AddInt32(calls, 1)
		if only != nil && !only(host) {
			return nil
		}
		return map[string]string{host: strings.TrimSpace(text)}
	}
	return New(dir, Limits{MaxFileSize: 1 << 20, MaxFiles: 100}, fakeLangOf, ext)
}

func waitDone(t *testing.T, x *Index[string], id string) {
	t.Helper()
	for start := time.Now(); !x.Done(id); {
		if time.Since(start) > 5*time.Second {
			t.Fatalf("%s scan did not finish", id)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func collect(x *Index[string], langs ...string) map[string]string {
	out := map[string]string{}
	x.Each(langs, func(p, v string) { out[filepath.Base(p)] = v })
	return out
}

func write(t *testing.T, dir, name, text string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEnsureScansOncePerLanguage(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.foo", "foo-a")
	write(t, dir, "b.bar", "bar-b")
	write(t, dir, "node_modules/c.foo", "skipped")
	write(t, dir, ".hidden/d.foo", "skipped")
	var calls int32
	x := newIndex(t, dir, &calls)

	if got := x.Scanned(); len(got) != 0 {
		t.Fatalf("nothing asked, scanned = %v", got)
	}
	x.Ensure("foo")
	waitDone(t, x, "foo")
	if got := collect(x, "foo"); len(got) != 1 || got["a.foo"] != "foo-a" {
		t.Fatalf("foo files = %v", got)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("extractor calls = %d, want 1 — bar files are not read for foo", calls)
	}
	if x.Done("bar") {
		t.Fatal("bar must not be scanned by the foo request")
	}
	x.Ensure("foo")
	if got := x.Scanned(); len(got) != 1 {
		t.Fatalf("Ensure again must not rescan: %v", got)
	}
	x.Ensure("bar", "")
	waitDone(t, x, "bar")
	if got := collect(x, "foo", "bar"); len(got) != 2 || got["b.bar"] != "bar-b" {
		t.Fatalf("both = %v", got)
	}
	if got := x.Scanned(); len(got) != 2 {
		t.Fatalf("the empty language id is never scanned: %v", got)
	}
}

func TestInvalidateRefreshesScannedLanguages(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "a.foo", "before")
	var calls int32
	x := newIndex(t, dir, &calls)
	x.Ensure("foo")
	waitDone(t, x, "foo")

	write(t, dir, "a.foo", "after")
	x.Invalidate(p)
	for start := time.Now(); collect(x, "foo")["a.foo"] != "after"; {
		if time.Since(start) > 5*time.Second {
			t.Fatalf("invalidation never refreshed: %v", collect(x, "foo"))
		}
		time.Sleep(2 * time.Millisecond)
	}
	// A deleted file drops out.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	x.Invalidate(p)
	for start := time.Now(); len(collect(x, "foo")) != 0; {
		if time.Since(start) > 5*time.Second {
			t.Fatalf("deleted file still indexed: %v", collect(x, "foo"))
		}
		time.Sleep(2 * time.Millisecond)
	}
	// An unscanned language's file is not indexed.
	x.Invalidate(write(t, dir, "b.bar", "x"))
	time.Sleep(20 * time.Millisecond)
	if got := collect(x, "bar"); len(got) != 0 {
		t.Fatalf("bar was never scanned, got %v", got)
	}
}

func TestEmptyRootIsDoneAndEmpty(t *testing.T) {
	var calls int32
	x := newIndex(t, "", &calls)
	x.Ensure("foo")
	if !x.Done("foo") || len(collect(x, "foo")) != 0 {
		t.Fatal("an empty root reads as scanned and empty")
	}
}

func TestConcurrentEnsureIsSafe(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.foo", "foo-a")
	var calls int32
	x := newIndex(t, dir, &calls)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			x.Ensure("foo")
		}()
	}
	wg.Wait()
	waitDone(t, x, "foo")
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("extractor calls = %d, want exactly one scan", calls)
	}
}
