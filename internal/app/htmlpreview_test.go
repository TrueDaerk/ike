package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/editor"
	"ike/internal/htmlpreview"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/preview"
)

// htmlpreview_test.go covers the HTML preview pane's app wiring (#2740),
// next to the markdown preview's preview_test.go: open/focus semantics, the
// non-HTML guard, the debounced change seam, source-mapped cursor sync,
// .html.gz buffers, session and named-layout restore, and the tab menu.

// openHTMLFile loads a temp HTML file into the active editor and returns the
// model plus the file path.
func openHTMLFile(t *testing.T, content string) (Model, string) {
	t.Helper()
	m := newSized()
	if m.onboardingOpen() {
		// The first-start dialog would eat the scripted keys of the
		// live-update test.
		m = m.closeOnboarding().(Model)
	}
	path := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPath(path, false)
	return tm.(Model), path
}

// stepHTML is step for a message that may leave HTML previews owing a render
// (#2745): step discards the Cmds, so the off-loop render the settled pass
// dispatched never lands. The helper finishes every owed render in place and
// runs another settled pass — the image reconcile reads the rendered page —
// until no preview owes one (graphics support pushed in by the reconcile
// owes a re-render).
func stepHTML(m Model, msg tea.Msg) Model {
	m = step(m, msg)
	for range 4 {
		flushed := false
		m.contentInstances(func(_ string, _ int, c *pane.Instance) bool {
			if c.Kind() == pane.KindHTMLPreview && c.HTMLPreview().Pending() {
				c.HTMLPreview().Flush()
				flushed = true
			}
			return true
		})
		if !flushed {
			break
		}
		// A RenderedMsg for no pane routes nowhere: a bare settled pass.
		m = step(m, htmlpreview.RenderedMsg{})
	}
	return m
}

// htmlPreviewKeyFor returns the key of the first dedicated HTML preview pane
// bound to path, or "".
func htmlPreviewKeyFor(m Model, path string) string {
	for _, key := range m.activeWS().Panes.Keys() {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindHTMLPreview && inst.HTMLPreview().Path() == path {
			return key
		}
	}
	return ""
}

func countHTMLPreviews(m Model) int {
	n := 0
	m.contentInstances(func(_ string, _ int, c *pane.Instance) bool {
		if c.Kind() == pane.KindHTMLPreview {
			n++
		}
		return true
	})
	return n
}

// paragraphs is an HTML page with one <p> per source line: paragraph i sits
// on source line i+2.
func paragraphs(n int) string {
	var b strings.Builder
	b.WriteString("<html>\n<body>\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "<p>para%03d</p>\n", i)
	}
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

// TestHTMLPreviewOpensSplit: html.preview splits the active editor with a
// rendered preview bound to its buffer and keeps focus on the editor.
func TestHTMLPreviewOpensSplit(t *testing.T) {
	m, path := openHTMLFile(t, "<html><head><title>T</title></head><body><h1>Hello Rendered</h1><p>body <b>text</b></p></body></html>\n")
	editorKey := m.activeWS().Panes.Focused()
	m = stepHTML(m, HTMLPreviewMsg{})
	key := htmlPreviewKeyFor(m, path)
	if key == "" {
		t.Fatal("html.preview should open an HTML preview pane bound to the buffer")
	}
	if m.activeWS().Panes.Focused() != editorKey {
		t.Fatalf("focus should stay on the editor, got %q", m.activeWS().Panes.Focused())
	}
	v := ansi.Strip(m.render())
	if !strings.Contains(v, "HTML PREVIEW") || !strings.Contains(v, "Hello Rendered") || !strings.Contains(v, "body text") {
		t.Fatalf("the workspace should show the titled, rendered preview:\n%s", v)
	}
	if strings.Contains(ansi.Strip(m.activeWS().Panes.Get(key).View()), "<b>") {
		t.Fatal("the preview must render the markup, not show it")
	}
	if !m.previewBound.Load() {
		t.Fatal("an open HTML preview must make the editor emitters send caret moves")
	}
}

// TestHTMLPreviewNeedsHTMLBuffer: a non-HTML buffer opens nothing and toasts.
func TestHTMLPreviewNeedsHTMLBuffer(t *testing.T) {
	m, _ := openMarkdownFile(t, "# not html\n")
	m = stepHTML(m, HTMLPreviewMsg{})
	if countHTMLPreviews(m) != 0 {
		t.Fatal("a markdown buffer must not open an HTML preview")
	}
	if n := lastNotification(t, m); !strings.Contains(n, "HTML preview needs") {
		t.Fatalf("the command must say why it is inert, got %q", n)
	}
}

// TestHTMLPreviewSecondPressFocuses: the second invocation focuses the open
// preview instead of splitting again — the markdown preview's rule.
func TestHTMLPreviewSecondPressFocuses(t *testing.T) {
	m, path := openHTMLFile(t, "<p>once</p>\n")
	m = stepHTML(m, HTMLPreviewMsg{})
	first := htmlPreviewKeyFor(m, path)
	m = stepHTML(m, HTMLPreviewMsg{})
	if n := countHTMLPreviews(m); n != 1 {
		t.Fatalf("second invocation must not duplicate the pane, got %d previews", n)
	}
	if m.activeWS().Panes.Focused() != first {
		t.Fatalf("second invocation should focus the existing preview, got %q", m.activeWS().Panes.Focused())
	}
	// A focused preview closes through the ordinary pane-close path.
	m.closeFocused()
	if m.activeWS().Panes.Has(first) {
		t.Fatal("closing the focused preview must remove its pane")
	}
}

// TestHTMLPreviewLiveUpdate: an edit's SyncMsg arms the debounce, and the
// resulting tick re-renders the preview with the new buffer text.
func TestHTMLPreviewLiveUpdate(t *testing.T) {
	m, path := openHTMLFile(t, "<p>draft</p>\n")
	editorKey := m.activeWS().Panes.Focused()
	m = stepHTML(m, HTMLPreviewMsg{})
	for _, k := range []tea.KeyPressMsg{
		{Code: 'o', Text: "o"},
		{Code: '<', Text: "<"}, {Code: 'p', Text: "p"}, {Code: '>', Text: ">"},
		{Code: 'U', Text: "U"}, {Code: 'n', Text: "n"}, {Code: 'i', Text: "i"}, {Code: 'q', Text: "q"},
		{Code: tea.KeyEscape},
	} {
		m = drainKey(m, k)
	}
	tm, cmd := m.Update(editor.SyncMsg{Path: path, FromKey: editorKey})
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("a change to a previewed buffer must arm the debounce tick")
	}
	key := htmlPreviewKeyFor(m, path)
	m = drainCmd(m, cmd)
	v := ansi.Strip(m.activeWS().Panes.Get(key).View())
	if !strings.Contains(v, "Uniq") || strings.Contains(v, "<p>") {
		t.Fatalf("preview should re-render the edited text, got:\n%s", v)
	}
}

