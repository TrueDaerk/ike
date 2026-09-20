package words

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/complete"
	"ike/internal/config"
)

// humps_test.go guards #2650: the source-side pre-filter is the hump matcher
// the popup uses, so "gur" reaches GotoURLResolver from the project index and
// mid-word hits ("my" → summary) never leave the source.

func TestProjectWordsHumpMatched(t *testing.T) {
	requireGrammar(t, "go")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x\n\nfunc GotoURLResolver() {}\nvar summary, mycelium int\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir)
	s.Observe(change("/a.go", "gur\nmy"))
	labels(t, s, complete.Request{Path: "/a.go"}) // starts the Go scan (#2652)
	waitScan(t, s, "go")
	if got := labels(t, s, complete.Request{Path: "/a.go", Line: 0, Col: 3}); len(got) != 1 || got[0] != "GotoURLResolver" {
		t.Fatalf("gur → %v, want [GotoURLResolver]", got)
	}
	if got := labels(t, s, complete.Request{Path: "/a.go", Line: 1, Col: 2}); len(got) != 1 || got[0] != "mycelium" {
		t.Fatalf("my → %v, want [mycelium] (summary is a mid-word hit)", got)
	}
}

// TestWordsCaseSensitivitySetting: the pre-filter reads
// completion.case_sensitivity, so "all" holds the prefix to its exact case.
func TestWordsCaseSensitivitySetting(t *testing.T) {
	prev := config.Get()
	c := *prev
	c.Completion.CaseSensitivity = "all"
	config.Set(&c)
	t.Cleanup(func() { config.Set(prev) })

	s := New("")
	s.Observe(change("/a.go", "Hello hello\nhel"))
	if got := labels(t, s, complete.Request{Path: "/a.go", Line: 1, Col: 3}); len(got) != 1 || got[0] != "hello" {
		t.Fatalf("all: hel → %v, want [hello]", got)
	}
}
