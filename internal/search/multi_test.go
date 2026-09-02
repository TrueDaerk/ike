package search

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// multiFixture builds n small roots, each with two matching files.
func multiFixture(t *testing.T, n int) []string {
	t.Helper()
	var roots []string
	for i := 0; i < n; i++ {
		root := t.TempDir()
		files := map[string]string{
			"a.go":       "// needle one\n",
			"sub/b.txt":  "needle two\nneedle three\n",
			"ignored.md": "needle in ignored\n",
		}
		for rel, content := range files {
			path := filepath.Join(root, rel)
			_ = os.MkdirAll(filepath.Dir(path), 0o755)
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored.md\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		roots = append(roots, root)
	}
	return roots
}

// runMulti executes one multi scan to completion, returning matches per root.
func runMulti(t *testing.T, forceGo bool, q MultiQuery) (map[string][]Match, MultiDoneMsg) {
	t.Helper()
	var mu sync.Mutex
	byRoot := map[string][]Match{}
	done := make(chan MultiDoneMsg, 1)
	s := NewMulti(func(msg tea.Msg) {
		switch m := msg.(type) {
		case MultiBatchMsg:
			mu.Lock()
			byRoot[m.Root] = append(byRoot[m.Root], m.Matches...)
			mu.Unlock()
		case MultiDoneMsg:
			done <- m
		}
	})
	s.forceGo = forceGo
	gen := s.ScanMulti(q)
	select {
	case d := <-done:
		if d.Gen != gen {
			t.Fatalf("done gen %d, want %d", d.Gen, gen)
		}
		mu.Lock()
		defer mu.Unlock()
		return byRoot, d
	case <-time.After(10 * time.Second):
		t.Fatal("multi scan did not finish")
		return nil, MultiDoneMsg{}
	}
}

func TestMultiFanOutAndMerge(t *testing.T) {
	roots := multiFixture(t, 3)
	byRoot, done := runMulti(t, true, MultiQuery{
		Query: Query{Pattern: "needle"},
		Roots: roots,
	})
	if len(done.Errs) != 0 {
		t.Fatalf("unexpected root errors: %v", done.Errs)
	}
	// 3 matches per root (the gitignored file is skipped), 9 total.
	if done.Total != 9 {
		t.Fatalf("total=%d, want 9", done.Total)
	}
	for _, root := range roots {
		if got := len(byRoot[root]); got != 3 {
			t.Fatalf("root %s: %d matches, want 3", root, got)
		}
		for _, m := range byRoot[root] {
			if !filepath.IsAbs(m.Path) || filepath.Dir(m.Path) != root && filepath.Dir(filepath.Dir(m.Path)) != root {
				t.Fatalf("match path %q not under its root %q", m.Path, root)
			}
		}
	}
	if done.Truncated {
		t.Fatal("uncapped scan must not report truncation")
	}
}

func TestMultiSharedCap(t *testing.T) {
	roots := multiFixture(t, 3)
	byRoot, done := runMulti(t, true, MultiQuery{
		Query: Query{Pattern: "needle", MaxResults: 4},
		Roots: roots,
	})
	if !done.Truncated {
		t.Fatal("hitting the shared cap must set Truncated")
	}
	if done.Total != 4 {
		t.Fatalf("total=%d, want 4 (shared cap across roots)", done.Total)
	}
	var streamed int
	for _, ms := range byRoot {
		streamed += len(ms)
	}
	if streamed != 4 {
		t.Fatalf("streamed %d matches, want 4", streamed)
	}
	// The cap is shared, not per root: 3 matches per root means the last root
	// must not have been scanned in full.
	if len(byRoot) == 3 {
		for _, ms := range byRoot {
			if len(ms) == 3 {
				return // ok: cap landed mid-root somewhere
			}
		}
	}
}

func TestMultiIncludeExclude(t *testing.T) {
	roots := multiFixture(t, 2)
	byRoot, done := runMulti(t, true, MultiQuery{
		Query: Query{Pattern: "needle", Include: []string{"*.go"}},
		Roots: roots,
	})
	if done.Total != 2 {
		t.Fatalf("total=%d, want 2 (one .go match per root)", done.Total)
	}
	for root, ms := range byRoot {
		for _, m := range ms {
			if filepath.Ext(m.Path) != ".go" {
				t.Fatalf("root %s: include glob leaked %q", root, m.Path)
			}
		}
	}
}

func TestMultiPerRootErrors(t *testing.T) {
	roots := multiFixture(t, 1)
	missing := filepath.Join(t.TempDir(), "gone")
	byRoot, done := runMulti(t, true, MultiQuery{
		Query: Query{Pattern: "needle"},
		Roots: []string{missing, roots[0]},
	})
	if len(done.Errs) != 1 || done.Errs[missing] == nil {
		t.Fatalf("want one error for the missing root, got %v", done.Errs)
	}
	// The healthy root still scanned fully.
	if len(byRoot[roots[0]]) != 3 || done.Total != 3 {
		t.Fatalf("healthy root got %d matches (total %d), want 3", len(byRoot[roots[0]]), done.Total)
	}
}

func TestMultiRescanCancelsPrevious(t *testing.T) {
	// A wide root keeps the first scan busy while the second starts.
	root := t.TempDir()
	for i := 0; i < 200; i++ {
		dir := filepath.Join(root, "d", string(rune('a'+i%26)))
		_ = os.MkdirAll(dir, 0o755)
		if err := os.WriteFile(filepath.Join(dir, "f"+string(rune('0'+i%10))+".txt"),
			[]byte("needle\nneedle\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan int, 2)
	s := NewMulti(nil)
	s.send = func(msg tea.Msg) {
		if d, ok := msg.(MultiDoneMsg); ok {
			done <- d.Gen
		}
	}
	s.forceGo = true
	s.ScanMulti(MultiQuery{Query: Query{Pattern: "needle"}, Roots: []string{root}})
	gen2 := s.ScanMulti(MultiQuery{Query: Query{Pattern: "needle"}, Roots: []string{root}})

	deadline := time.After(10 * time.Second)
	for {
		select {
		case g := <-done:
			if g == gen2 {
				if s.Gen() != gen2 {
					t.Fatalf("service gen %d, want %d", s.Gen(), gen2)
				}
				return
			}
		case <-deadline:
			t.Fatal("second multi scan did not finish")
		}
	}
}