// TestHTMLPreviewStaleTickDropped: a tick for an older sequence renders
// nothing through the root model's route either.
func TestHTMLPreviewStaleTickDropped(t *testing.T) {
	m, path := openHTMLFile(t, "<p>first</p>\n")
	m = stepHTML(m, HTMLPreviewMsg{})
	key := htmlPreviewKeyFor(m, path)
	pv := m.activeWS().Panes.Get(key).HTMLPreview()
	pv.SetSource("<p>second</p>")
	m = step(m, htmlpreview.RenderTickMsg{Key: key, Seq: 1})
	if v := ansi.Strip(m.activeWS().Panes.Get(key).View()); !strings.Contains(v, "first") {
		t.Fatalf("a stale tick must not render:\n%s", v)
	}
}

// TestHTMLPreviewCursorSync: the caret's source line scrolls the preview to
// the paragraph on that line — line-accurate through the source map.
func TestHTMLPreviewCursorSync(t *testing.T) {
	m, path := openHTMLFile(t, paragraphs(120))
	m = stepHTML(m, HTMLPreviewMsg{})
	key := htmlPreviewKeyFor(m, path)
	for _, para := range []int{90, 40, 117} {
		m = step(m, preview.CursorMsg{Path: path, Line: para + 2})
		v := ansi.Strip(m.activeWS().Panes.Get(key).View())
		want := fmt.Sprintf("para%03d", para)
		if !strings.Contains(v, want) {
			t.Fatalf("caret on %s: the preview does not show it:\n%s", want, v)
		}
		if para == 40 && (strings.Contains(v, "para000") || strings.Contains(v, "para090")) {
			t.Fatalf("caret on para040 must scroll to it, not show the top or the old spot:\n%s", v)
		}
	}
}

// TestHTMLPreviewGzBuffer: an .html.gz opens through the gz viewer as its
// decompressed page and previews like a plain .html.
func TestHTMLPreviewGzBuffer(t *testing.T) {
	p := writeGzFile(t, "site.html.gz", []byte("<html><body><h1>Compressed Page</h1><p>inside the gz</p></body></html>\n"))
	m := newSized()
	m = step(m, OpenGzipMsg{Path: p})
	vpath := archiveEntryPath(p, "site.html")
	if countTabsForPath(m, vpath) != 1 {
		t.Fatalf("setup: the gz viewer must open %s", vpath)
	}
	m = stepHTML(m, HTMLPreviewMsg{})
	key := htmlPreviewKeyFor(m, vpath)
	if key == "" {
		t.Fatal("html.preview must accept the decompressed .html.gz buffer")
	}
	v := ansi.Strip(m.activeWS().Panes.Get(key).View())
	if !strings.Contains(v, "Compressed Page") || !strings.Contains(v, "inside the gz") {
		t.Fatalf("the preview must render the decompressed page:\n%s", v)
	}
	// Restore re-reads the page through the same decompression.
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	m2 := stepHTML(New(), tea.WindowSizeMsg{Width: 100, Height: 30})
	rk := htmlPreviewKeyFor(m2, vpath)
	if rk == "" {
		t.Fatal("layout restore should rebuild the gz page's preview")
	}
	if v := ansi.Strip(m2.activeWS().Panes.Get(rk).View()); !strings.Contains(v, "Compressed Page") {
		t.Fatalf("the restored preview must decompress the page:\n%s", v)
	}
}

