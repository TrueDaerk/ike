package explorer

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/watch"
)

// dirChanged routes one watcher directory event through Update and pumps the
// resulting re-scan to quiescence.
func dirChanged(m Model, path string) Model {
	m, cmd := m.Update(watch.EventMsg{Kind: watch.DirChanged, Path: path})
	m, _ = pumpScans(m, cmd)
	return m
}

func hasRow(m Model, path string) bool { return rowPaths(m)[path] }

func TestExternalDirChangeRefreshesSubtree(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m, _ = send(m, key("j"), key("l")) // expand sub/
	if !hasRow(m, filepath.Join(root, "sub", "c.txt")) {
		t.Fatalf("test setup: sub/ should be expanded, rows=%v", rowPaths(m))
	}
	mustWrite(t, filepath.Join(root, "sub", "new.txt"), "n")
	m = dirChanged(m, filepath.Join(root, "sub"))
	if !hasRow(m, filepath.Join(root, "sub", "new.txt")) {
		t.Fatalf("external create must appear after the dir event, rows=%v", rowPaths(m))
	}
	if !hasRow(m, filepath.Join(root, "sub", "c.txt")) {
		t.Fatal("refresh must not collapse the expanded subtree")
	}
	if cur := m.current(); cur == nil || cur.path != filepath.Join(root, "sub") {
		t.Fatalf("cursor must stay on its entry, got %v", m.current())
	}
}

func TestExternalDirChangeKeepsCursorWhenRowsAboveVanish(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	bPath := filepath.Join(root, "b.txt")
	for m.current() != nil && m.current().path != bPath {
		m, _ = send(m, key("j"))
	}
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	m = dirChanged(m, root)
	if hasRow(m, filepath.Join(root, "a.txt")) {
		t.Fatal("externally removed entry must disappear")
	}
	if cur := m.current(); cur == nil || cur.path != bPath {
		t.Fatalf("cursor must follow its entry across the refresh, got %v", cur)
	}
}

func TestExternalDirChangeRespectsHiddenFilter(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	mustWrite(t, filepath.Join(root, ".secret"), "s")
	m = dirChanged(m, root)
	if hasRow(m, filepath.Join(root, ".secret")) {
		t.Fatal("hidden entry must stay filtered after an external refresh")
	}
	m, _ = m.Update(ToggleHiddenMsg{})
	if !hasRow(m, filepath.Join(root, ".secret")) {
		t.Fatal("the refreshed children must include the hidden entry once shown")
	}
}

func TestExternalDirChangeIgnoresUnknownAndUnloadedDirs(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	// sub/ is collapsed (never loaded): the event must not scan it.
	if _, cmd := m.Update(watch.EventMsg{Kind: watch.DirChanged, Path: filepath.Join(root, "sub")}); cmd != nil {
		t.Fatal("a never-loaded directory must not re-scan on an external event")
	}
	if _, cmd := m.Update(watch.EventMsg{Kind: watch.DirChanged, Path: "/no/such/dir"}); cmd != nil {
		t.Fatal("an unknown path must be a no-op")
	}
}
