package pane

import (
	"os"
	"path/filepath"
	"testing"
)

// editorInst allocates a fresh editor instance in a fresh registry.
func editorInst(t *testing.T) *Instance {
	t.Helper()
	r := newReg()
	return r.Get(r.AddEditor())
}

// loadTab loads a real temp file into the instance's active tab.
func loadTab(t *testing.T, i *Instance, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := i.Editor().Load(path); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestEditorStartsWithOneTab verifies a fresh editor pane holds exactly one
// scratch tab and the explorer holds none.
func TestEditorStartsWithOneTab(t *testing.T) {
	r := newReg()
	r.AddExplorer()
	i := r.Get(r.AddEditor())
	if i.TabCount() != 1 || i.ActiveTab() != 0 {
		t.Fatalf("fresh editor: tabs=%d active=%d, want 1/0", i.TabCount(), i.ActiveTab())
	}
	if i.Editor() == nil || i.Editor().HasFile() {
		t.Fatal("the initial tab must be an empty scratch buffer")
	}
	if r.Get(ExplorerKey).TabCount() != 0 {
		t.Fatal("the explorer holds no tabs")
	}
}

// TestAddTabAppendsAndActivates checks AddTab grows the ordered list at the end
// and moves the active index onto the new tab.
func TestAddTabAppendsAndActivates(t *testing.T) {
	i := editorInst(t)
	first := i.Editor()
	added := i.AddTab()
	if i.TabCount() != 2 || i.ActiveTab() != 1 {
		t.Fatalf("after AddTab: tabs=%d active=%d, want 2/1", i.TabCount(), i.ActiveTab())
	}
	if i.Editor() != added || i.TabEditor(0) != first {
		t.Fatal("AddTab must append at the end and activate the new tab")
	}
}

// TestActivateTab verifies switching the active tab and bounds checking.
func TestActivateTab(t *testing.T) {
	i := editorInst(t)
	i.AddTab()
	if !i.ActivateTab(0) || i.ActiveTab() != 0 {
		t.Fatal("ActivateTab(0) must switch back to the first tab")
	}
	if i.ActivateTab(-1) || i.ActivateTab(2) {
		t.Fatal("out-of-range indexes must be rejected")
	}
}

// TestTabForPath resolves tabs by document path.
func TestTabForPath(t *testing.T) {
	i := editorInst(t)
	a := loadTab(t, i, "a.txt")
	i.AddTab()
	b := loadTab(t, i, "b.txt")
	if i.TabForPath(a) != 0 || i.TabForPath(b) != 1 {
		t.Fatalf("TabForPath: a=%d b=%d, want 0/1", i.TabForPath(a), i.TabForPath(b))
	}
	if i.TabForPath("missing") != -1 || i.EditorForPath("missing") != nil {
		t.Fatal("unknown paths resolve to -1/nil")
	}
	if i.EditorForPath(a) != i.TabEditor(0) {
		t.Fatal("EditorForPath must return the tab's editor model")
	}
}

// TestCloseTabAdjustsActive covers the active-index rules: a neighbour slides
// in when the active tab closes, the last position falls back left, and
// closing before the active tab shifts it down.
func TestCloseTabAdjustsActive(t *testing.T) {
	i := editorInst(t)
	loadTab(t, i, "a.txt")
	i.AddTab()
	loadTab(t, i, "b.txt")
	i.AddTab()
	c := loadTab(t, i, "c.txt")

	// Close before the active tab: active follows its document.
	if !i.CloseTab(0) || i.TabCount() != 2 || i.Editor().Path() != c {
		t.Fatalf("closing a background tab must keep the active document, active=%d", i.ActiveTab())
	}
	// Close the active tab at the last position: fall back to the left.
	if !i.CloseTab(i.ActiveTab()) || i.TabCount() != 1 || i.ActiveTab() != 0 {
		t.Fatal("closing the last-position active tab must activate its left neighbour")
	}
	// The only tab never closes through CloseTab; the pane closes instead.
	if i.CloseTab(0) {
		t.Fatal("closing the only tab must be refused")
	}
}

// TestCloseTabActivatesRightNeighbour checks the tab sliding into the closed
// position becomes active.
func TestCloseTabActivatesRightNeighbour(t *testing.T) {
	i := editorInst(t)
	loadTab(t, i, "a.txt")
	i.AddTab()
	loadTab(t, i, "b.txt")
	i.AddTab()
	c := loadTab(t, i, "c.txt")
	i.ActivateTab(1)
	if !i.CloseTab(1) || i.Editor().Path() != c || i.ActiveTab() != 1 {
		t.Fatalf("the right neighbour must slide in and take over, active=%d path=%q",
			i.ActiveTab(), i.Editor().Path())
	}
}

// TestMoveTabReorders verifies reordering keeps the same tab active.
func TestMoveTabReorders(t *testing.T) {
	i := editorInst(t)
	a := loadTab(t, i, "a.txt")
	i.AddTab()
	b := loadTab(t, i, "b.txt")
	i.AddTab()
	c := loadTab(t, i, "c.txt")

	if !i.MoveTab(2, 0) {
		t.Fatal("MoveTab(2,0) must succeed")
	}
	want := []string{c, a, b}
	for n, p := range want {
		if i.TabEditor(n).Path() != p {
			t.Fatalf("tab %d = %q, want %q", n, i.TabEditor(n).Path(), p)
		}
	}
	if i.Editor().Path() != c {
		t.Fatal("the moved tab must stay active")
	}
	if i.MoveTab(0, 3) || i.MoveTab(-1, 0) {
		t.Fatal("out-of-range moves must be rejected")
	}
}
