package finder

import (
	"path/filepath"
	"testing"

	"ike/internal/histories"

	"ike/internal/search"
)

// history_persist_test.go covers the persistent findInPath history bucket
// (#1171): commits push into the injected store and a fresh finder seeds its
// recall list from it. It also covers the persisted last-search state
// (#2054): closing the overlay saves query/toggles/globs/cursor, and a fresh
// finder over the same store resumes them on its first Open.

// openedWith builds an open finder wired to a history store at file.
func openedWith(t *testing.T, file string) *Model {
	t.Helper()
	m := New(search.New(nil))
	m.SetSize(100, 30)
	m.SetHistories(histories.NewAt(file))
	m.Open(t.TempDir())
	return m
}

// TestHistoryPersistsAcrossFinders: a committed query lands in the store and
// a fresh finder over the same file recalls it with up.
func TestHistoryPersistsAcrossFinders(t *testing.T) {
	file := filepath.Join(t.TempDir(), "histories.json")
	m := openedWith(t, file)
	typeText(m, "needle")
	feed(m, match("a.go", 1))
	m.Update(key("enter")) // opens the match, commits the query and closes

	if got := histories.NewAt(file).All(histories.FindInPath); len(got) != 1 || got[0] != "needle" {
		t.Fatalf("stored bucket = %v, want [needle]", got)
	}

	fresh := openedWith(t, file)
	// The last search state resumes on the first Open of a fresh session
	// (#2054), so the query is already there, not just recallable via up.
	if fresh.query.Text != "needle" {
		t.Fatalf("fresh finder query = %q, want needle", fresh.query.Text)
	}
	fresh.Update(key("up")) // empty list → history recall
	if fresh.query.Text != "needle" {
		t.Fatalf("up in a fresh finder = %q, want needle", fresh.query.Text)
	}
}

// TestFindStatePersistsAcrossFinders: closing the overlay saves the full
// search state (query, toggles, globs, result cursor, #2054); a fresh finder
// over the same store resumes it on its first Open, with the cursor landing
// back on the same match once the reopened scan's results are in.
func TestFindStatePersistsAcrossFinders(t *testing.T) {
	file := filepath.Join(t.TempDir(), "histories.json")
	m := openedWith(t, file)
	typeText(m, "needle")
	m.include.Set("*.go")
	m.exclude.Set("vendor")
	m.caseSensitive, m.wholeWord, m.regex = true, true, false
	feed(m, match("a.go", 1), match("b.go", 2))
	m.list.SetCursor(1) // select the second match
	m.Close()

	fresh := openedWith(t, file)
	if fresh.query.Text != "needle" || fresh.include.Text != "*.go" || fresh.exclude.Text != "vendor" {
		t.Fatalf("fresh finder state = %q/%q/%q, want needle/*.go/vendor", fresh.query.Text, fresh.include.Text, fresh.exclude.Text)
	}
	if !fresh.caseSensitive || !fresh.wholeWord || fresh.regex {
		t.Fatalf("fresh finder toggles = %v/%v/%v, want true/true/false", fresh.caseSensitive, fresh.wholeWord, fresh.regex)
	}
	feed(fresh, match("a.go", 1), match("b.go", 2))
	if got, ok := fresh.list.Current(); !ok || got.Path != "b.go" {
		t.Fatalf("fresh finder cursor landed on %+v, want b.go", got)
	}
}

// TestHistoryStoreDedupeAndOrder: re-committing an old query moves it to the
// front of the persisted bucket, mirroring the in-memory list.
func TestHistoryStoreDedupeAndOrder(t *testing.T) {
	file := filepath.Join(t.TempDir(), "histories.json")
	m := openedWith(t, file)
	for _, q := range []string{"one", "two", "one"} {
		m.Open(t.TempDir())
		m.query.Clear()
		m.preselect = false // drop the remembered query
		typeText(m, q)
		feed(m, match("a.go", 1))
		m.Update(key("enter"))
	}
	got := histories.NewAt(file).All(histories.FindInPath)
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("stored bucket = %v, want [one two]", got)
	}
}
