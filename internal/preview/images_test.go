package preview

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi/kitty"

	"ike/internal/theme"
)

// writePNG drops a solid w×h PNG at dir/name and returns its path.
func writePNG(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 0x20, G: 0x80, B: 0xc0, A: 0xff})
		}
	}
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}

// newImaged returns a sized preview bound to dir/doc.md holding src.
func newImaged(dir, src string, gfx bool) Model {
	m := New("preview", filepath.Join(dir, "doc.md"), theme.DefaultPalette())
	m.SetSize(60, 20)
	m.gfx = gfx
	m.SetSourceImmediate(src)
	return m
}

// placeholders counts the Kitty placeholder cells in a rendered view.
func placeholders(s string) int { return strings.Count(s, string(kitty.Placeholder)) }

// TestInlineImageRendersPlaceholders guards the Kitty path: on a supporting
// terminal a local image becomes a block of placeholder cells with a live
// image id, and the reconcile pass gets a transmission for it.
func TestInlineImageRendersPlaceholders(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, dir, "img/logo.png", 40, 20)
	m := newImaged(dir, "# Doc\n\n![the logo](img/logo.png)\n\ntail\n", true)

	if !m.HasImages() {
		t.Fatal("the buffer references a decodable local image")
	}
	ids := m.ImageIDs()
	if len(ids) != 1 {
		t.Fatalf("ImageIDs = %v, want one placement", ids)
	}
	if n := placeholders(strings.Join(m.lines, "\n")); n == 0 {
		t.Fatal("the image line should be replaced by placeholder cells")
	}
	// The "Image: … → path" text line is gone; the pixels replace it.
	if strings.Contains(ansiPlain(m.lines), "img/logo.png") {
		t.Fatal("the inline image should not also print its text form")
	}
	seqs := m.SyncSeqs()
	if len(seqs) != 1 || !strings.Contains(seqs[0], "a=T") {
		t.Fatalf("first show must transmit the image, got %d sequences", len(seqs))
	}
	if got := m.TransmittedIDs(); len(got) != 1 || got[0] != ids[0] {
		t.Fatalf("TransmittedIDs = %v, want %v", got, ids)
	}
	if extra := m.SyncSeqs(); len(extra) != 0 {
		t.Fatalf("an unchanged placement must not retransmit, got %d sequences", len(extra))
	}
}

// TestInlineImageRetransmitsOnResize guards the geometry diff: a narrower
// pane re-places the image, which deletes the old placement and transmits the
// new grid.
func TestInlineImageRetransmitsOnResize(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, dir, "logo.png", 40, 20)
	m := newImaged(dir, "![logo](logo.png)\n", true)
	m.SyncSeqs()
	m.SetSize(24, 20)
	seqs := m.SyncSeqs()
	if len(seqs) != 2 || !strings.Contains(seqs[0], "a=d") || !strings.Contains(seqs[1], "a=T") {
		t.Fatalf("a resize must delete then transmit, got %v", len(seqs))
	}
}

// TestInlineImageFallbackKeepsTextAndAddsMetadata guards the non-Kitty path:
// glamour's text form stays and a metadata caption explains what is not being
// shown; nothing is transmitted.
func TestInlineImageFallbackKeepsTextAndAddsMetadata(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, dir, "logo.png", 40, 20)
	m := newImaged(dir, "![the logo](logo.png)\n", false)

	plain := ansiPlain(m.lines)
	if !strings.Contains(plain, "the logo") {
		t.Fatalf("the alt text must survive, got:\n%s", plain)
	}
	if !strings.Contains(plain, "PNG · 40×20 px") {
		t.Fatalf("the fallback should describe the image, got:\n%s", plain)
	}
	if placeholders(strings.Join(m.lines, "\n")) != 0 {
		t.Fatal("a terminal without Kitty graphics must get no placeholder cells")
	}
	if ids := m.ImageIDs(); len(ids) != 0 {
		t.Fatalf("nothing is placed without graphics support, got %v", ids)
	}
	if seqs := m.SyncSeqs(); len(seqs) != 0 {
		t.Fatalf("the fallback must transmit nothing, got %d sequences", len(seqs))
	}
	// Support arriving later re-renders into the real pixels.
	m.SetGraphics(true)
	if placeholders(strings.Join(m.lines, "\n")) == 0 {
		t.Fatal("SetGraphics(true) should re-render with the image block")
	}
}

