package app

import (
	"testing"

	"ike/internal/explorer"
	"ike/internal/layout"
	"ike/internal/pane"
)

// explorer_viewer_tab_test.go covers #1851: every file opened from the
// explorer's tree — plain or viewer-handled — lands as a tab in the
// last-focused editor pane; only the explicit "open in split" action (o) still
// splits a pane beside it.

// explorerOpen runs one explorer file open end to end: the file handler's
// dispatch and the viewer's background open (#1795) both settle, so the test
// sees the pane arrangement the user ends up with.
func explorerOpen(t *testing.T, m Model, path string, newPane bool) Model {
	t.Helper()
	out, cmd := m.Update(explorer.OpenFileMsg{Path: path, NewPane: newPane})
	return settle(t, out.(Model), cmd)
}

// TestExplorerOpenViewerAsTabInRecentEditor (#1851): opening a viewer file from
// the explorer nests the viewer as a tab in the editor the user last worked in
// — no new leaf appears.
func TestExplorerOpenViewerAsTabInRecentEditor(t *testing.T) {
	for _, tc := range viewerFiles {
		t.Run(tc.name, func(t *testing.T) {
			m := newSized()
			editorKey := m.fileEditorKey()
			if editorKey == "" {
				t.Fatal("the default layout must have an editor pane")
			}
			m.setFocus(editorKey)
			m.setFocus(pane.ExplorerKey) // the explorer holds focus while opening
			before := len(m.leafOrder())

			m = explorerOpen(t, m, tc.file(t), false)

			if got := len(m.leafOrder()); got != before {
				t.Fatalf("leaf count = %d, want %d — the viewer must not split", got, before)
			}
			if got := m.activeWS().Panes.Focused(); got != editorKey {
				t.Fatalf("focused pane = %q, want the editor %q", got, editorKey)
			}
			inst := m.activeWS().Panes.Get(editorKey)
			c := inst.ActiveContent()
			if c == nil || c.Kind() != tc.kind {
				t.Fatalf("active tab content = %v, want kind %d", c, tc.kind)
			}
		})
	}
}

// TestExplorerOpenTextFileStaysInEditor (#1851 unchanged): a plain file keeps
// landing as an editor tab in the same pane.
func TestExplorerOpenTextFileStaysInEditor(t *testing.T) {
	m := newSized()
	editorKey := m.fileEditorKey()
	m.setFocus(editorKey)
	m.setFocus(pane.ExplorerKey)
	before := len(m.leafOrder())
	path := writeTempFile(t, "notes.txt", "hello\n")

	m = explorerOpen(t, m, path, false)

	if got := len(m.leafOrder()); got != before {
		t.Fatalf("leaf count = %d, want %d", got, before)
	}
	inst := m.activeWS().Panes.Get(editorKey)
	if inst == nil || inst.TabForPath(canonicalPath(path)) < 0 {
		t.Fatalf("the file must open as a tab in the editor %q", editorKey)
	}
}

// TestExplorerSplitOpenStillSplits (#1851): the explicit "open in split" action
// (explorer o) keeps creating a pane beside the editor, for every file kind.
func TestExplorerSplitOpenStillSplits(t *testing.T) {
	for _, tc := range viewerFiles {
		t.Run(tc.name, func(t *testing.T) {
			m := newSized()
			editorKey := m.fileEditorKey()
			m.setFocus(editorKey)
			before := len(m.leafOrder())

			m = explorerOpen(t, m, tc.file(t), true)

			if got := len(m.leafOrder()); got != before+1 {
				t.Fatalf("leaf count = %d, want %d — the split open must add a leaf", got, before+1)
			}
			key := m.activeWS().Panes.Focused()
			if key == editorKey || !layout.Panes(m.activeWS().Tree)[key] {
				t.Fatalf("split open landed on %q, want a fresh viewer leaf", key)
			}
			if inst := m.activeWS().Panes.Get(key); inst == nil || inst.Kind() != tc.kind {
				t.Fatalf("split pane kind = %v, want %d", inst, tc.kind)
			}
		})
	}
}

// TestExplorerOpenViewerRefocusesExisting (#1851): a viewer already showing the
// file is refocused, never duplicated as a tab.
func TestExplorerOpenViewerRefocusesExisting(t *testing.T) {
	m := newSized()
	editorKey := m.fileEditorKey()
	m.setFocus(editorKey)
	p := writeTestDB(t, "app.db")
	m = openData(t, m, p) // split off first, the #1779 way
	dataKey := m.activeWS().Panes.Focused()
	m.setFocus(pane.ExplorerKey)
	before := len(m.leafOrder())

	m = explorerOpen(t, m, p, false)

	if got := len(m.leafOrder()); got != before {
		t.Fatalf("leaf count = %d, want %d — the open viewer must be reused", got, before)
	}
	if got := m.activeWS().Panes.Focused(); got != dataKey {
		t.Fatalf("focused pane = %q, want the open data pane %q", got, dataKey)
	}
	if inst := m.activeWS().Panes.Get(editorKey); inst.ActiveContent() != nil {
		t.Fatal("the editor pane must not have gained a viewer tab")
	}
}

// TestExplorerOpenViewerWithoutEditorSpawnsOne (#1851): with nothing but the
// explorer left, the open creates the editor pane it needs and puts the viewer
// in it rather than crashing or dropping the file.
func TestExplorerOpenViewerWithoutEditorSpawnsOne(t *testing.T) {
	m := newSized()
	for _, key := range m.leafOrder() {
		if key != pane.ExplorerKey {
			m.closePane(key)
		}
	}
	if leaves := m.leafOrder(); len(leaves) != 1 || leaves[0] != pane.ExplorerKey {
		t.Fatalf("setup leaves = %v", leaves)
	}
	m.setFocus(pane.ExplorerKey)

	m = explorerOpen(t, m, writeTestDB(t, "app.db"), false)

	key := m.activeWS().Panes.Focused()
	if key == pane.ExplorerKey {
		t.Fatal("the viewer must not land on the explorer")
	}
	if !layout.Panes(m.activeWS().Tree)[key] {
		t.Fatalf("pane %q missing from the tree", key)
	}
	inst := m.activeWS().Panes.Get(key)
	c := inst.ActiveContent()
	if c == nil {
		c = inst
	}
	if c.Kind() != pane.KindData {
		t.Fatalf("open pane kind = %d, want the data viewer", c.Kind())
	}
}
