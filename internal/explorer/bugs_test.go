package explorer

import (
	"fmt"
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

// TestTrashLivesInStateDir guards #1038: trash goes under the state store
// (IKE_CONFIG_DIR here), not a project-root .ike-trash; delete + undo still
// round-trip, and stale trash (incl. the legacy dir) is purged on startup.
func TestTrashLivesInStateDir(t *testing.T) {
	root := t.TempDir()
	cfg := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", cfg)
	target := filepath.Join(root, "doomed.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(root, ".ike-trash")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	m := New(root)
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("legacy .ike-trash must be purged on startup")
	}

	tp, err := m.toTrash(target)
	if err != nil {
		t.Fatal(err)
	}
	if rel, err := filepath.Rel(cfg, tp); err != nil || rel == "" || rel[0] == '.' {
		t.Fatalf("trash path %q must live under IKE_CONFIG_DIR %q", tp, cfg)
	}
	if _, err := os.Stat(filepath.Join(root, ".ike-trash")); !os.IsNotExist(err) {
		t.Fatal("no .ike-trash may appear in the project root")
	}
	// Round-trip: the trashed file moves back.
	if err := os.Rename(tp, target); err != nil {
		t.Fatalf("restore from trash: %v", err)
	}
}

// TestScrollbarThumbDrag guards #1036: a thumb press grabs, drag motion maps
// the pointer back to a proportional scroll offset; track press still jumps.
func TestScrollbarThumbDrag(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 40; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("f%02d.txt", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := New(root)
	m.SetSize(24, 10)
	m.applyScan(scanCmd(root)().(ScanDoneMsg))
	textW, textH, needV, _, _ := m.viewport()
	if !needV {
		t.Fatal("setup: expected vertical overflow")
	}
	if !m.ScrollbarHit(textW, 0) || m.ScrollbarHit(textW-1, 0) {
		t.Fatal("ScrollbarHit must match exactly the bar column")
	}
	// Thumb sits at the top initially: pressing row 0 grabs it.
	if !m.ScrollbarPress(0) {
		t.Fatal("press on the thumb must start a drag")
	}
	m.ScrollbarDrag(textH - 1)
	if m.offset == 0 {
		t.Fatal("dragging to the bottom must scroll down")
	}
	maxOff := len(m.rows) - textH
	if m.offset != maxOff {
		t.Fatalf("offset = %d want max %d", m.offset, maxOff)
	}
	// Track press (top, thumb now at the bottom) jumps without dragging.
	if m.ScrollbarPress(0) {
		t.Fatal("press on the track must not start a drag")
	}
	if m.offset != 0 {
		t.Fatalf("track press must jump, offset = %d", m.offset)
	}
}
