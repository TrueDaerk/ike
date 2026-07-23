package explorer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
)

// TestCreateEntryWithPathMakesParents guards #1031: a pathy name in the
// new-file prompt creates the intermediate directories, JetBrains-style.
func TestCreateEntryWithPathMakesParents(t *testing.T) {
	root := t.TempDir()
	m := New(root)
	if cmd := m.createEntry(root, filepath.Join("nested", "deep", "newfile.txt"), false); cmd == nil {
		t.Fatalf("createEntry failed: %v", m.err)
	}
	if _, err := os.Stat(filepath.Join(root, "nested", "deep", "newfile.txt")); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

// TestHiddenToggleKeepsSelection guards #1033: the cursor sticks to the same
// path when dot-entries appear above it.
func TestHiddenToggleKeepsSelection(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{".hidden", "a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := New(root)
	m.applyScan(scanCmd(root)().(ScanDoneMsg))
	// Select a.txt (row 1: root row 0, then files; hidden off by default).
	for i, n := range m.rows {
		if n.name == "a.txt" {
			m.cursor = i
		}
	}
	sel := m.rows[m.cursor].path
	m, _ = m.Update(ToggleHiddenMsg{})
	if got := m.rows[m.cursor].path; got != sel {
		t.Fatalf("selection moved to %q, want %q", got, sel)
	}
	m, _ = m.Update(ToggleHiddenMsg{})
	if got := m.rows[m.cursor].path; got != sel {
		t.Fatalf("selection moved back-toggle to %q, want %q", got, sel)
	}
}

// TestSortModes guards #1037: explorer.sort orders a level by name, type or
// modified — dirs always first, live config change re-sorts.
func TestSortModes(t *testing.T) {
	m := New(".")
	now := time.Now()
	mk := func(name string, dir bool, age time.Duration) scanEntry {
		return scanEntry{name: name, isDir: dir, mod: now.Add(-age)}
	}
	entries := []scanEntry{
		mk("b.txt", false, 2*time.Hour),
		mk("a.go", false, 1*time.Hour),
		mk("c.go", false, 3*time.Hour),
		mk("zdir", true, 0),
	}
	names := func() []string {
		out := make([]string, len(m.root.children))
		for i, c := range m.root.children {
			out[i] = c.name
		}
		return out
	}
	eq := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	m.setChildren(m.root, entries)
	if got := names(); !eq(got, []string{"zdir", "a.go", "b.txt", "c.go"}) {
		t.Fatalf("name sort = %v", got)
	}
	m.Configure(host.MapConfig{"explorer.sort": "type"})
	if got := names(); !eq(got, []string{"zdir", "a.go", "c.go", "b.txt"}) {
		t.Fatalf("type sort = %v", got)
	}
	m.Configure(host.MapConfig{"explorer.sort": "modified"})
	if got := names(); !eq(got, []string{"zdir", "a.go", "b.txt", "c.go"}) {
		t.Fatalf("modified sort = %v", got)
	}
	// Unknown value keeps the current ordering rather than breaking.
	m.Configure(host.MapConfig{"explorer.sort": "bogus"})
	if m.sort != "modified" {
		t.Fatalf("sort = %q after bogus value", m.sort)
	}
}

// TestFileOpErrorShowsDialogOverIntactTree guards #1030: a failed op opens a
// dismissable dialog; the tree stays rendered and navigable after dismissal.
func TestFileOpErrorShowsDialogOverIntactTree(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(root)
	m.SetSize(40, 12)
	m.applyScan(scanCmd(root)().(ScanDoneMsg))
	m.createEntry(root, "a.txt", false) // already exists → error dialog
	if !m.Prompting() {
		t.Fatal("failed op must open the error dialog")
	}
	v := m.View()
	if !strings.Contains(v, "a.txt") {
		t.Fatalf("tree must stay rendered under the dialog:\n%s", v)
	}
	if !strings.Contains(v, "already exists") || !strings.Contains(v, "any key to dismiss") {
		t.Fatalf("dialog content missing:\n%s", v)
	}
	// Any key dismisses and clears the error; the tree is plain again.
	m.handlePromptKey(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.Prompting() || m.err != nil {
		t.Fatal("dismiss must close the dialog and clear the error")
	}
	if v := m.View(); strings.Contains(v, "already exists") {
		t.Fatalf("error must be gone after dismiss:\n%s", v)
	}
}

// TestScanErrorBannerKeepsTree guards #1030: a scan error renders as a
// bottom banner over the intact tree, not a full-view replacement.
func TestScanErrorBannerKeepsTree(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(root)
	m.SetSize(40, 8)
	m.applyScan(scanCmd(root)().(ScanDoneMsg))
	m.applyScan(ScanDoneMsg{Path: root, Err: os.ErrPermission})
	v := m.View()
	if !strings.Contains(v, "a.txt") || !strings.Contains(v, "error:") {
		t.Fatalf("want tree + banner:\n%s", v)
	}
	if m.Prompting() {
		t.Fatal("a scan error must not open a modal (poll spam)")
	}
}
