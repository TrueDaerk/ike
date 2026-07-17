package app

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/layout"
)

// open_reuse_test.go guards #641: file open and diff open share one emptiness
// predicate (editor.IsEmpty / Instance.IsEmptyEditor) — a truly empty editor
// is reused in place, a scratch tab with typed text is never clobbered, and
// NewPane does not split past an empty editor.

// TestOpenReusesEmptyEditorTab: opening a file while the active editor is a
// truly empty scratch tab fills that tab in place — no new leaf, no new tab.
func TestOpenReusesEmptyEditorTab(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	os.WriteFile(f, []byte("hello\n"), 0o644)

	m := newSized() // default layout: explorer + one empty editor
	key := m.activeEditorKey()
	if key == "" || !m.panes.Get(key).IsEmptyEditor() {
		t.Fatalf("expected an empty editor pane, got %q", key)
	}
	leaves := len(layout.Leaves(m.tree))

	tm, _ := m.openPath(f, false)
	m = tm.(Model)

	if got := len(layout.Leaves(m.tree)); got != leaves {
		t.Fatalf("open split a new pane: leaves %d -> %d", leaves, got)
	}
	inst := m.panes.Get(key)
	if inst.TabCount() != 1 {
		t.Fatalf("open should fill the empty tab in place, got %d tabs", inst.TabCount())
	}
	if ed := inst.Editor(); ed == nil || ed.Path() != f {
		t.Fatalf("active tab does not hold %s", f)
	}
}

// TestOpenPreservesScratchText: a pathless tab with typed text is not
// reusable — opening a file appends a new tab and the scratch text survives.
func TestOpenPreservesScratchText(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	os.WriteFile(f, []byte("hello\n"), 0o644)

	m := newSized()
	key := m.activeEditorKey()
	inst := m.panes.Get(key)
	inst.Editor().RestoreText("scratch notes") // pathless tab with content
	if inst.IsEmptyEditor() {
		t.Fatal("a scratch tab with text must not count as empty")
	}

	tm, _ := m.openPath(f, false)
	m = tm.(Model)

	if inst.TabCount() != 2 {
		t.Fatalf("open should append a new tab beside the scratch tab, got %d tabs", inst.TabCount())
	}
	if ed := inst.Editor(); ed == nil || ed.Path() != f {
		t.Fatal("the new active tab should hold the opened file")
	}
	if got := inst.TabEditor(0).Text(); got != "scratch notes" {
		t.Fatalf("scratch text was clobbered: %q", got)
	}
}

// TestOpenNewPaneReusesEmptyEditor: NewPane intent (explorer open-in-new-pane)
// reuses an empty active editor instead of splitting past it and stranding a
// blank pane — the file-open twin of TestDiffReusesEmptyEditor (#628).
func TestOpenNewPaneReusesEmptyEditor(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	os.WriteFile(f, []byte("hello\n"), 0o644)

	m := newSized()
	key := m.activeEditorKey()
	if key == "" || !m.panes.Get(key).IsEmptyEditor() {
		t.Fatalf("expected an empty editor pane, got %q", key)
	}
	leaves := len(layout.Leaves(m.tree))

	tm, _ := m.openPath(f, true)
	m = tm.(Model)

	if got := len(layout.Leaves(m.tree)); got != leaves {
		t.Fatalf("NewPane split past the empty editor: leaves %d -> %d", leaves, got)
	}
	if ed := m.panes.Get(key).Editor(); ed == nil || ed.Path() != f {
		t.Fatal("the empty editor should have been reused for the file")
	}
}

// TestOpenNewPaneSplitsBesideFileBackedEditor: NewPane still splits when the
// active editor holds a file.
func TestOpenNewPaneSplitsBesideFileBackedEditor(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	os.WriteFile(a, []byte("a\n"), 0o644)
	os.WriteFile(b, []byte("b\n"), 0o644)

	m := newSized()
	tm, _ := m.openPath(a, false)
	m = tm.(Model)
	leaves := len(layout.Leaves(m.tree))

	tm, _ = m.openPath(b, true)
	m = tm.(Model)

	if got := len(layout.Leaves(m.tree)); got != leaves+1 {
		t.Fatalf("NewPane should split beside a file-backed editor: leaves %d -> %d", leaves, got)
	}
}