// TestHTMLPreviewPersistsAndRestores: a saved preview leaf restores as a
// preview of the same file, re-read from disk (session restore).
func TestHTMLPreviewPersistsAndRestores(t *testing.T) {
	m, path := openHTMLFile(t, "<h2>Persisted Page</h2>\n")
	m = stepHTML(m, HTMLPreviewMsg{})
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	m2 := stepHTML(New(), tea.WindowSizeMsg{Width: 100, Height: 30})
	key := htmlPreviewKeyFor(m2, path)
	if key == "" {
		t.Fatal("layout restore should rebuild the HTML preview pane")
	}
	if v := ansi.Strip(m2.activeWS().Panes.Get(key).View()); !strings.Contains(v, "Persisted Page") {
		t.Fatalf("restored preview should render the file from disk, got:\n%s", v)
	}
	// A second preview mints past the restored key.
	if next := m2.activeWS().Panes.AddHTMLPreview(path); next == key {
		t.Fatalf("a new preview reused the restored key %q", key)
	}
}

// TestHTMLPreviewTabPersistsAndRestores: a preview merged into the editor's
// tab strip survives the session as a content tab.
func TestHTMLPreviewTabPersistsAndRestores(t *testing.T) {
	m, path := openHTMLFile(t, "<p>Tabbed Page</p>\n")
	edKey := m.activeWS().Panes.Focused()
	m = stepHTML(m, HTMLPreviewMsg{})
	pvKey := htmlPreviewKeyFor(m, path)
	// A whole-pane center drop: the viewer becomes a tab host, whose tabs
	// then move into the editor (#1778).
	if !m.activeWS().Panes.Get(pvKey).ConvertToTabHost() {
		t.Fatal("setup: the HTML preview must convert into a tab host")
	}
	m.mergePaneTabs(pvKey, edKey)
	inst := m.activeWS().Panes.Get(edKey)
	if inst == nil || inst.TabCount() != 2 || inst.TabContent(1) == nil || inst.TabContent(1).Kind() != pane.KindHTMLPreview {
		t.Fatal("setup: the preview must merge as a content tab")
	}
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	m2 := stepHTML(New(), tea.WindowSizeMsg{Width: 100, Height: 30})
	found := false
	m2.contentInstances(func(_ string, _ int, c *pane.Instance) bool {
		if c.Kind() == pane.KindHTMLPreview && c.HTMLPreview().Path() == path {
			found = strings.Contains(ansi.Strip(c.View()), "Tabbed Page")
			return false
		}
		return true
	})
	if !found {
		t.Fatal("the preview tab must restore bound to its file, rendered")
	}
}

// TestHTMLPreviewNamedLayout: a saved named layout takes the preview as an
// anonymous content slot, and applying it keeps the live preview pane.
func TestHTMLPreviewNamedLayout(t *testing.T) {
	m, path := openHTMLFile(t, "<p>layout</p>\n")
	m = stepHTML(m, HTMLPreviewMsg{})
	key := htmlPreviewKeyFor(m, path)
	snap, ok := snapshotLayout(m.activeWS().Tree, m.activeWS().Panes)
	if !ok {
		t.Fatal("a layout holding an HTML preview must snapshot")
	}
	editorSlots := 0
	for _, id := range snap.Panes {
		if id.Kind == "editor" {
			editorSlots++
		}
	}
	if editorSlots != 2 {
		t.Fatalf("editor + preview must snapshot as two content slots, got %d: %+v", editorSlots, snap.Panes)
	}
	tree, leaves, ok := layout.DecodeTree(snap.Tree)
	if !ok {
		t.Fatal("snapshot tree must decode")
	}
	if !m.applySnapshot(tree, mergeIdentities(leaves, snap.Panes)) {
		t.Fatal("applying the snapshot failed")
	}
	if !m.activeWS().Panes.Has(key) || !slices.Contains(layout.Leaves(m.activeWS().Tree), key) {
		t.Fatal("the live HTML preview must survive the layout apply")
	}
}

// TestHTMLPreviewTabMenuEntry: the tab context menu offers html.preview on an
// HTML tab only.
func TestHTMLPreviewTabMenuEntry(t *testing.T) {
	has := func(path string) bool {
		for _, it := range withHTMLPreviewItem(tabContextItems(false), path) {
			if it.Command == "html.preview" {
				return true
			}
		}
		return false
	}
	if !has("/a/index.html") || !has("/a/site.html.gz!site.html") {
		t.Fatal("an HTML tab's menu must offer HTML Preview")
	}
	if has("/a/readme.md") {
		t.Fatal("a non-HTML tab's menu must not offer HTML Preview")
	}
}
