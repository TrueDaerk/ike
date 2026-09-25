package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/config"
	"ike/internal/explorer"
	"ike/internal/layout"
	"ike/internal/pane"
)

// htmlview_test.go covers the HTML tab's two views (#2766): an HTML file
// opens rendered in its own tab, the [Source] [Preview] strip in the pane's
// bottom-left corner and html.view.* switch it, a jump to a source position
// lands in Source, and the view survives session restore and named layouts.

// withHTMLOpenMode sets preview.html_open_mode for the test.
func withHTMLOpenMode(t *testing.T, mode string) {
	t.Helper()
	orig := config.Get()
	t.Cleanup(func() { config.Set(orig) })
	c, _ := config.Load(config.Options{})
	c.Preview.HTMLOpenMode = mode
	config.Set(c)
}

// openHTMLTab opens a temp HTML page the way the explorer does and settles
// the renders it owes. It returns the model, the page path and the editor
// pane's key.
func openHTMLTab(t *testing.T, content string) (Model, string, string) {
	t.Helper()
	return openHTMLTabMode(t, content, "")
}

// openHTMLTabMode is openHTMLTab with preview.html_open_mode set to mode
// ("" keeps the default) once the model has loaded its config.
func openHTMLTabMode(t *testing.T, content, mode string) (Model, string, string) {
	t.Helper()
	m := newSized()
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	if mode != "" {
		withHTMLOpenMode(t, mode)
	}
	path := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m = stepHTML(m, explorer.OpenFileMsg{Path: path})
	key := m.editorWithFile(path)
	if key == "" {
		t.Fatal("the page must open in an editor pane")
	}
	return m, path, key
}

// htmlTab returns the pane at key and its active tab index.
func htmlTab(m Model, key string) (*pane.Instance, int) {
	inst := m.activeWS().Panes.Get(key)
	return inst, inst.ActiveTab()
}

// stripClick clicks the view strip of pane key at strip column col.
func stripClick(t *testing.T, m Model, key string, col int) Model {
	t.Helper()
	r, ok := m.lay.Panes[key]
	if !ok {
		t.Fatalf("pane %s has no rect", key)
	}
	inst := m.activeWS().Panes.Get(key)
	_, h := r.W, paneInterior(r.H, paneChromeH+m.breadcrumbRows(inst))
	x := r.X + paneContentX + col
	y := r.Y + m.contentYOff(key) + h - 1
	return stepHTML(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
}

// TestHTMLOpenLandsInPreview: opening page.html from the explorer shows the
// rendered page in the tab, with [Source] [Preview] in the body's bottom-left
// corner and Preview highlighted.
func TestHTMLOpenLandsInPreview(t *testing.T) {
	m, path, key := openHTMLTab(t, "<h2>Rendered Heading</h2>\n<p>body text</p>\n")
	inst, idx := htmlTab(m, key)
	if inst.TabViewMode(idx) != pane.ViewPreview || inst.ShownTabView(idx) == nil {
		t.Fatal("an opened HTML file must land in Preview view")
	}
	if inst.TabCount() != 1 || inst.TabPath(idx) != path {
		t.Fatal("the preview is the file's own tab, not a second one")
	}
	if inst.ContextID() != "preview" {
		t.Fatalf("context = %q, want preview", inst.ContextID())
	}
	lines := strings.Split(ansi.Strip(inst.View()), "\n")
	if !strings.Contains(strings.Join(lines, "\n"), "## Rendered Heading") {
		t.Fatalf("the tab must show the rendered page:\n%s", strings.Join(lines, "\n"))
	}
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, "[Source] [Preview] [Browser]") {
		t.Fatalf("bottom row = %q, want the view strip", last)
	}
	if strings.Contains(strings.Join(lines, "\n"), "<h2>") {
		t.Fatal("Preview view must not show the source")
	}
	// No split: the rendering lives in the tab.
	if htmlPreviewKeyFor(m, path) != "" {
		t.Fatal("opening an HTML file must not split a preview pane")
	}
}

