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

// TestFileModeFsFallback (#1433, widened by #1775): a non-path query offers
// filesystem prefix candidates anchored at the root, shown by absolute path —
// below the project matches, which keep the top of the list.
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

	// Project matches keep the top of the list; the filesystem row follows
	// (#1775 — before, a single project match hid the fallback entirely).
	f2 := fileMode("zebra-project.go")
	items = f2.Results("zeb", cx)
	if len(items) != 2 || items[0].Title != "zebra-project.go" {
		t.Fatalf("project match must rank first, got %v", items)
	}
	if items[1].Title != filepath.Join(root, "zebra.txt") {
		t.Fatalf("filesystem row must follow the project match, got %v", items)
	}

	// No match anywhere: empty result, no raw-query row in fuzzy mode.
	if items = f.Results("nothing-here", cx); len(items) != 0 {
		t.Fatalf("no-match fallback must stay empty, got %v", items)
	}
}

// TestFileModeHomeFallback guards #1775: a plain fuzzy query also offers
// candidates from the home directory, titled in "~/" notation, so a file like
// ~/Hierarchie.txt is reachable without typing the "~/" prefix by hand. A
// project hit for the same query still ranks above it.
func TestFileModeHomeFallback(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "Hierarchie.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, "Hierarchy-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := fileMode("internal/hier/hierarchy.go")
	f.home = func() string { return home }
	cx := Context{Root: t.TempDir()}

	items := f.Results("Hierar", cx)
	if len(items) != 3 || items[0].Title != "internal/hier/hierarchy.go" {
		t.Fatalf("want project hit first plus two home rows, got %v", items)
	}
	file, dir := items[1], items[2]
	if file.Title != "~"+string(filepath.Separator)+"Hierarchie.txt" {
		t.Fatalf("home file row title = %q, want ~-notation", file.Title)
	}
	if msg, ok := file.Msg.(OpenFileMsg); !ok || msg.Path != filepath.Join(home, "Hierarchie.txt") {
		t.Fatalf("home file row msg = %v, want the absolute path", file.Msg)
	}
	wantDir := "~" + string(filepath.Separator) + "Hierarchy-dir" + string(filepath.Separator)
	if dir.Title != wantDir {
		t.Fatalf("home dir row title = %q, want %q", dir.Title, wantDir)
	}
	if msg, ok := dir.Msg.(OpenPathDescendMsg); !ok || msg.Query != wantDir || msg.Prefix != '@' {
		t.Fatalf("home dir row must descend within '@', msg = %v", dir.Msg)
	}

	// The home anchor is skipped when the query matches nothing there.
	if got := f.Results("no-such-name", cx); len(got) != 0 {
		t.Fatalf("unmatched query must stay empty, got %v", got)
	}
}

// TestFileModeFallbackDedupes guards #1775: a project file is never listed
// twice when the root-anchored fallback finds the same path.
func TestFileModeFallbackDedupes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "dup.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := fileMode("dup.go")
	items := f.Results("dup", Context{Root: root})
	if len(items) != 1 || items[0].Title != "dup.go" {
		t.Fatalf("want the project row only, got %v", items)
	}
}

