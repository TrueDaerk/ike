package palette

import (
	"path/filepath"
	"testing"
)

// TestFileModeUsageBreaksScoreTies guards #1419: among equal fuzzy scores the
// more-often-chosen file ranks first; a better match still wins over usage.
func TestFileModeUsageBreaksScoreTies(t *testing.T) {
	f := fileMode("ax.go", "bax.go")
	cx := Context{Root: "/proj"}
	u := &Usage{}
	u.Bump(filepath.Join("/proj", "bax.go"))
	f.SetUsage(u)

	// Empty query: every file scores 0, usage decides.
	got := f.Results("", cx)
	if len(got) != 2 || got[0].Title != "bax.go" {
		t.Fatalf("usage tie-break: got %v, want bax.go first", got)
	}

	// "ax" matches ax.go strictly better (start anchor): score beats usage.
	got = f.Results("ax", cx)
	if len(got) != 2 || got[0].Title != "ax.go" {
		t.Fatalf("score must beat usage: got %v, want ax.go first", got)
	}
}

// TestFileModeUsageKeyMatchesEmittedPath guards #1419's key contract: the
// counter is keyed by the same joined path the activated OpenFileMsg carries.
func TestFileModeUsageKeyMatchesEmittedPath(t *testing.T) {
	f := fileMode("a.go", "b.go")
	cx := Context{Root: "/proj"}
	items := f.Results("", cx)
	u := &Usage{}
	u.Bump(items[1].Msg.(OpenFileMsg).Path) // choose b.go
	f.SetUsage(u)
	if got := f.Results("", cx); got[0].Title != "b.go" {
		t.Fatalf("bump via emitted path had no effect: got %v", got)
	}
}

// TestFileModeRefreshDropsCache guards #1372: Refresh invalidates the cached
// walk, so the next Results call re-walks and reflects created and deleted
// files; without Refresh the snapshot stays cached across keystrokes.
func TestFileModeRefreshDropsCache(t *testing.T) {
	walks := 0
	files := []string{"a.go", "gone.go"}
	f := &FileMode{walk: func(string) []string { walks++; return files }}
	cx := Context{Root: "/proj"}

	f.Results("", cx)
	f.Results("a", cx)
	if walks != 1 {
		t.Fatalf("walks = %d, want 1 (cached across keystrokes)", walks)
	}

	files = []string{"a.go", "new.go"} // created new.go, deleted gone.go
	if got := f.Results("new", cx); len(got) != 0 {
		t.Fatalf("stale cache expected before Refresh, got %v", got)
	}

	f.Refresh()
	if got := f.Results("new", cx); len(got) != 1 || got[0].Title != "new.go" {
		t.Fatalf("after Refresh: Results(new) = %v, want new.go", got)
	}
	if got := f.Results("gone", cx); len(got) != 0 {
		t.Fatalf("deleted file still listed after Refresh: %v", got)
	}
	if walks != 2 {
		t.Fatalf("walks = %d, want 2 (one re-walk after Refresh)", walks)
	}
}

// TestPaletteOpenRefreshesFileMode guards #1372 end to end: every palette open
// (plain, locked, anchored) drops the file-mode cache, so a file created after
// the previous open is findable at the next one.
func TestPaletteOpenRefreshesFileMode(t *testing.T) {
	files := []string{"old.go"}
	f := &FileMode{walk: func(string) []string { return files }}
	p := New(Config{DefaultPrefix: '@'}, f)
	cx := Context{Root: "/proj"}

	open := map[string]func(){
		"Open":         func() { p.Open(cx) },
		"OpenLocked":   func() { p.OpenLocked(cx, '@') },
		"OpenAnchored": func() { p.OpenAnchored(cx, '@', 0, 0, 40) },
	}
	for name, reopen := range open {
		files = []string{"old.go"}
		p.Open(cx)
		f.Results("", cx) // cache fills before fresh.go exists
		files = []string{"old.go", "fresh.go"}
		reopen()
		if got := f.Results("fresh", cx); len(got) != 1 || got[0].Title != "fresh.go" {
			t.Fatalf("%s: Results(fresh) = %v, want fresh.go", name, got)
		}
	}
}
