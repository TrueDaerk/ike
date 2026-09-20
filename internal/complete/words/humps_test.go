package words

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ike/internal/complete"
	"ike/internal/config"
)

// humps_test.go guards #2650: the source-side pre-filter is the hump matcher
// the popup uses, so "gur" reaches GotoURLResolver from the project index and
// mid-word hits ("my" → summary) never leave the source.

func TestProjectWordsHumpMatched(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("func GotoURLResolver() {}\nvar summary, mycelium int\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir)
	for start := time.Now(); !s.ScanDone(); {
		if time.Since(start) > 5*time.Second {
			t.Fatal("scan did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	s.Observe(change("/a.go", "gur\nmy"))
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