// TestHTMLOpenModeSource: preview.html_open_mode = source opens the editor.
func TestHTMLOpenModeSource(t *testing.T) {
	m, _, key := openHTMLTabMode(t, "<p>x</p>\n", config.HTMLOpenSource)
	inst, idx := htmlTab(m, key)
	if inst.TabViewMode(idx) != pane.ViewSource {
		t.Fatal("source mode must open in Source view")
	}
	v := ansi.Strip(inst.View())
	if !strings.Contains(v, "<p>x</p>") || !strings.Contains(v, "[Source] [Preview]") {
		t.Fatalf("Source view shows the text and the strip:\n%s", v)
	}
}

// TestHTMLViewStripClicks: [Source] shows the editor with the caret where it
// was, [Preview] returns to the rendered view at its previous scroll.
func TestHTMLViewStripClicks(t *testing.T) {
	m, _, key := openHTMLTab(t, paragraphs(80))
	inst, idx := htmlTab(m, key)
	ed := inst.TabEditor(idx)
	// The caret moves in Source view; the preview follows it on the way back.
	m = stepHTML(m, HTMLViewMsg{})
	ed.SetCursor(5, 2)
	m = stepHTML(m, HTMLViewMsg{Preview: true})
	pv := inst.ShownTabView(idx).HTMLPreview()
	pv.ScrollBy(20)
	top := pv.Top()
	if top == 0 {
		t.Fatal("setup: the preview must scroll")
	}

	m = stripClick(t, m, key, 1) // [Source]
	if inst.TabViewMode(idx) != pane.ViewSource {
		t.Fatal("[Source] must switch to the editor")
	}
	if line, col := ed.CursorPos(); line != 5 || col != 2 {
		t.Fatalf("caret = %d:%d, want 5:2", line, col)
	}
	if !strings.Contains(ansi.Strip(inst.View()), "<p>para") {
		t.Fatal("Source view must show the text")
	}

	m = stripClick(t, m, key, len("[Source] ")+1) // [Preview]
	if inst.TabViewMode(idx) != pane.ViewPreview {
		t.Fatal("[Preview] must switch back")
	}
	if got := inst.ShownTabView(idx).HTMLPreview().Top(); got != top {
		t.Fatalf("preview scroll = %d, want the previous %d", got, top)
	}
	_ = m
}

// TestHTMLViewStripBrowserButton: [Browser] toggles the preview's browser
// mode exactly like b — here with no browser, the same fallback toast.
func TestHTMLViewStripBrowserButton(t *testing.T) {
	m, _, key := openHTMLTab(t, "<p>x</p>\n")
	inst, idx := htmlTab(m, key)
	pv := inst.ShownTabView(idx).HTMLPreview()
	pv.SetBrowser("/nonexistent/chrome", 0)
	r := m.lay.Panes[key]
	h := paneInterior(r.H, paneChromeH+m.breadcrumbRows(inst))
	col := len("[Source] [Preview] ") + 1
	tm, cmd := m.Update(tea.MouseClickMsg{X: r.X + paneContentX + col, Y: r.Y + m.contentYOff(key) + h - 1, Button: tea.MouseLeft})
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("[Browser] must run the toggle")
	}
	for _, msg := range cmdMsgs(cmd) {
		m = stepHTML(m, msg)
	}
	if n := lastNotification(t, m); !strings.Contains(n, "preview.html_browser") {
		t.Fatalf("toast = %q, want the browser fallback notice", n)
	}
}

// TestHTMLViewToggleCommand: html.view.toggle flips the views; source and
// preview pick one; keys in Preview view drive the preview, not the buffer.
func TestHTMLViewToggleCommand(t *testing.T) {
	m, _, key := openHTMLTab(t, paragraphs(80))
	inst, idx := htmlTab(m, key)
	ed := inst.TabEditor(idx)
	before := ed.Text()
	m = stepHTML(m, keyMsg('j'))
	m = stepHTML(m, keyMsg('x'))
	if ed.Text() != before {
		t.Fatal("keys in Preview view must not edit the hidden buffer")
	}
	if inst.ShownTabView(idx).HTMLPreview().Top() != 1 {
		t.Fatal("j in Preview view scrolls the rendered page")
	}
	m = stepHTML(m, HTMLViewMsg{Toggle: true})
	if inst.TabViewMode(idx) != pane.ViewSource || inst.ContextID() != "editor" {
		t.Fatal("the toggle must switch to Source")
	}
	m = stepHTML(m, HTMLViewMsg{Toggle: true})
	if inst.TabViewMode(idx) != pane.ViewPreview {
		t.Fatal("the toggle must switch back to Preview")
	}
	m = stepHTML(m, HTMLViewMsg{})
	if inst.TabViewMode(idx) != pane.ViewSource {
		t.Fatal("html.view.source must show Source")
	}
	m = stepHTML(m, HTMLViewMsg{Preview: true})
	if inst.TabViewMode(idx) != pane.ViewPreview {
		t.Fatal("html.view.preview must show Preview")
	}
}

