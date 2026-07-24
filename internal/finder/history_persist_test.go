package finder

import (
	"path/filepath"
	"testing"

	"ike/internal/histories"

	"ike/internal/search"
)

// history_persist_test.go covers the persistent findInPath history bucket
// (#1171): commits push into the injected store and a fresh finder seeds its
// recall list from it.

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
	m.Update(key("enter")) // opens the match, commits the query

	if got := histories.NewAt(file).All(histories.FindInPath); len(got) != 1 || got[0] != "needle" {
		t.Fatalf("stored bucket = %v, want [needle]", got)
	}

	fresh := openedWith(t, file)
	if fresh.query != "" {
		t.Fatalf("fresh finder query = %q, want empty", fresh.query)
	}
	fresh.Update(key("up")) // empty list → history recall
	if fresh.query != "needle" {
		t.Fatalf("up in a fresh finder = %q, want needle", fresh.query)
	}
}

// TestHistoryStoreDedupeAndOrder: re-committing an old query moves it to the
// front of the persisted bucket, mirroring the in-memory list.
func TestHistoryStoreDedupeAndOrder(t *testing.T) {
	file := filepath.Join(t.TempDir(), "histories.json")
	m := openedWith(t, file)
	for _, q := range []string{"one", "two", "one"} {
		m.Open(t.TempDir())
		m.query, m.cur, m.preselect = "", 0, false // drop the remembered query
		typeText(m, q)
		feed(m, match("a.go", 1))
		m.Update(key("enter"))
	}
	got := histories.NewAt(file).All(histories.FindInPath)
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("stored bucket = %v, want [one two]", got)
	}
}