// TestFileModeFallbackCapped guards #1775: the fallback stays a short tail
// under the project matches instead of flooding the list.
func TestFileModeFallbackCapped(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"c1", "c2", "c3", "c4", "c5", "c6", "c7", "c8", "c9", "c10"} {
		if err := os.WriteFile(filepath.Join(root, "cap-"+n+".txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f := fileMode()
	if items := f.Results("cap-", Context{Root: root}); len(items) != maxFsFallback {
		t.Fatalf("fallback rows = %d, want the %d cap", len(items), maxFsFallback)
	}
}

// TestFileModeCompleteItem guards #1775: tab on a fuzzy query adopts the
// selected candidate — a project hit by its relative path, a directory with
// its trailing separator so the next tab descends — while a path query
// declines and keeps its pathcomplete common-prefix completion.
func TestFileModeCompleteItem(t *testing.T) {
	f := fileMode("internal/app/app.go")
	sel := Item{Title: "internal/app/app.go", Msg: OpenFileMsg{Path: "/proj/internal/app/app.go"}}
	if got, ok := f.CompleteItem("app", sel); !ok || got != "internal/app/app.go" {
		t.Fatalf("CompleteItem(app) = %q,%v want the selected path", got, ok)
	}
	dirSel := Item{Title: "~/dir/", Msg: OpenPathDescendMsg{Query: "~/dir/", Prefix: '@'}}
	if got, ok := f.CompleteItem("dir", dirSel); !ok || got != "~/dir/" {
		t.Fatalf("CompleteItem(dir) = %q,%v want the trailing-separator path", got, ok)
	}
	if _, ok := f.CompleteItem("~/x", dirSel); ok {
		t.Fatal("path queries must decline item completion (pathcomplete owns tab)")
	}
	cmdSel := Item{Title: "Run Something", Msg: RunCommandMsg{ID: "x"}}
	if _, ok := f.CompleteItem("run", cmdSel); ok {
		t.Fatal("a non-file row must not be adopted as a query")
	}
}

// TestFileModeScratchQueryListsScratchFiles guards #1812: typing "scratch" in
// the '@' finder offers the scratch store's files, newest-first like
// ScratchMode, tagged with a Detail chip, and selectable through the usual
// OpenFileMsg.
func TestFileModeScratchQueryListsScratchFiles(t *testing.T) {
	f := fileMode("app.go")
	f.SetScratchList(func() []string { return []string{"/scratch/newest.go", "/scratch/older.txt"} })

	items := f.Results("scratch", Context{Root: "/proj"})
	var got []Item
	for _, it := range items {
		if it.Detail == "scratch" {
			got = append(got, it)
		}
	}
	if len(got) != 2 {
		t.Fatalf("scratch rows = %d, want 2: %+v", len(got), items)
	}
	if got[0].Title != "newest.go" || got[1].Title != "older.txt" {
		t.Fatalf("scratch rows not newest-first: %+v", got)
	}
	if got[0].Msg != (OpenFileMsg{Path: "/scratch/newest.go"}) {
		t.Fatalf("scratch row Msg = %+v, want OpenFileMsg to the scratch path", got[0].Msg)
	}
}

// TestFileModeScratchQueryDoesNotAffectNormalQueries guards #1812's other
// acceptance criterion: a query matching neither "scratch" nor a scratch's own
// name (#2341) is untouched, and an empty query (which would fuzzy-match
// anything) does not pull scratch rows in either.
func TestFileModeScratchQueryDoesNotAffectNormalQueries(t *testing.T) {
	f := fileMode("app.go")
	f.SetScratchList(func() []string { return []string{"/scratch/notes.go"} })

	for _, q := range []string{"", "app", "xyz"} {
		items := f.Results(q, Context{Root: "/proj"})
		for _, it := range items {
			if it.Detail == "scratch" {
				t.Fatalf("query %q must not surface scratch rows, got %+v", q, items)
			}
		}
	}
}

// TestFileModeScratchQueryNilSourceIsSafe guards the nil-safe default: a
// FileMode never wired with SetScratchList (e.g. most existing tests) must
// not panic on a "scratch" query.
func TestFileModeScratchQueryNilSourceIsSafe(t *testing.T) {
	f := fileMode("app.go")
	if items := f.Results("scratch", Context{Root: "/proj"}); len(items) != 0 {
		t.Fatalf("no scratch source: got %+v, want no rows", items)
	}
}

// TestFileModeScratchFoundByOwnName guards #2341: a scratch is findable by its
// own file name in the '@' finder, not only through the word "scratch", and
// activates through the ordinary OpenFileMsg.
func TestFileModeScratchFoundByOwnName(t *testing.T) {
	f := fileMode("app.go")
	f.SetScratchList(func() []string { return []string{"/scratch/notes.go", "/scratch/todo.md"} })

	items := f.Results("notes", Context{Root: "/proj"})
	var got []Item
	for _, it := range items {
		if it.Detail == "scratch" {
			got = append(got, it)
		}
	}
	if len(got) != 1 {
		t.Fatalf("scratch rows = %d, want only the name match: %+v", len(got), items)
	}
	if got[0].Title != "notes.go" {
		t.Fatalf("scratch row title = %q, want notes.go", got[0].Title)
	}
	if got[0].Msg != (OpenFileMsg{Path: "/scratch/notes.go"}) {
		t.Fatalf("scratch row Msg = %+v, want OpenFileMsg to the scratch path", got[0].Msg)
	}
}

// TestFileModeScratchNameMatchesRankBelowProject guards #2341's ranking rule:
// a query matching both a project file and a scratch keeps the project file
// on top — scratch rows are appended below, like the filesystem fallback.
func TestFileModeScratchNameMatchesRankBelowProject(t *testing.T) {
	f := fileMode("notes.go")
	f.SetScratchList(func() []string { return []string{"/scratch/notes.go"} })

	items := f.Results("notes", Context{Root: "/proj"})
	if len(items) < 2 {
		t.Fatalf("items = %+v, want the project file and the scratch", items)
	}
	if items[0].Detail == "scratch" {
		t.Fatalf("scratch row displaced the project match: %+v", items)
	}
	if items[0].Msg != (OpenFileMsg{Path: filepath.Join("/proj", "notes.go")}) {
		t.Fatalf("first row = %+v, want the project notes.go", items[0].Msg)
	}
	if items[1].Detail != "scratch" {
		t.Fatalf("second row = %+v, want the scratch row below the project match", items[1])
	}
}

// TestFileModeScratchNameMatchesRankByScore guards #2341's scratch-internal
// ordering: among scratch rows the better name match leads; the store's
// newest-first order only breaks ties.
func TestFileModeScratchNameMatchesRankByScore(t *testing.T) {
	f := fileMode()
	f.SetScratchList(func() []string { return []string{"/scratch/xnotes.go", "/scratch/notes.go"} })

	items := f.Results("notes", Context{Root: "/proj"})
	if len(items) != 2 {
		t.Fatalf("items = %+v, want both scratch matches", items)
	}
	if items[0].Title != "notes.go" {
		t.Fatalf("scratch order = %q,%q; want the stronger match first", items[0].Title, items[1].Title)
	}
}

// TestFileModeScratchNotListedTwice guards #2341: a scratch that also lies
// under the project root — and would therefore be found again by the
// filesystem fallback (#1775) — is offered exactly once.
func TestFileModeScratchNotListedTwice(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.go")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := fileMode() // empty project walk: the row can only come from scratch or fallback
	f.SetScratchList(func() []string { return []string{path} })

	items := f.Results("notes", Context{Root: root})
	hits := 0
	for _, it := range items {
		if m, ok := it.Msg.(OpenFileMsg); ok && m.Path == path {
			hits++
		}
	}
	if hits != 1 {
		t.Fatalf("path listed %d times, want once: %+v", hits, items)
	}
}

// TestFileModeScratchQueryStillListsWholeStore guards #2341's compatibility
// clause: typing "scratch" keeps listing the entire store newest-first, even
// though no scratch name matches that word.
func TestFileModeScratchQueryStillListsWholeStore(t *testing.T) {
	f := fileMode()
	f.SetScratchList(func() []string { return []string{"/scratch/newest.go", "/scratch/older.txt"} })

	items := f.Results("scratch", Context{Root: "/proj"})
	if len(items) != 2 || items[0].Title != "newest.go" || items[1].Title != "older.txt" {
		t.Fatalf("items = %+v, want the whole store newest-first", items)
	}
}

// TestFileModeScratchPathQueryUnaffected guards #2341: a path query still goes
// to pathcomplete only — scratch rows never leak into it.
func TestFileModeScratchPathQueryUnaffected(t *testing.T) {
	f := fileMode()
	f.SetScratchList(func() []string { return []string{"/scratch/notes.go"} })

	for _, q := range []string{"/notes", "~/notes", "./notes"} {
		for _, it := range f.Results(q, Context{Root: "/proj"}) {
			if it.Detail == "scratch" {
				t.Fatalf("path query %q surfaced a scratch row: %+v", q, it)
			}
		}
	}
}
