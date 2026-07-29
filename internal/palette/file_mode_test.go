package palette

import "testing"

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
