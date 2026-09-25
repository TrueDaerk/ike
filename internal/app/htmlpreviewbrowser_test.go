package app

import (
	"strings"
	"testing"

	"ike/internal/pane"
)

// htmlpreviewbrowser_test.go covers the app wiring of the HTML preview's
// browser screenshot mode (#2746): html.preview.browser's routing and
// fallback toast, and the mode's round trip through the layout state. The
// screenshot pipeline itself is tested in internal/htmlpreview against a
// fake browser.

// TestHTMLPreviewBrowserNeedsPane: without an HTML preview the command only
// says how to get one.
func TestHTMLPreviewBrowserNeedsPane(t *testing.T) {
	m, _ := openHTMLFile(t, "<p>x</p>\n")
	m = stepHTML(m, HTMLPreviewBrowserMsg{})
	if n := lastNotification(t, m); n != htmlPreviewNeedsPane {
		t.Fatalf("toast = %q, want %q", n, htmlPreviewNeedsPane)
	}
}

// TestHTMLPreviewBrowserFallbackToasts: with a browser that does not resolve
// the command keeps text mode and toasts the notice naming the setting — the
// active editor's preview is found even though the editor holds focus.
func TestHTMLPreviewBrowserFallbackToasts(t *testing.T) {
	m, path := openHTMLFile(t, "<p>x</p>\n")
	m = stepHTML(m, HTMLPreviewMsg{})
	pv := m.activeWS().Panes.Get(htmlPreviewKeyFor(m, path)).HTMLPreview()
	pv.SetBrowser("/nonexistent/chrome", 0)
	tm, cmd := m.Update(HTMLPreviewBrowserMsg{})
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("the fallback must deliver a notice")
	}
	for _, msg := range cmdMsgs(cmd) {
		m = stepHTML(m, msg)
	}
	if pv.BrowserMode() {
		t.Fatal("the preview must stay in text mode")
	}
	if n := lastNotification(t, m); !strings.Contains(n, `"/nonexistent/chrome" not found`) || !strings.Contains(n, "preview.html_browser") {
		t.Fatalf("toast = %q", n)
	}
}

// TestHTMLPreviewBrowserModePersists: a preview in browser mode saves
// mode "browser" in the layout state, dedicated or as a content tab, and the
// restore brings the mode back; text mode saves no mode.
func TestHTMLPreviewBrowserModePersists(t *testing.T) {
	m, path := openHTMLFile(t, "<p>x</p>\n")
	m = stepHTML(m, HTMLPreviewMsg{})
	key := htmlPreviewKeyFor(m, path)
	inst := m.activeWS().Panes.Get(key)
	if id, _ := contentIdentity(inst); id.Mode != "" {
		t.Fatalf("text mode must persist no mode, got %q", id.Mode)
	}
	inst.HTMLPreview().SetBrowserMode(true)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	_, ids, ok := loadLayout()
	if !ok || ids[key].Kind != "htmlpreview" || ids[key].Mode != htmlPreviewBrowserMode {
		t.Fatalf("saved identity = %+v (ok %v), want mode browser", ids[key], ok)
	}
	restored := m.activeWS().Panes.NewContentPane(pane.KindHTMLPreview, path, "", "", "")
	m.restoreHTMLPreview(restored, ids[key].Mode)
	if !restored.HTMLPreview().BrowserMode() {
		t.Fatal("the restore must bring browser mode back")
	}
	// A content tab carries the mode in its own identity.
	edKey := m.activeWS().Panes.Focused()
	if !inst.ConvertToTabHost() {
		t.Fatal("setup: the HTML preview must convert into a tab host")
	}
	m.mergePaneTabs(key, edKey)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	_, ids, _ = loadLayout()
	found := false
	for _, ct := range ids[edKey].CTabs {
		if ct.Kind == "htmlpreview" && ct.Mode == htmlPreviewBrowserMode {
			found = true
		}
	}
	if !found {
		t.Fatalf("content tab identities = %+v, want the browser mode kept", ids[edKey].CTabs)
	}
}

// TestHTMLPreviewBrowserModeFlipSavesLayout: a mode flip — here the restore
// seam, b and the fallback take the same path — persists the layout on the
// next settled pass, so the mode survives without a clean quit.
func TestHTMLPreviewBrowserModeFlipSavesLayout(t *testing.T) {
	m, path := openHTMLFile(t, "<p>x</p>\n")
	m = stepHTML(m, HTMLPreviewMsg{})
	key := htmlPreviewKeyFor(m, path)
	pv := m.activeWS().Panes.Get(key).HTMLPreview()
	// A browser that resolves; the screenshot Cmd dispatched is never run.
	pv.SetBrowser("/bin/sh", 0)
	pv.SetBrowserMode(true)
	m.htmlPreviewRenderCmd()
	if _, ids, _ := loadLayout(); ids[key].Mode != htmlPreviewBrowserMode {
		t.Fatalf("saved mode = %q, want browser right after the flip", ids[key].Mode)
	}
	pv.SetBrowserMode(false)
	m.htmlPreviewRenderCmd()
	if _, ids, _ := loadLayout(); ids[key].Mode != "" {
		t.Fatalf("saved mode = %q, want text mode after flipping back", ids[key].Mode)
	}
	// A restore whose browser no longer resolves falls back on the render
	// pass, and the layout records the text mode it fell back to.
	pv.SetBrowser("/nonexistent/chrome", 0)
	pv.SetBrowserMode(true)
	m.htmlPreviewRenderCmd()
	if _, ids, _ := loadLayout(); pv.BrowserMode() || ids[key].Mode != "" {
		t.Fatalf("fallback: mode %v, saved %q; want text mode", pv.BrowserMode(), ids[key].Mode)
	}
}
