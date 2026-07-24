package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ike/internal/palette"
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
	in := []RecentEntry{{Path: "a.go"}, {Path: ""}, {Path: "b.go"}, {Path: "./a.go"}}
	for i := 0; i < maxRecentFiles+10; i++ {
		in = append(in, RecentEntry{Path: fmt.Sprintf("f%03d.go", i)})
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

// TestRecentFilesRemove (#1113): Remove drops the entry by cleaned path and
// keeps the rest in order; unknown paths are a no-op.
func TestRecentFilesRemove(t *testing.T) {
	r := &recentFiles{}
	r.Touch("a.go")
	r.Touch("b.go")
	r.Touch("c.go")
	r.Remove("./b.go")
	if got := r.List(); len(got) != 2 || got[0] != "c.go" || got[1] != "a.go" {
		t.Fatalf("List() after Remove = %v, want [c.go a.go]", got)
	}
	r.Remove("missing.go")
	if got := r.List(); len(got) != 2 {
		t.Fatalf("removing an unknown path must be a no-op, got %v", got)
	}
}

// TestRecentFilesTouchStampsTime (#1113): every touch records when it happened.
func TestRecentFilesTouchStampsTime(t *testing.T) {
	t0 := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	now := t0
	r := &recentFiles{now: func() time.Time { return now }}
	r.Touch("a.go")
	now = t0.Add(5 * time.Minute)
	r.Touch("b.go")
	got := r.Entries()
	if len(got) != 2 {
		t.Fatalf("entries = %v", got)
	}
	if !got[0].LastOpened.Equal(t0.Add(5*time.Minute)) || !got[1].LastOpened.Equal(t0) {
		t.Fatalf("timestamps = %v / %v, want touch times", got[0].LastOpened, got[1].LastOpened)
	}
	// Re-touching refreshes the stamp.
	now = t0.Add(time.Hour)
	r.Touch("a.go")
	if got := r.Entries(); !got[0].LastOpened.Equal(t0.Add(time.Hour)) {
		t.Fatalf("re-touch must refresh the timestamp, got %v", got[0].LastOpened)
	}
}

func TestSessionRoundTripsRecentFiles(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	ts := time.Date(2026, 7, 24, 9, 30, 0, 0, time.UTC)
	saveSession(sessionState{RecentFiles: recentFileList{
		{Path: "x.go", TS: ts},
		{Path: "y.go"},
	}})
	s, ok := loadSession()
	if !ok {
		t.Fatalf("loadSession failed")
	}
	if len(s.RecentFiles) != 2 || s.RecentFiles[0].Path != "x.go" || s.RecentFiles[1].Path != "y.go" {
		t.Fatalf("RecentFiles = %v, want [x.go y.go]", s.RecentFiles)
	}
	if !s.RecentFiles[0].TS.Equal(ts) {
		t.Fatalf("TS = %v, want %v (timestamps must persist, #1113)", s.RecentFiles[0].TS, ts)
	}
	if !s.RecentFiles[1].TS.IsZero() {
		t.Fatalf("zero TS must round-trip as zero, got %v", s.RecentFiles[1].TS)
	}
}

// TestSessionLoadsLegacyRecentFilesShape (#1113): a pre-timestamp session.json
// stores recent_files as a bare string array; it must still load, with zero
// timestamps.
func TestSessionLoadsLegacyRecentFilesShape(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	legacy := `{"explorer":{"show_hidden":false},"recent_files":["x.go","y.go"]}`
	if err := os.WriteFile(filepath.Join(dir, "session.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	s, ok := loadSession()
	if !ok {
		t.Fatalf("legacy session must load")
	}
	if len(s.RecentFiles) != 2 || s.RecentFiles[0].Path != "x.go" || s.RecentFiles[1].Path != "y.go" {
		t.Fatalf("RecentFiles = %v, want the legacy paths", s.RecentFiles)
	}
	if !s.RecentFiles[0].TS.IsZero() {
		t.Fatalf("legacy entries must migrate with zero timestamps, got %v", s.RecentFiles[0].TS)
	}
}

// TestRemoveRecentFileMsgRemovesAndPersists (#1113): the palette's aux action
// drops the entry from the MRU store and persists the session immediately.
func TestRemoveRecentFileMsgRemovesAndPersists(t *testing.T) {
	m := sized(t, 100, 40)
	m.recent.Touch("a.go")
	m.recent.Touch("b.go")

	out, _ := m.Update(palette.RemoveRecentFileMsg{Path: "a.go"})
	m = out.(Model)
	if got := m.recent.List(); len(got) != 1 || got[0] != "b.go" {
		t.Fatalf("MRU after removal = %v, want [b.go]", got)
	}
	s, ok := loadSession()
	if !ok || len(s.RecentFiles) != 1 || s.RecentFiles[0].Path != "b.go" {
		t.Fatalf("removal must persist immediately, session = %+v", s.RecentFiles)
	}
}

// TestSessionSaveRestoreCycleKeepsMRU (#1112): the regression the bug report
// asks for — a full save/load/Set cycle keeps the MRU across "sessions".
func TestSessionSaveRestoreCycleKeepsMRU(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	r := &recentFiles{}
	r.Touch("a.go")
	r.Touch("b.go")
	saveSession(sessionState{RecentFiles: recentListFromEntries(r.Entries())})

	s, ok := loadSession()
	if !ok {
		t.Fatalf("loadSession failed")
	}
	next := &recentFiles{} // the next launch's fresh store
	next.Set(s.RecentFiles.toEntries())
	got := next.Entries()
	if len(got) != 2 || got[0].Path != "b.go" || got[1].Path != "a.go" {
		t.Fatalf("restored MRU = %v, want [b.go a.go]", got)
	}
	if got[0].LastOpened.IsZero() {
		t.Fatal("restored entries must keep their timestamps")
	}
}
