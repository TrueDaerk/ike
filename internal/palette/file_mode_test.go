package palette

import (
	"os"
	"path/filepath"
	"strings"
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

// TestFileModeIsPathQuery (#1433): only queries explicitly written as
// filesystem paths bypass the project fuzzy walk.
func TestFileModeIsPathQuery(t *testing.T) {
	for q, want := range map[string]bool{
		"/etc/hos": true, "~/": true, "~": true, "./x": true, "../x": true,
		"": false, "app": false, "src/main.go": false, "a/b": false,
	} {
		if got := isPathQuery(q); got != want {
			t.Errorf("isPathQuery(%q) = %v, want %v", q, got, want)
		}
	}
}

// TestFileModePathQueryListsFilesystem (#1433): an '@' query typed as a path
// serves pathcomplete candidates — files open via OpenFileMsg, directories
// descend back into the '@' mode.
func TestFileModePathQueryListsFilesystem(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := fileMode("project.go")
	items := f.Results(dir+string(filepath.Separator), Context{Root: "/proj"})
	var file, sub bool
	for _, it := range items {
		switch {
		case strings.HasSuffix(it.Title, "note.txt"):
			file = true
			msg, ok := it.Msg.(OpenFileMsg)
			if !ok || msg.Path != filepath.Join(dir, "note.txt") {
				t.Fatalf("file item msg=%v", it.Msg)
			}
		case strings.HasSuffix(it.Title, "sub"+string(filepath.Separator)):
			sub = true
			msg, ok := it.Msg.(OpenPathDescendMsg)
			if !ok || msg.Prefix != '@' {
				t.Fatalf("dir item must descend back into '@', msg=%v", it.Msg)
			}
		}
	}
	if !file || !sub {
		t.Fatalf("want file+dir filesystem candidates, got %v", items)
	}
}

// TestFileModePathQueryTabCompletes (#1433): tab extends a path query through
// the shared engine; a fuzzy query stays inert.
func TestFileModePathQueryTabCompletes(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "unique-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := fileMode("project.go")
	got := f.Complete(filepath.Join(dir, "uni"))
	want := filepath.Join(dir, "unique-dir") + string(filepath.Separator)
	if got != want {
		t.Fatalf("Complete=%q want %q", got, want)
	}
	if got := f.Complete("proj"); got != "proj" {
		t.Fatalf("fuzzy query must stay inert on tab, got %q", got)
	}
}

// TestFileModeFsFallback (#1433): a non-path query with no project match
// falls back to filesystem prefix candidates anchored at the root, shown by
// absolute path; a query with project matches never reaches the fallback.
func TestFileModeFsFallback(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "zebra.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "zoo"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := fileMode("main.go")
	cx := Context{Root: root}

	items := f.Results("zeb", cx)
	if len(items) != 1 || items[0].Title != filepath.Join(root, "zebra.txt") {
		t.Fatalf("fallback = %v, want absolute zebra.txt", items)
	}
	if msg, ok := items[0].Msg.(OpenFileMsg); !ok || msg.Path != filepath.Join(root, "zebra.txt") {
		t.Fatalf("fallback file msg=%v", items[0].Msg)
	}

	items = f.Results("zo", cx)
	wantDir := filepath.Join(root, "zoo") + string(filepath.Separator)
	if len(items) != 1 || items[0].Title != wantDir {
		t.Fatalf("dir fallback = %v, want %q", items, wantDir)
	}
	if msg, ok := items[0].Msg.(OpenPathDescendMsg); !ok || msg.Prefix != '@' || msg.Query != wantDir {
		t.Fatalf("dir fallback msg=%v", items[0].Msg)
	}

	// A project match wins outright: no filesystem rows mixed in.
	f2 := fileMode("zebra-project.go")
	items = f2.Results("zeb", cx)
	if len(items) != 1 || items[0].Title != "zebra-project.go" {
		t.Fatalf("project match must suppress the fallback, got %v", items)
	}

	// No match anywhere: empty result, no raw-query row in fuzzy mode.
	if items = f.Results("nothing-here", cx); len(items) != 0 {
		t.Fatalf("no-match fallback must stay empty, got %v", items)
	}
}