// TestHTMLViewNeedsHTMLTab: the view commands toast on a non-HTML tab.
func TestHTMLViewNeedsHTMLTab(t *testing.T) {
	m := newSized()
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = stepHTML(m, explorer.OpenFileMsg{Path: path})
	m = stepHTML(m, HTMLViewMsg{Toggle: true})
	if n := lastNotification(t, m); n != htmlViewNeedsHTML {
		t.Fatalf("toast = %q, want %q", n, htmlViewNeedsHTML)
	}
}

// TestHTMLViewCommandsRegistered: the three commands are on the palette,
// gated to HTML buffers.
func TestHTMLViewCommandsRegistered(t *testing.T) {
	m := newSized()
	for _, id := range []string{"html.view.toggle", "html.view.source", "html.view.preview"} {
		c, ok := m.reg.Command(id)
		if !ok {
			t.Fatalf("%s must be registered", id)
		}
		if !slices.Equal(c.Languages, []string{"html"}) {
			t.Fatalf("%s languages = %v, want [html]", id, c.Languages)
		}
	}
}

// TestHTMLJumpLandsInSource: a navigation to a source position opens an HTML
// file in Source with the caret on the target line, and switches a tab that
// already shows the page rendered back to Source.
func TestHTMLJumpLandsInSource(t *testing.T) {
	m := newSized()
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	path := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(path, []byte(paragraphs(20)), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPathAt(path, 7, 0)
	m = tm.(Model)
	key := m.editorWithFile(path)
	inst, idx := htmlTab(m, key)
	if inst.TabViewMode(idx) != pane.ViewSource || inst.TabView(idx) != nil {
		t.Fatal("a jump must open the page in Source, without rendering it")
	}
	if line, _ := inst.TabEditor(idx).CursorPos(); line != 7 {
		t.Fatalf("caret line = %d, want 7", line)
	}
	m = stepHTML(m, HTMLViewMsg{Preview: true})
	if inst.TabViewMode(idx) != pane.ViewPreview {
		t.Fatal("setup: the tab must show Preview")
	}
	tm, _ = m.openPathAt(path, 12, 0)
	m = tm.(Model)
	if inst.TabViewMode(idx) != pane.ViewSource {
		t.Fatal("a jump into a tab showing Preview must switch it to Source")
	}
	if line, _ := inst.TabEditor(idx).CursorPos(); line != 12 {
		t.Fatalf("caret line = %d, want 12", line)
	}
	// A line-less target (a CLI path, a deep link without :line) is a plain
	// open and keeps the tab's view.
	m = stepHTML(m, HTMLViewMsg{Preview: true})
	tm, _ = m.openPathAt(path, -1, -1)
	m = tm.(Model)
	if inst.TabViewMode(idx) != pane.ViewPreview {
		t.Fatal("a line-less open must not force Source")
	}
}

// TestHTMLViewResyncsOnShow: an edit made in Source view shows when the tab
// switches back to Preview.
func TestHTMLViewResyncsOnShow(t *testing.T) {
	m, _, key := openHTMLTab(t, "<p>before</p>\n")
	inst, idx := htmlTab(m, key)
	m = stepHTML(m, HTMLViewMsg{})
	inst.TabEditor(idx).RestoreText("<p>after edit</p>\n")
	m = stepHTML(m, HTMLViewMsg{Preview: true})
	if v := ansi.Strip(inst.View()); !strings.Contains(v, "after edit") {
		t.Fatalf("Preview must show the edited text:\n%s", v)
	}
}

// TestHTMLPreviewSplitBesideTabPreview: html.preview still splits a preview
// pane beside a tab showing its Preview view, rather than focusing the tab.
func TestHTMLPreviewSplitBesideTabPreview(t *testing.T) {
	m, path, _ := openHTMLTab(t, "<p>x</p>\n")
	m = stepHTML(m, HTMLPreviewMsg{})
	if htmlPreviewKeyFor(m, path) == "" {
		t.Fatal("html.preview must still open its split pane")
	}
}

// TestHTMLViewPersistsAndRestores: session restore brings the tab back in the
// view it was in, the browser mode included.
func TestHTMLViewPersistsAndRestores(t *testing.T) {
	m, path, _ := openHTMLTab(t, "<p>Restored Page</p>\n")
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	m2 := stepHTML(New(), tea.WindowSizeMsg{Width: 100, Height: 30})
	key := m2.editorWithFile(path)
	if key == "" {
		t.Fatal("the tab must restore")
	}
	inst, idx := htmlTab(m2, key)
	if inst.TabViewMode(idx) != pane.ViewPreview || inst.ShownTabView(idx) == nil {
		t.Fatal("the tab must restore in Preview view")
	}
	if v := ansi.Strip(inst.View()); !strings.Contains(v, "Restored Page") || strings.Contains(v, "<p>") {
		t.Fatalf("the restored tab must show the rendered page:\n%s", v)
	}
	// Back to Source: the next restore opens the editor.
	m2 = stepHTML(m2, HTMLViewMsg{})
	m3 := stepHTML(New(), tea.WindowSizeMsg{Width: 100, Height: 30})
	inst3, idx3 := htmlTab(m3, m3.editorWithFile(path))
	if inst3.TabViewMode(idx3) != pane.ViewSource {
		t.Fatal("a tab saved in Source view must restore in Source")
	}
	_ = m2
}

// TestHTMLViewBrowserModePersists: a tab in Preview view whose preview is in
// browser mode restores in browser mode.
func TestHTMLViewBrowserModePersists(t *testing.T) {
	m, path, key := openHTMLTab(t, "<p>x</p>\n")
	inst, idx := htmlTab(m, key)
	inst.ShownTabView(idx).HTMLPreview().SetBrowserMode(true)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	m2 := New()
	inst2 := m2.activeWS().Panes.Get(m2.editorWithFile(path))
	if inst2 == nil {
		t.Fatal("the tab must restore")
	}
	v := inst2.ShownTabView(inst2.ActiveTab())
	if v == nil || !v.HTMLPreview().BrowserMode() {
		t.Fatal("the tab must restore in Preview view with browser mode on")
	}
}

// TestHTMLViewNamedLayout: applying a named layout keeps the tab in the view
// it was in — the live editor pane carries its tabs into the slot.
func TestHTMLViewNamedLayout(t *testing.T) {
	m, path, _ := openHTMLTab(t, "<p>layout</p>\n")
	snap, ok := snapshotLayout(m.activeWS().Tree, m.activeWS().Panes)
	if !ok {
		t.Fatal("the layout must snapshot")
	}
	tree, leaves, ok := layout.DecodeTree(snap.Tree)
	if !ok {
		t.Fatal("snapshot tree must decode")
	}
	if !m.applySnapshot(tree, mergeIdentities(leaves, snap.Panes)) {
		t.Fatal("applying the snapshot failed")
	}
	key := m.editorWithFile(path)
	inst, idx := htmlTab(m, key)
	if inst.TabViewMode(idx) != pane.ViewPreview || inst.ShownTabView(idx) == nil {
		t.Fatal("the tab must keep its Preview view through the layout apply")
	}
}

// TestHTMLViewCloseTabClosesPreview: closing the tab closes both views.
func TestHTMLViewCloseTabClosesPreview(t *testing.T) {
	m, path, key := openHTMLTab(t, "<p>x</p>\n")
	other := filepath.Join(filepath.Dir(path), "notes.txt")
	if err := os.WriteFile(other, []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = stepHTML(m, explorer.OpenFileMsg{Path: other})
	inst := m.activeWS().Panes.Get(key)
	idx := inst.TabForPath(path)
	if inst.TabView(idx) == nil {
		t.Fatal("setup: the HTML tab must hold its preview")
	}
	m.closeTab(inst, idx)
	if countHTMLPreviews(m) != 0 {
		t.Fatal("closing the tab must leave no preview behind")
	}
}
