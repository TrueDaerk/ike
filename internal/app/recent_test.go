package app

import (
	"fmt"
	"testing"
)

func TestRecentFilesTouchMovesToFrontAndDedupes(t *testing.T) {
	r := &recentFiles{}
	r.Touch("a.go")
	r.Touch("b.go")
	r.Touch("a.go")
	got := r.List()
	if len(got) != 2 || got[0] != "a.go" || got[1] != "b.go" {
		t.Fatalf("List() = %v, want [a.go b.go]", got)
	}
}

func TestRecentFilesTouchIgnoresEmptyAndCaps(t *testing.T) {
	r := &recentFiles{}
	r.Touch("")
	if len(r.List()) != 0 {
		t.Fatalf("empty path must not be recorded")
	}
	for i := 0; i < maxRecentFiles+10; i++ {
		r.Touch(fmt.Sprintf("f%03d.go", i))
	}
	got := r.List()
	if len(got) != maxRecentFiles {
		t.Fatalf("len = %d, want cap %d", len(got), maxRecentFiles)
	}
	if got[0] != fmt.Sprintf("f%03d.go", maxRecentFiles+9) {
		t.Fatalf("front = %q, want the most recent touch", got[0])
	}
}

func TestRecentFilesSetDedupesAndCaps(t *testing.T) {
	r := &recentFiles{}
	in := []string{"a.go", "", "b.go", "./a.go"}
	for i := 0; i < maxRecentFiles+10; i++ {
		in = append(in, fmt.Sprintf("f%03d.go", i))
	}
	r.Set(in)
	got := r.List()
	if len(got) != maxRecentFiles {
		t.Fatalf("len = %d, want cap %d", len(got), maxRecentFiles)
	}
	if got[0] != "a.go" || got[1] != "b.go" {
		t.Fatalf("front = %v, want [a.go b.go ...] (empty and ./a.go dropped)", got[:2])
	}
}

func TestSessionRoundTripsRecentFiles(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	saveSession(sessionState{RecentFiles: []string{"x.go", "y.go"}})
	s, ok := loadSession()
	if !ok {
		t.Fatalf("loadSession failed")
	}
	if len(s.RecentFiles) != 2 || s.RecentFiles[0] != "x.go" || s.RecentFiles[1] != "y.go" {
		t.Fatalf("RecentFiles = %v, want [x.go y.go]", s.RecentFiles)
	}
}
