package search

import (
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// fixture builds a small project tree with matches in several files, a
// gitignored directory, a hidden file, and a binary file.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"main.go":          "package main\n// needle here\nfunc Needle() {}\n",
		"sub/util.go":      "// another needle\nvar needleCount = 2\n",
		"sub/notes.txt":    "needle Needle NEEDLE\n",
		".hidden.txt":      "needle in a hidden file\n",
		"ignored/gen.go":   "// needle in ignored dir\n",
		"build.log":        "needle in ignored file\n",
		"sub/data.bin":     "needle\x00binary\n",
		"unrelated/foo.md": "haystack only\n",
	}
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored/\n*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// runScan executes one scan to completion and returns its matches.
func runScan(t *testing.T, forceGo bool, q Query) ([]Match, DoneMsg) {
	t.Helper()
	var mu sync.Mutex
	var matches []Match
	done := make(chan DoneMsg, 1)
	s := New(func(msg tea.Msg) {
		switch m := msg.(type) {
		case BatchMsg:
			mu.Lock()
			matches = append(matches, m.Matches...)
			mu.Unlock()
		case DoneMsg:
			done <- m
		}
	})
	s.forceGo = forceGo
	gen := s.Scan(q)
	select {
	case d := <-done:
		if d.Gen != gen {
			t.Fatalf("done gen %d, want %d", d.Gen, gen)
		}
		if d.Err != nil {
			t.Fatalf("scan error: %v", d.Err)
		}
		mu.Lock()
		defer mu.Unlock()
		return matches, d
	case <-time.After(10 * time.Second):
		t.Fatal("scan did not finish")
		return nil, DoneMsg{}
	}
}

// sortMatches orders matches deterministically for comparison.
func sortMatches(ms []Match) {
	sort.Slice(ms, func(i, j int) bool {
		if ms[i].Path != ms[j].Path {
			return ms[i].Path < ms[j].Path
		}
		if ms[i].Line != ms[j].Line {
			return ms[i].Line < ms[j].Line
		}
		return ms[i].StartCol < ms[j].StartCol
	})
}

