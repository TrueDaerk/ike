package app

import (
	"testing"

	"ike/internal/layout"
	"ike/internal/pane"
)

// flex_viewer_open_test.go covers #3131: with no text editor live but a
// viewer (hex, image, data, …) holding the flexible region alone, a file
// open lands as a tab in that viewer's pane — never as an editor carved out
// of the explorer's column.

// flexViewerOnly builds the `E | V` layout: the explorer and one viewer
// pane of the given kind, no editor. The viewer splits off the default
// editor (#1779), which is then closed.
func flexViewerOnly(t *testing.T, file string, kind pane.Kind) (Model, string) {
	t.Helper()
	m := newSized()
	editorKey := m.fileEditorKey()
	m.setFocus(editorKey)
	out, cmd := m.openPath(file, false)
	m = settle(t, out.(Model), cmd)
	viewerKey := m.activeWS().Panes.Focused()
	if inst := m.activeWS().Panes.Get(viewerKey); inst == nil || inst.Kind() != kind {
		t.Fatalf("setup: focused pane %q is %v, want kind %d", viewerKey, inst, kind)
	}
	m.closePane(editorKey)
	if leaves := m.leafOrder(); len(leaves) != 2 || leaves[0] != pane.ExplorerKey || leaves[1] != viewerKey {
		t.Fatalf("setup leaves = %v, want [explorer %s]", leaves, viewerKey)
	}
	if m.fileEditorKey() != "" {
		t.Fatal("setup: no pane may edit files")
	}
	m.setFocus(pane.ExplorerKey)
	m.layout()
	return m, viewerKey
}

var flexViewers = []struct {
	name string
	kind pane.Kind
	file func(t *testing.T) string
}{
	{"hex", pane.KindHex, func(t *testing.T) string { return writeTestBinary(t, "blob.bin") }},
	{"image", pane.KindImage, func(t *testing.T) string { return writeTestPNG(t) }},
	{"data", pane.KindData, func(t *testing.T) string { return writeTestDB(t, "app.db") }},
}

// TestExplorerOpenLandsBesideFlexViewer (#3131): explorer Enter on a text
// file opens it as a tab in the flexible region's viewer pane; the leaf
// count and the explorer's width stay as they were.
func TestExplorerOpenLandsBesideFlexViewer(t *testing.T) {
	for _, tc := range flexViewers {
		t.Run(tc.name, func(t *testing.T) {
			m, viewerKey := flexViewerOnly(t, tc.file(t), tc.kind)
			explW := m.lay.Panes[pane.ExplorerKey].W
			before := len(m.leafOrder())
			path := writeTempFile(t, "notes.txt", "hello\n")

			m = explorerOpen(t, m, path, false)

			if got := len(m.leafOrder()); got != before {
				t.Fatalf("leaf count = %d, want %d — the open must not split", got, before)
			}
			inst := m.activeWS().Panes.Get(viewerKey)
			if inst == nil || inst.TabForPath(canonicalPath(path)) < 0 {
				t.Fatalf("the file must open as a tab in the viewer pane %q", viewerKey)
			}
			if got := m.lay.Panes[pane.ExplorerKey].W; got < explW-1 || got > explW+1 {
				t.Fatalf("explorer width = %d, want %d", got, explW)
			}
		})
	}
}

// TestExplorerSplitOpenBesideFlexViewer (#3131): explorer `o` keeps
// splitting, but beside the flexible region's viewer — the explorer column
// keeps its width.
func TestExplorerSplitOpenBesideFlexViewer(t *testing.T) {
	m, viewerKey := flexViewerOnly(t, writeTestBinary(t, "blob.bin"), pane.KindHex)
	explW := m.lay.Panes[pane.ExplorerKey].W
	path := writeTempFile(t, "notes.txt", "hello\n")

	m = explorerOpen(t, m, path, true)

	key := m.editorWithFile(canonicalPath(path))
	if key == "" || key == viewerKey {
		t.Fatalf("the split open must land in a fresh editor, got %q", key)
	}
	if got := len(m.leafOrder()); got != 3 {
		t.Fatalf("leaf count = %d, want 3", got)
	}
	sp := parentSplit(m.activeWS().Tree, key)
	if sp == nil || !layout.Panes(sp)[viewerKey] {
		t.Fatalf("the new editor must split beside the viewer %q", viewerKey)
	}
	if got := m.lay.Panes[pane.ExplorerKey].W; got < explW-1 || got > explW+1 {
		t.Fatalf("explorer width = %d, want %d", got, explW)
	}
}

// TestFlexFileHostSkipsNonViewers (#3131): with only the explorer left there
// is no flexible-region host, so the editor-slot fallback (#3109) decides.
func TestFlexFileHostSkipsNonViewers(t *testing.T) {
	m := newSized()
	for _, key := range m.leafOrder() {
		if key != pane.ExplorerKey {
			m.closePane(key)
		}
	}
	if got := m.flexFileHostKey(); got != "" {
		t.Fatalf("flexFileHostKey = %q with only the explorer, want \"\"", got)
	}
}