// TestRemoteImageIsNeverFetched guards the network boundary: an http image
// stays the text glamour rendered and produces no placement at all.
func TestRemoteImageIsNeverFetched(t *testing.T) {
	m := newImaged(t.TempDir(), "![remote](https://example.invalid/x.png)\n", true)
	if m.HasImages() {
		t.Fatal("a remote image must not become a placement")
	}
	if !strings.Contains(ansiPlain(m.lines), "remote") {
		t.Fatal("a remote image keeps its alt text")
	}
	if placeholders(strings.Join(m.lines, "\n")) != 0 {
		t.Fatal("a remote image must not render pixels")
	}
}

// TestMissingImageTargetStaysText guards the broken-link path: a target that
// does not resolve renders as glamour left it, with no placement and no
// crash.
func TestMissingImageTargetStaysText(t *testing.T) {
	m := newImaged(t.TempDir(), "![gone](nope/missing.png)\n", true)
	if m.HasImages() {
		t.Fatal("an undecodable target must not become a placement")
	}
	if !strings.Contains(ansiPlain(m.lines), "gone") {
		t.Fatal("a missing image keeps its alt text")
	}
}

// TestFencedImageIsNotPlaced guards the fence rule: an image written inside a
// code block is a literal, not a reference.
func TestFencedImageIsNotPlaced(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, dir, "logo.png", 8, 8)
	m := newImaged(dir, "```\n![logo](logo.png)\n```\n", true)
	if m.HasImages() {
		t.Fatal("a fenced image reference must not be placed")
	}
}

// TestUnreferencedImageLosesItsPlacement guards the live-edit path: editing
// the image out of the buffer drops it from the live set, so the app's
// reconcile pass deletes it from the terminal.
func TestUnreferencedImageLosesItsPlacement(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, dir, "logo.png", 8, 8)
	m := newImaged(dir, "![logo](logo.png)\n", true)
	m.SyncSeqs()
	if len(m.TransmittedIDs()) != 1 {
		t.Fatal("the image should be resident before the edit")
	}
	m.SetSourceImmediate("no image any more\n")
	if ids := m.ImageIDs(); len(ids) != 0 {
		t.Fatalf("the dropped image must leave the live set, got %v", ids)
	}
	if ids := m.TransmittedIDs(); len(ids) != 0 {
		t.Fatalf("its transmission state must be forgotten, got %v", ids)
	}
}

// TestScrollSyncAnchorsAroundImageBlock guards the #62 contract with #2180's
// image blocks present: the headings on both sides of an image still map to
// ordered rendered lines, and a cursor past the image scrolls to its section.
func TestScrollSyncAnchorsAroundImageBlock(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, dir, "wide.png", 80, 80)
	src := "# Top\n\nintro\n\n![wide](wide.png)\n\n## Bottom Section\n\nend\n"
	m := newImaged(dir, src, true)

	if len(m.anchors) != 2 {
		t.Fatalf("anchors = %d, want 2", len(m.anchors))
	}
	if m.anchors[0].rendered < 0 || m.anchors[1].rendered <= m.anchors[0].rendered {
		t.Fatalf("headings must stay ordered around the image block: %+v", m.anchors)
	}
	// The image block really does sit between them.
	block := 0
	for i := m.anchors[0].rendered; i < m.anchors[1].rendered; i++ {
		block += placeholders(m.lines[i])
	}
	if block == 0 {
		t.Fatal("the image block should render between the two headings")
	}
	// The second heading's rendered row is where the cursor on it maps to.
	if got := m.mapLine(6); got != m.anchors[1].rendered {
		t.Fatalf("cursor on the second heading maps to %d, want %d", got, m.anchors[1].rendered)
	}
}

// ansiPlain joins rendered lines with their escape sequences removed.
func ansiPlain(lines []string) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		plain, _ := plainMap(l)
		out[i] = plain
	}
	return strings.Join(out, "\n")
}
