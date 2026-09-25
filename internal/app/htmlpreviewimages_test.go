package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// htmlpreviewimages_test.go covers the HTML preview's inline images in the
// Kitty reconcile pass (0530/5, #2743), next to the markdown preview's
// TestPreviewInlineImagesReconcile: probe, transmit, release on park,
// re-send on resume, delete on close.

// openHTMLPreviewWithImage opens dir/page.html referencing a local PNG and its
// HTML preview, returning the model and the preview key.
func openHTMLPreviewWithImage(t *testing.T) (Model, string) {
	t.Helper()
	png := writeTestPNG(t)
	m, path := openHTMLFile(t, "<h1>Doc</h1>\n<p><img src=\"pic.png\" alt=\"pic\"></p>\n")
	// The page and the image share a directory, so the src is relative.
	if err := os.Rename(png, filepath.Join(filepath.Dir(path), "pic.png")); err != nil {
		t.Fatal(err)
	}
	m = step(m, HTMLPreviewMsg{})
	key := htmlPreviewKeyFor(m, path)
	if key == "" {
		t.Fatal("html.preview should open a pane")
	}
	return m, key
}

func TestHTMLPreviewInlineImagesReconcile(t *testing.T) {
	m, key := openHTMLPreviewWithImage(t)
	hv := m.activeWS().Panes.Get(key).HTMLPreview()
	if !hv.ImagesEnabled() {
		t.Fatal("preview.html_images defaults to on")
	}

	// Support unknown: the image fires the capability probe, nothing is
	// transmitted and the page shows the placeholder.
	if !m.gfxQueried {
		t.Fatal("an HTML preview with a local image must trigger the capability probe")
	}
	if len(m.liveImages) != 0 {
		t.Fatalf("nothing may be transmitted before support is known, got %v", m.liveImages)
	}
	if !strings.Contains(strings.Join(hv.Lines(), "\n"), "[pic]") {
		t.Fatal("without known support the image renders as [alt]")
	}

	// Support confirmed: the placement goes out and is tracked.
	ok := true
	m.kittyGfx = &ok
	raw := rawStrings(m.imageSyncCmd())
	ids := hv.ImageIDs()
	if len(ids) != 1 || !strings.Contains(raw, "a=T") {
		t.Fatalf("a supporting terminal must receive the transmission, ids %v, raw %.80q", ids, raw)
	}
	if !m.liveImages[ids[0]] {
		t.Fatalf("the placement should be tracked as live, got %v", m.liveImages)
	}

	// Parking releases it; the resume's reconcile re-sends it.
	if raw := rawStrings(m.releaseWorkspaceImages(m.activeWS())); !strings.Contains(raw, "a=d") || !strings.Contains(raw, "i="+itoa(ids[0])) {
		t.Fatalf("parking must delete the preview's placement, got %q", raw)
	}
	if len(m.liveImages) != 0 || len(hv.TransmittedIDs()) != 0 {
		t.Fatal("released placements must leave the live set and be forgotten")
	}
	if raw := rawStrings(m.imageSyncCmd()); !strings.Contains(raw, "a=T") {
		t.Fatalf("the resume must re-send the placement, got %.80q", raw)
	}

	// Closing the pane deletes the placement.
	m.activeWS().Panes.SetFocused(key)
	m = step(m, ClosePaneMsg{})
	if htmlPreviewKeyFor(m, hv.Path()) != "" {
		t.Fatal("the preview should be closed")
	}
	if m.liveImages[ids[0]] {
		t.Fatal("closing the pane must delete its placement")
	}
}

// TestHTMLPreviewImagesSettingOff: preview.html_images off keeps the
// placeholder and places nothing, even on a supporting terminal.
func TestHTMLPreviewImagesSettingOff(t *testing.T) {
	m, key := openHTMLPreviewWithImage(t)
	ok := true
	m.kittyGfx = &ok
	m.imageSyncCmd()
	inst := m.activeWS().Panes.Get(key)
	hv := inst.HTMLPreview()
	hv.SetImagesEnabled(false)
	for _, id := range hv.ImageIDs() {
		t.Fatalf("disabled images must place nothing, got %d", id)
	}
	raw := rawStrings(m.imageSyncCmd())
	if !strings.Contains(raw, "a=d") || len(m.liveImages) != 0 {
		t.Fatalf("turning images off must delete the resident placement, got %q", raw)
	}
}
