package words

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ike/internal/complete"
	"ike/internal/host"
)

func change(path, text string) host.EditorEvent {
	return host.EditorEvent{Kind: host.EditorChange, Path: path, Text: text}
}

func labels(t *testing.T, s *Source, req complete.Request) []string {
	t.Helper()
	items, err := s.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Label
	}
	return out
}

func TestBufferWordsPrefixFiltered(t *testing.T) {
	s := New("")
	s.Observe(change("/a.go", "hello helper unrelated\nhel"))
	// Typing "hel" at line 1 col 3: hello and helper match, the typed word
	// itself ("hel") is excluded, "unrelated" filtered out.
	got := labels(t, s, complete.Request{Path: "/a.go", Line: 1, Col: 3})
	want := []string{"hello", "helper"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLocalityTiersAndCrossBuffer(t *testing.T) {
	s := New("")
	s.Observe(change("/a.go", "alpha\nal"))
	s.Observe(change("/b.go", "albatross"))
	items, err := s.Complete(context.Background(), complete.Request{Path: "/a.go", Line: 1, Col: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Label != "alpha" || items[1].Label != "albatross" {
		t.Fatalf("items = %+v", items)
	}
	// Current buffer sorts before other buffers via the SortText tier.
	if !(items[0].SortText < items[1].SortText) {
		t.Fatalf("tiers: %q vs %q", items[0].SortText, items[1].SortText)
	}
}

func TestChangeRefreshesIndex(t *testing.T) {
	s := New("")
	s.Observe(change("/a.go", "aardvark\naa"))
	if got := labels(t, s, complete.Request{Path: "/a.go", Line: 1, Col: 2}); len(got) != 1 || got[0] != "aardvark" {
		t.Fatalf("got %v", got)
	}
	s.Observe(change("/a.go", "aabandoned\naa"))
	if got := labels(t, s, complete.Request{Path: "/a.go", Line: 1, Col: 2}); len(got) != 1 || got[0] != "aabandoned" {
		t.Fatalf("after change got %v, want the new word only", got)
	}
}

func TestShortAndNumericWordsSkipped(t *testing.T) {
	s := New("")
	s.Observe(change("/a.go", "ab 123abc zz9 valid_name\nva"))
	got := labels(t, s, complete.Request{Path: "/a.go", Line: 1, Col: 2})
	if len(got) != 1 || got[0] != "valid_name" {
		t.Fatalf("got %v, want [valid_name]", got)
	}
}

func TestProjectScan(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.py"), []byte("def projectword(): pass"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "y.js"), []byte("ignoredword"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir)
	for start := time.Now(); !s.ScanDone(); {
		if time.Since(start) > 5*time.Second {
			t.Fatal("scan did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	s.Observe(change("/a.go", "pro"))
	got := labels(t, s, complete.Request{Path: "/a.go", Line: 0, Col: 3})
	if len(got) != 1 || got[0] != "projectword" {
		t.Fatalf("got %v, want [projectword] (node_modules skipped)", got)
	}
}

func TestLargeFileDropsBuffer(t *testing.T) {
	s := New("")
	s.Observe(change("/a.go", "largeword\nla"))
	s.Observe(host.EditorEvent{Kind: host.EditorChange, Path: "/a.go", Large: true})
	if got := labels(t, s, complete.Request{Path: "/a.go", Line: 1, Col: 2}); len(got) != 0 {
		t.Fatalf("large-file buffer must drop its words, got %v", got)
	}
}