func relPaths(t *testing.T, root string, ms []Match) []string {
	t.Helper()
	seen := map[string]bool{}
	var out []string
	for _, m := range ms {
		rel, err := filepath.Rel(root, m.Path)
		if err != nil {
			t.Fatal(err)
		}
		if !seen[rel] {
			seen[rel] = true
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

func TestGoBackendRespectsIgnoresAndBinary(t *testing.T) {
	root := fixture(t)
	ms, _ := runScan(t, true, Query{Pattern: "needle", Root: root})
	got := relPaths(t, root, ms)
	want := []string{"main.go", filepath.Join("sub", "notes.txt"), filepath.Join("sub", "util.go")}
	if len(got) != len(want) {
		t.Fatalf("files=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("files=%v want %v", got, want)
		}
	}
}

func TestBackendParity(t *testing.T) {
	if rgPath() == "" {
		t.Skip("ripgrep not installed")
	}
	root := fixture(t)
	for _, q := range []Query{
		{Pattern: "needle", Root: root},
		{Pattern: "needle", Root: root, CaseSensitive: true},
		{Pattern: "Needle", Root: root, WholeWord: true, CaseSensitive: true},
		{Pattern: `needle\w*`, Root: root, Regex: true, CaseSensitive: true},
		{Pattern: "needle", Root: root, Include: []string{"*.go"}},
		{Pattern: "needle", Root: root, Exclude: []string{"*.txt"}},
	} {
		rg, _ := runScan(t, false, q)
		gofb, _ := runScan(t, true, q)
		sortMatches(rg)
		sortMatches(gofb)
		if len(rg) != len(gofb) {
			t.Fatalf("query %+v: rg=%d matches, go=%d", q, len(rg), len(gofb))
		}
		for i := range rg {
			if rg[i] != gofb[i] {
				t.Fatalf("query %+v: match %d differs\n rg: %+v\n go: %+v", q, i, rg[i], gofb[i])
			}
		}
	}
}

func TestCaseAndWordFlags(t *testing.T) {
	root := fixture(t)
	all, _ := runScan(t, true, Query{Pattern: "needle", Root: root})
	sensitive, _ := runScan(t, true, Query{Pattern: "needle", Root: root, CaseSensitive: true})
	if len(sensitive) >= len(all) {
		t.Fatalf("case-sensitive must reduce matches: %d vs %d", len(sensitive), len(all))
	}
	word, _ := runScan(t, true, Query{Pattern: "needle", Root: root, WholeWord: true, CaseSensitive: true})
	for _, m := range word {
		if m.EndCol-m.StartCol != len("needle") {
			t.Fatalf("whole-word match has wrong span: %+v", m)
		}
	}
	// needleCount must not appear as a whole-word match.
	if len(word) >= len(sensitive) {
		t.Fatalf("whole-word must exclude needleCount: %d vs %d", len(word), len(sensitive))
	}
}

func TestTruncationSignal(t *testing.T) {
	root := fixture(t)
	ms, done := runScan(t, true, Query{Pattern: "needle", Root: root, MaxResults: 2})
	if !done.Truncated {
		t.Fatal("hitting MaxResults must set Truncated")
	}
	if len(ms) != 2 || done.Total != 2 {
		t.Fatalf("bounded scan returned %d/%d matches, want 2", len(ms), done.Total)
	}
}

func TestNoMatchesIsCleanDone(t *testing.T) {
	root := fixture(t)
	for _, forceGo := range []bool{true, false} {
		if !forceGo && rgPath() == "" {
			continue
		}
		ms, done := runScan(t, forceGo, Query{Pattern: "zzz-not-there", Root: root})
		if len(ms) != 0 || done.Err != nil || done.Truncated {
			t.Fatalf("forceGo=%v: want clean empty done, got %d matches, err=%v", forceGo, len(ms), done.Err)
		}
	}
}

func TestBadRegexReportsError(t *testing.T) {
	root := fixture(t)
	var done DoneMsg
	ch := make(chan DoneMsg, 1)
	s := New(func(msg tea.Msg) {
		if d, ok := msg.(DoneMsg); ok {
			ch <- d
		}
	})
	s.forceGo = true
	s.Scan(Query{Pattern: "(unclosed", Root: root, Regex: true})
	select {
	case done = <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("no done message")
	}
	if done.Err == nil {
		t.Fatal("a bad regex must surface as a scan error")
	}
}

func TestNewScanCancelsPrevious(t *testing.T) {
	// A wide tree makes the first scan slow enough to still be running when
	// the second starts; generation filtering is what consumers rely on.
	root := t.TempDir()
	for i := 0; i < 200; i++ {
		dir := filepath.Join(root, "d", string(rune('a'+i%26)))
		_ = os.MkdirAll(dir, 0o755)
		if err := os.WriteFile(filepath.Join(dir, "f"+string(rune('a'+i%26))+".txt"),
			[]byte("needle\nneedle\nneedle\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	staleAfterDone := false
	var doneGen int
	done := make(chan int, 2)
	s := New(nil)
	s.send = func(msg tea.Msg) {
		mu.Lock()
		defer mu.Unlock()
		switch m := msg.(type) {
		case BatchMsg:
			if doneGen != 0 && m.Gen != doneGen {
				staleAfterDone = true
			}
		case DoneMsg:
			done <- m.Gen
		}
	}
	s.forceGo = true
	s.Scan(Query{Pattern: "needle", Root: root})
	gen2 := s.Scan(Query{Pattern: "needle", Root: root})

	// Wait until gen2 finishes; gen1 may or may not emit a stale Done first.
	deadline := time.After(10 * time.Second)
	for {
		select {
		case g := <-done:
			if g == gen2 {
				mu.Lock()
				doneGen = g
				mu.Unlock()
				// Drain a moment: no further batches from the cancelled scan.
				time.Sleep(50 * time.Millisecond)
				mu.Lock()
				defer mu.Unlock()
				if staleAfterDone {
					t.Fatal("cancelled scan kept streaming after the new scan finished")
				}
				if s.Gen() != gen2 {
					t.Fatalf("service gen %d, want %d", s.Gen(), gen2)
				}
				return
			}
		case <-deadline:
			t.Fatal("second scan did not finish")
		}
	}
}

func TestGitignoreRules(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"keep.txt":            "needle\n",
		"gen/out.txt":         "needle\n",
		"deep/nested/gen.tmp": "needle\n",
		"sub/local.txt":       "needle\n",
		"sub/keepme.txt":      "needle\n",
		"docs/api/gen.md":     "needle\n",
	}
	for rel, content := range files {
		path := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Root rules: a dir, a glob, a **/ pattern; a nested .gitignore scoped rule.
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("gen/\n*.tmp\n**/api/gen.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", ".gitignore"), []byte("local.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ms, _ := runScan(t, true, Query{Pattern: "needle", Root: root})
	got := relPaths(t, root, ms)
	want := []string{"keep.txt", filepath.Join("sub", "keepme.txt")}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("gitignore filtering wrong: got %v want %v", got, want)
	}
}
