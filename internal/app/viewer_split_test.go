package app

import (
	"testing"

	"ike/internal/layout"
	"ike/internal/pane"
)

// splitSibling returns the leaves of the subtree sharing a split with key, and
// false when key is not one side of a split (a single-leaf tree).
func splitSibling(root layout.Node, key string) ([]string, bool) {
	var walk func(n layout.Node) ([]string, bool)
	walk = func(n layout.Node) ([]string, bool) {
		s, ok := n.(*layout.Split)
		if !ok {
			return nil, false
		}
		if leaf, ok := s.A.(*layout.Leaf); ok && leaf.Pane == key {
			return layout.Leaves(s.B), true
		}
		if leaf, ok := s.B.(*layout.Leaf); ok && leaf.Pane == key {
			return layout.Leaves(s.A), true
		}
		if got, ok := walk(s.A); ok {
			return got, true
		}
		return walk(s.B)
	}
	return walk(root)
}

// viewerOpens drives the three viewer opens that share viewerSplitTarget
// (#1779), each returning the key of the pane it created.
var viewerOpens = []struct {
	name string
	open func(t *testing.T, m *Model) string
}{
	{"data", func(t *testing.T, m *Model) string {
		m.openDataPane(writeTestDB(t, "app.db"))
		return m.activeWS().Panes.Focused()
	}},
	{"image", func(t *testing.T, m *Model) string {
		m.openImagePreview(writeTestPNG(t))
		return m.activeWS().Panes.Focused()
	}},
	{"archive", func(t *testing.T, m *Model) string {
		m.openArchivePane(writeTestArchive(t, "src.tar", map[string]string{"a.txt": "a"}))
		return m.activeWS().Panes.Focused()
	}},
}

// TestViewerOpenFromExplorerSplitsRecentEditor: opening a viewer while the
// explorer holds the focus splits the editor the user came from, not the
// explorer (#1779).
func TestViewerOpenFromExplorerSplitsRecentEditor(t *testing.T) {
	for _, tc := range viewerOpens {
		t.Run(tc.name, func(t *testing.T) {
			m := newSized()
			editorKey := m.fileEditorKey()
			if editorKey == "" {
				t.Fatal("the default layout must have an editor pane")
			}
			m.setFocus(editorKey)
			m.setFocus(pane.ExplorerKey)

			key := tc.open(t, &m)
			if key == pane.ExplorerKey || key == editorKey {
				t.Fatalf("viewer pane key = %q", key)
			}
			sib, ok := splitSibling(m.activeWS().Tree, key)
			if !ok {
				t.Fatal("viewer pane must be one side of a split")
			}
			if len(sib) != 1 || sib[0] != editorKey {
				t.Fatalf("viewer split off %v, want the editor %q", sib, editorKey)
			}
		})
	}
}

// TestViewerOpenFromEditorSplitsFocusedPane: with an editor focused the target
// stays the focused pane — the pre-#1779 behaviour.
func TestViewerOpenFromEditorSplitsFocusedPane(t *testing.T) {
	for _, tc := range viewerOpens {
		t.Run(tc.name, func(t *testing.T) {
			m := newSized()
			editorKey := m.fileEditorKey()
			m.setFocus(editorKey)

			key := tc.open(t, &m)
			sib, ok := splitSibling(m.activeWS().Tree, key)
			if !ok {
				t.Fatal("viewer pane must be one side of a split")
			}
			if len(sib) != 1 || sib[0] != editorKey {
				t.Fatalf("viewer split off %v, want the focused editor %q", sib, editorKey)
			}
		})
	}
}

// TestViewerOpenWithoutEditorStillOpens: a workspace holding only the explorer
// has no better target left — the pane still opens rather than being dropped.
func TestViewerOpenWithoutEditorStillOpens(t *testing.T) {
	for _, tc := range viewerOpens {
		t.Run(tc.name, func(t *testing.T) {
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

			key := tc.open(t, &m)
			if key == pane.ExplorerKey {
				t.Fatal("the viewer pane must be created and focused")
			}
			if !layout.Panes(m.activeWS().Tree)[key] {
				t.Fatalf("viewer pane %q missing from the tree", key)
			}
		})
	}
}

// TestViewerSplitTargetPrefersContentPanes: a focused terminal or viewer is a
// content pane and keeps the split, while a focused tool window falls back to
// the recent editor.
func TestViewerSplitTargetPrefersContentPanes(t *testing.T) {
	m := newSized()
	editorKey := m.fileEditorKey()
	m.setFocus(editorKey)

	m.openDataPane(writeTestDB(t, "app.db"))
	dataKey := m.activeWS().Panes.Focused()
	if got := m.viewerSplitTarget(); got != dataKey {
		t.Fatalf("focused data pane target = %q, want %q", got, dataKey)
	}

	m.setFocus(pane.ExplorerKey)
	if got := m.viewerSplitTarget(); got != editorKey {
		t.Fatalf("focused explorer target = %q, want the recent editor %q", got, editorKey)
	}
}
