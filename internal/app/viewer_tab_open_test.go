package app

import (
	"testing"

	"ike/internal/explorer"
	"ike/internal/layout"
	"ike/internal/palette"
	"ike/internal/pane"
)

// viewer_tab_open_test.go covers #1825: a viewer file picked in the palette
// (the '@' finder and its siblings) opens as a content tab in the focused
// pane, exactly where a plain file would land, while every other open path
// keeps the #1779 split.

// paletteOpen runs one palette file pick end to end: the file handler's
// dispatch and the viewer's background open (#1795) both settle, so the test
// sees the pane arrangement the user ends up with.
func paletteOpen(t *testing.T, m Model, path string) Model {
	t.Helper()
	out, cmd := m.Update(palette.OpenFileMsg{Path: path})
	return settle(t, out.(Model), cmd)
}

// viewerFiles are the three viewer file kinds sharing the open paths, each
// with the pane kind its handler routes to.
var viewerFiles = []struct {
	name string
	kind pane.Kind
	file func(t *testing.T) string
}{
	{"data", pane.KindData, func(t *testing.T) string { return writeTestDB(t, "app.db") }},
	{"image", pane.KindImage, func(t *testing.T) string { return writeTestPNG(t) }},
	{"archive", pane.KindArchive, func(t *testing.T) string {
		return writeTestArchive(t, "src.tar", map[string]string{"a.txt": "a"})
	}},
}

// TestPaletteOpenViewerAsTabInFocusedPane (#1825): picking a database (or any
// other viewer file) in the palette while an editor pane holds the focus nests
// the viewer as a tab in that pane — no new leaf appears.
func TestPaletteOpenViewerAsTabInFocusedPane(t *testing.T) {
	for _, tc := range viewerFiles {
		t.Run(tc.name, func(t *testing.T) {
			m := newSized()
			editorKey := m.fileEditorKey()
			if editorKey == "" {
				t.Fatal("the default layout must have an editor pane")
			}
			m.setFocus(editorKey)
			before := len(m.leafOrder())

			m = paletteOpen(t, m, tc.file(t))

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

// TestPaletteOpenViewerReplacesScratchTab (#1825): the lone empty scratch tab
// of a fresh editor pane gives way to the viewer, like a plain file open
// (#156) — the pane ends up with the viewer as its only tab.
func TestPaletteOpenViewerReplacesScratchTab(t *testing.T) {
	m := newSized()
	editorKey := m.fileEditorKey()
	m.setFocus(editorKey)
	if !m.activeWS().Panes.Get(editorKey).IsEmptyEditor() {
		t.Skip("the default editor pane is not an empty scratch")
	}

	m = paletteOpen(t, m, writeTestDB(t, "app.db"))

	inst := m.activeWS().Panes.Get(editorKey)
	if inst.TabCount() != 1 {
		t.Fatalf("tab count = %d, want the viewer to replace the scratch tab", inst.TabCount())
	}
	if c := inst.TabContent(0); c == nil || c.Kind() != pane.KindData {
		t.Fatal("the only tab must carry the data viewer")
	}
}

// TestPaletteOpenViewerLoadsInTab (#1825/#1795): the tab-nested data viewer
// runs its background open like a dedicated pane — the tables are there.
func TestPaletteOpenViewerLoadsInTab(t *testing.T) {
	m := newSized()
	m.setFocus(m.fileEditorKey())

	m = paletteOpen(t, m, writeTestDB(t, "app.db"))

	c := m.activeWS().Panes.FocusedInstance().ActiveContent()
	if c == nil || c.Kind() != pane.KindData {
		t.Fatal("the palette pick must open a data viewer tab")
	}
	if err := c.Data().Err(); err != nil {
		t.Fatalf("data viewer error = %v", err)
	}
	if c.Data().Tables() == 0 {
		t.Fatal("the tab-nested viewer must list the database tables")
	}
}

// TestPaletteOpenViewerRefocusesExistingPane (#1825): a viewer already showing
// the file is refocused, never duplicated as a tab.
func TestPaletteOpenViewerRefocusesExistingPane(t *testing.T) {
	m := newSized()
	editorKey := m.fileEditorKey()
	m.setFocus(editorKey)
	p := writeTestDB(t, "app.db")
	m = openData(t, m, p) // split off first, the #1779 way
	dataKey := m.activeWS().Panes.Focused()
	m.setFocus(editorKey)
	before := len(m.leafOrder())

	m = paletteOpen(t, m, p)

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

// TestPaletteOpenViewerFromExplorerStillSplits (#1825 vs #1779): with a pane
// that cannot host tabs focused — the explorer — the palette pick falls back
// to the split beside the pane the user worked in.
func TestPaletteOpenViewerFromExplorerStillSplits(t *testing.T) {
	m := newSized()
	editorKey := m.fileEditorKey()
	m.setFocus(editorKey)
	m.setFocus(pane.ExplorerKey)

	m = paletteOpen(t, m, writeTestDB(t, "app.db"))

	key := m.activeWS().Panes.Focused()
	if key == pane.ExplorerKey || key == editorKey {
		t.Fatalf("viewer pane key = %q, want a fresh split", key)
	}
	sib, ok := splitSibling(m.activeWS().Tree, key)
	if !ok || len(sib) != 1 || sib[0] != editorKey {
		t.Fatalf("viewer split off %v (ok=%v), want the editor %q", sib, ok, editorKey)
	}
}

// TestExplorerOpenViewerStillSplits (#1779 unchanged): an open driven from the
// explorer's file tree keeps splitting a viewer pane beside the working pane,
// whatever the palette path does.
func TestExplorerOpenViewerStillSplits(t *testing.T) {
	m := newSized()
	editorKey := m.fileEditorKey()
	m.setFocus(editorKey)
	p := writeTestDB(t, "app.db")

	out, cmd := m.Update(explorer.OpenFileMsg{Path: p})
	m = settle(t, out.(Model), cmd)

	key := m.activeWS().Panes.Focused()
	if key == editorKey {
		t.Fatal("the explorer open must create its own data pane")
	}
	if !layout.Panes(m.activeWS().Tree)[key] {
		t.Fatalf("data pane %q missing from the tree", key)
	}
	if inst := m.activeWS().Panes.Get(editorKey); inst.ActiveContent() != nil {
		t.Fatal("the editor pane must not have gained a viewer tab")
	}
}

// TestPaletteTabRequestDoesNotLeak (#1825): the pending tab request belongs to
// the one palette open that set it — a following explorer open splits.
func TestPaletteTabRequestDoesNotLeak(t *testing.T) {
	m := newSized()
	editorKey := m.fileEditorKey()
	m.setFocus(editorKey)

	// A palette pick on a plain text file: no viewer consumes the request.
	m = paletteOpen(t, m, writeTempFile(t, "notes.txt", "hello\n"))
	if m.viewerTabHost != "" {
		t.Fatalf("viewerTabHost = %q after a plain file open, want it cleared", m.viewerTabHost)
	}

	out, cmd := m.Update(explorer.OpenFileMsg{Path: writeTestDB(t, "app.db")})
	m = settle(t, out.(Model), cmd)
	if inst := m.activeWS().Panes.Get(editorKey); inst.ActiveContent() != nil {
		t.Fatal("the explorer open must split, not reuse a stale tab request")
	}
}
