package app

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/preview"
)

// openPreviewIn writes doc.md (plus any extra files) into a fresh temp dir,
// opens it and splits its preview off, returning the model, the preview's
// pane key and the directory.
func openPreviewIn(t *testing.T, doc string, extra map[string]string) (Model, string, string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range extra {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	tm, _ = m.Update(MarkdownPreviewMsg{})
	m = tm.(Model)
	key := previewKeyFor(m, path)
	if key == "" {
		t.Fatal("the preview pane should have opened")
	}
	return m, key, dir
}

// TestPreviewRelativeLinkOpensTarget guards #2180's first criterion: enter on
// a relative link opens that file in the editor.
func TestPreviewRelativeLinkOpensTarget(t *testing.T) {
	m, key, dir := openPreviewIn(t, "# Doc\n\nSee [the notes](notes.md).\n",
		map[string]string{"notes.md": "# Notes\n\nbody\n"})
	target := filepath.Join(dir, "notes.md")

	tm, _ := m.Update(preview.LinkMsg{Key: key, Path: filepath.Join(dir, "doc.md"), Target: "notes.md"})
	m = tm.(Model)
	if m.editorForPath(target) == nil {
		t.Fatal("following a relative link must open the target in an editor")
	}
}

// TestPreviewLinkKeysFollowSelection guards the whole keyboard path: with the
// preview focused, tab selects the first link and enter follows it.
func TestPreviewLinkKeysFollowSelection(t *testing.T) {
	m, key, dir := openPreviewIn(t, "# Doc\n\nSee [the notes](notes.md).\n",
		map[string]string{"notes.md": "# Notes\n"})
	m.setFocus(key)

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if got, ok := m.activeWS().Panes.Get(key).Preview().SelectedTarget(); !ok || got != "notes.md" {
		t.Fatalf("tab should select the document's first link, got %q (%v)", got, ok)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.editorForPath(filepath.Join(dir, "notes.md")) == nil {
		t.Fatal("enter on the selected link must open it")
	}
}

// TestPreviewMissingLinkTargetToasts guards the second half of that criterion:
// a link to a file that is not there says so instead of failing silently.
func TestPreviewMissingLinkTargetToasts(t *testing.T) {
	m, key, dir := openPreviewIn(t, "# Doc\n\n[gone](nope/missing.md)\n", nil)

	tm, _ := m.Update(preview.LinkMsg{Key: key, Path: filepath.Join(dir, "doc.md"), Target: "nope/missing.md"})
	m = tm.(Model)
	if len(m.toasts) == 0 || !strings.Contains(m.toasts[0].text, "link target not found") {
		t.Fatalf("a missing target must toast, got %+v", m.toasts)
	}
	if m.editorForPath(filepath.Join(dir, "nope/missing.md")) != nil {
		t.Fatal("a missing target must not open an editor")
	}
}

// TestPreviewExternalLinkUsesSystemOpener guards the external case: an http
// URL reaches the platform opener, and only on the explicit key — never as a
// side effect of rendering.
func TestPreviewExternalLinkUsesSystemOpener(t *testing.T) {
	var opened string
	orig := browserOpen
	browserOpen = func(u string) error { opened = u; return nil }
	defer func() { browserOpen = orig }()

	m, key, dir := openPreviewIn(t, "# Doc\n\n[up](https://example.com/x)\n", nil)
	if opened != "" {
		t.Fatalf("rendering must not open anything, got %q", opened)
	}
	tm, _ := m.Update(preview.LinkMsg{Key: key, Path: filepath.Join(dir, "doc.md"), Target: "https://example.com/x"})
	m = tm.(Model)
	if opened != "https://example.com/x" {
		t.Fatalf("opened = %q, want the link URL", opened)
	}
}

// TestPreviewLinkCopyGoesToClipboard guards the copy action: the raw
// destination lands on the clipboard whatever it points at.
func TestPreviewLinkCopyGoesToClipboard(t *testing.T) {
	var copied string
	orig := clipboardWrite
	clipboardWrite = func(text string) { copied = text }
	defer func() { clipboardWrite = orig }()

	m, key, dir := openPreviewIn(t, "# Doc\n\n[up](https://example.com/x)\n", nil)
	tm, _ := m.Update(preview.LinkMsg{Key: key, Path: filepath.Join(dir, "doc.md"), Target: "https://example.com/x", Copy: true})
	m = tm.(Model)
	if copied != "https://example.com/x" {
		t.Fatalf("copied = %q, want the link URL", copied)
	}
	if len(m.toasts) == 0 || !strings.Contains(m.toasts[0].text, "copied") {
		t.Fatalf("the copy should be confirmed, got %+v", m.toasts)
	}
}

// TestPreviewAnchorScrollsPreview guards the in-document case: a "#anchor"
// link scrolls the preview itself rather than opening anything.
func TestPreviewAnchorScrollsPreview(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Top\n\nJump to [the end](#the-end).\n\n")
	for i := 0; i < 60; i++ {
		b.WriteString("filler line\n")
	}
	b.WriteString("\n## The End\n\ndone\n")
	m, key, dir := openPreviewIn(t, b.String(), nil)
	pv := m.activeWS().Panes.Get(key).Preview()
	pv.SetCursorLine(0)

	tm, _ := m.Update(preview.LinkMsg{Key: key, Path: filepath.Join(dir, "doc.md"), Target: "#the-end"})
	m = tm.(Model)
	if v := ansi.Strip(m.activeWS().Panes.Get(key).View()); !strings.Contains(v, "The End") {
		t.Fatalf("the anchor should scroll the preview to its heading, got:\n%s", v)
	}
	if len(m.toasts) != 0 {
		t.Fatalf("a resolvable anchor must not toast, got %+v", m.toasts)
	}
}

// TestPreviewUnknownAnchorToasts guards the anchor miss.
func TestPreviewUnknownAnchorToasts(t *testing.T) {
	m, key, dir := openPreviewIn(t, "# Top\n\n[nowhere](#nowhere)\n", nil)
	tm, _ := m.Update(preview.LinkMsg{Key: key, Path: filepath.Join(dir, "doc.md"), Target: "#nowhere"})
	m = tm.(Model)
	if len(m.toasts) == 0 || !strings.Contains(m.toasts[0].text, "no heading for") {
		t.Fatalf("an unknown anchor must toast, got %+v", m.toasts)
	}
}

// TestPreviewLinkWithFragmentJumpsToHeading guards the cross-file anchor: a
// "other.md#section" link opens the file with the cursor on that heading, the
// move that re-syncs any preview of it in turn.
func TestPreviewLinkWithFragmentJumpsToHeading(t *testing.T) {
	other := "# Other\n\nintro\n\n## Deep Section\n\nbody\n"
	m, key, dir := openPreviewIn(t, "# Doc\n\n[deep](other.md#deep-section)\n",
		map[string]string{"other.md": other})

	tm, _ := m.Update(preview.LinkMsg{Key: key, Path: filepath.Join(dir, "doc.md"), Target: "other.md#deep-section"})
	m = tm.(Model)
	ed := m.editorForPath(filepath.Join(dir, "other.md"))
	if ed == nil {
		t.Fatal("the fragment link must still open the file")
	}
	if line, _ := ed.Cursor(); line != 5 {
		t.Fatalf("cursor should land on the heading (1-based line 5), got %d", line)
	}
}

// TestPreviewLinkFromClosedPaneIsInert guards the late message: a LinkMsg
// whose preview has since closed must not panic or toast about an anchor.
func TestPreviewLinkFromClosedPaneIsInert(t *testing.T) {
	m, key, dir := openPreviewIn(t, "# Top\n\n[a](#top)\n", nil)
	m.setFocus(key)
	m.closeFocused()
	tm, _ := m.Update(preview.LinkMsg{Key: key, Path: filepath.Join(dir, "doc.md"), Target: "#top"})
	m = tm.(Model)
	if len(m.toasts) == 0 || !strings.Contains(m.toasts[0].text, "no heading for") {
		t.Fatalf("a link from a closed preview should fall through to the miss toast, got %+v", m.toasts)
	}
}

// TestPreviewInlineImagesReconcile guards the graphics wiring: a preview whose
// buffer references a local image joins the Kitty reconcile pass — the
// capability probe fires for it, and once support is known its placements are
// transmitted and released with the workspace.
func TestPreviewInlineImagesReconcile(t *testing.T) {
	dir := t.TempDir()
	writeInlinePNG(t, filepath.Join(dir, "logo.png"))
	doc := "# Doc\n\n![logo](logo.png)\n"
	path := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	tm, _ = m.Update(MarkdownPreviewMsg{})
	m = tm.(Model)
	key := previewKeyFor(m, path)

	// Support unknown: the reconcile pass that closed the open Update pass
	// probed, because the preview has an image to show, and transmitted
	// nothing yet.
	if !m.gfxQueried {
		t.Fatal("a preview with a local image must trigger the capability probe")
	}
	if len(m.liveImages) != 0 {
		t.Fatalf("nothing may be transmitted before support is known, got %v", m.liveImages)
	}
	// Support confirmed: the placements go out and are tracked.
	ok := true
	m.kittyGfx = &ok
	if cmd := m.imageSyncCmd(); cmd == nil {
		t.Fatal("a supporting terminal must receive the inline transmission")
	}
	pv := m.activeWS().Panes.Get(key).Preview()
	ids := pv.ImageIDs()
	if len(ids) != 1 {
		t.Fatalf("the preview should place one image, got %v", ids)
	}
	if !m.liveImages[ids[0]] {
		t.Fatalf("the placement should be tracked as live, got %v", m.liveImages)
	}
	// Parking the workspace releases it again.
	if cmd := m.releaseWorkspaceImages(m.activeWS()); cmd == nil {
		t.Fatal("parking must release the preview's placements")
	}
	if len(m.liveImages) != 0 {
		t.Fatalf("released placements must leave the live set, got %v", m.liveImages)
	}
	if len(pv.TransmittedIDs()) != 0 {
		t.Fatal("the preview must forget its transmissions so a resume re-sends them")
	}
}

// TestPreviewsMintedGuardsReconcile guards the #2187 early-out: a workspace
// with neither an image pane nor a preview skips the walk entirely.
func TestPreviewsMintedGuardsReconcile(t *testing.T) {
	m := newSized()
	if m.activeWS().Panes.PreviewsMinted() {
		t.Fatal("a fresh workspace has minted no preview")
	}
	if cmd := m.imageSyncCmd(); cmd != nil {
		t.Fatal("the reconcile pass must be skipped without images or previews")
	}
	m2, _, _ := openPreviewIn(t, "# Doc\n\nno images here\n", nil)
	if !m2.activeWS().Panes.PreviewsMinted() {
		t.Fatal("opening a preview mints one")
	}
	if cmd := m2.imageSyncCmd(); cmd != nil {
		t.Fatal("a preview without images still has nothing to reconcile")
	}
}

// writeInlinePNG drops a small solid PNG at path, for a preview to reference.
func writeInlinePNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: 0x20, G: 0x80, B: 0xc0, A: 0xff})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
