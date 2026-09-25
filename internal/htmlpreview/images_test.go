package htmlpreview

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"

	"ike/internal/theme"
)

// images_test.go guards the HTML preview's inline images (0530/5, #2743),
// mirroring internal/preview/images_test.go for the markdown preview.

// writeImage drops a solid w×h PNG (or JPEG, by extension) at dir/name.
func writeImage(t *testing.T, dir, name string, w, h int) string {
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
	if strings.HasSuffix(name, ".jpg") {
		err = jpeg.Encode(f, img, nil)
	} else {
		err = png.Encode(f, img)
	}
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// newImaged returns a sized preview bound to dir/page.html holding src.
func newImaged(dir, src string, gfx bool) Model {
	m := New("htmlpreview", filepath.Join(dir, "page.html"), theme.DefaultPalette())
	m.SetSize(60, 20)
	m.gfx = gfx
	m.SetSourceImmediate(src)
	m.Flush()
	return m
}

// placeholders counts the Kitty placeholder cells in the rendered lines.
func placeholders(lines []string) int {
	return strings.Count(strings.Join(lines, "\n"), string(kitty.Placeholder))
}

// plainText joins the rendered lines with their escape sequences removed.
func plainText(lines []string) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Strip(l)
	}
	return strings.Join(out, "\n")
}

// TestInlineImageRendersPlaceholders: on a Kitty-capable terminal a local
// PNG and JPEG referenced relatively become placeholder blocks with live ids
// instead of "[alt]", and the reconcile pass transmits each once.
func TestInlineImageRendersPlaceholders(t *testing.T) {
	dir := t.TempDir()
	writeImage(t, dir, "img/logo.png", 40, 20)
	writeImage(t, dir, "photo.jpg", 30, 30)
	m := newImaged(dir, `<h1>Doc</h1><p><img src="img/logo.png" alt="the logo"></p><p><img src="./photo.jpg?v=2"></p><p>tail</p>`, true)

	if !m.HasImages() {
		t.Fatal("the document references decodable local images")
	}
	ids := m.ImageIDs()
	if len(ids) != 2 {
		t.Fatalf("ImageIDs = %v, want two placements", ids)
	}
	if placeholders(m.Lines()) == 0 {
		t.Fatal("the images should render as placeholder cells")
	}
	if text := plainText(m.Lines()); strings.Contains(text, "[the logo]") || strings.Contains(text, "photo.jpg") {
		t.Fatalf("a drawn image must not also show its placeholder text:\n%s", text)
	}
	seqs := m.SyncSeqs()
	if len(seqs) != 2 || !strings.Contains(seqs[0], "a=T") {
		t.Fatalf("first show must transmit both images, got %d sequences", len(seqs))
	}
	if got := m.TransmittedIDs(); len(got) != 2 {
		t.Fatalf("TransmittedIDs = %v, want %v", got, ids)
	}
	if extra := m.SyncSeqs(); len(extra) != 0 {
		t.Fatalf("an unchanged placement must not retransmit, got %d sequences", len(extra))
	}
	// The block sits between the heading and the tail, and the source map
	// still lands the tail paragraph after it.
	if !strings.Contains(plainText(m.Lines()), "tail") {
		t.Fatal("content after the images must still render")
	}
}

// TestInlineImageRetransmitsOnResize: a narrower pane re-places the image,
// which deletes the old placement and transmits the new grid.
func TestInlineImageRetransmitsOnResize(t *testing.T) {
	dir := t.TempDir()
	writeImage(t, dir, "logo.png", 40, 20)
	m := newImaged(dir, `<img src="logo.png">`, true)
	m.SyncSeqs()
	m.SetSize(24, 20)
	m.Flush()
	seqs := m.SyncSeqs()
	if len(seqs) != 2 || !strings.Contains(seqs[0], "a=d") || !strings.Contains(seqs[1], "a=T") {
		t.Fatalf("a resize must delete then transmit, got %d sequences", len(seqs))
	}
}

// TestInlineImageFallback: without Kitty graphics the image stays "[alt]"
// (or "[image: name]"), nothing is placed or transmitted, and support
// arriving later re-renders into pixels.
func TestInlineImageFallback(t *testing.T) {
	dir := t.TempDir()
	writeImage(t, dir, "logo.png", 40, 20)
	m := newImaged(dir, `<p><img src="logo.png" alt="the logo"> <img src="logo.png"></p>`, false)

	text := plainText(m.Lines())
	if !strings.Contains(text, "[the logo]") || !strings.Contains(text, "[image: logo.png]") {
		t.Fatalf("the fallback must be the bracketed alt / name, got:\n%s", text)
	}
	if placeholders(m.Lines()) != 0 {
		t.Fatal("a terminal without Kitty graphics must get no placeholder cells")
	}
	if !m.HasImages() {
		t.Fatal("a decodable image must still fire the capability probe")
	}
	if ids := m.ImageIDs(); len(ids) != 0 {
		t.Fatalf("nothing is placed without graphics support, got %v", ids)
	}
	if seqs := m.SyncSeqs(); len(seqs) != 0 {
		t.Fatalf("the fallback must transmit nothing, got %d sequences", len(seqs))
	}
	m.SetGraphics(true)
	m.Flush()
	if placeholders(m.Lines()) == 0 {
		t.Fatal("SetGraphics(true) should re-render with the image block")
	}
	// One file referenced twice is one placement.
	if ids := m.ImageIDs(); len(ids) != 1 {
		t.Fatalf("ImageIDs = %v, want the one shared placement", ids)
	}
}

// TestRemoteImageIsNeverFetched guards the network boundary: an http(s),
// scheme-relative or data: src keeps its placeholder, produces no placement
// and never reaches the server.
func TestRemoteImageIsNeverFetched(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	src := `<p><img src="` + srv.URL + `/x.png" alt="remote"></p>
<p><img src="//` + host + `/y.png" alt="relative"></p>
<p><img src="data:image/png;base64,AAAA" alt="inline data"></p>`
	m := newImaged(t.TempDir(), src, true)
	m.SyncSeqs()
	if hits.Load() != 0 {
		t.Fatalf("the preview made %d network requests", hits.Load())
	}
	if m.HasImages() {
		t.Fatal("a remote image must not become a placement")
	}
	text := plainText(m.Lines())
	for _, want := range []string{"[remote]", "[relative]", "[inline data]"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s:\n%s", want, text)
		}
	}
}

// TestUnsupportedAndMissingImagesStayText: a file that does not decode (an
// SVG, a missing path) keeps its placeholder with no placement.
func TestUnsupportedAndMissingImagesStayText(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vec.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newImaged(dir, `<img src="vec.svg" alt="vector"><img src="nope/gone.png"><svg><circle r="3"/></svg>`, true)
	if m.HasImages() {
		t.Fatal("an undecodable src must not become a placement")
	}
	text := plainText(m.Lines())
	for _, want := range []string{"[vector]", "[image: gone.png]", "[svg]"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s:\n%s", want, text)
		}
	}
}

// TestAbsoluteAndFileURLSources: an absolute path and a file:// URL resolve
// like a relative src; a file:// URL naming another host does not.
func TestAbsoluteAndFileURLSources(t *testing.T) {
	dir := t.TempDir()
	abs := writeImage(t, dir, "sub dir/a.png", 16, 16)
	src := `<img src="` + abs + `"><img src="file://` + filepath.ToSlash(abs) + `"><img src="file://elsewhere/a.png" alt="far">`
	m := newImaged(t.TempDir(), src, true)
	if ids := m.ImageIDs(); len(ids) != 1 {
		t.Fatalf("absolute path and file:// URL are one placement, got %v", ids)
	}
	if !strings.Contains(plainText(m.Lines()), "[far]") {
		t.Fatal("a file:// URL on another host is not local")
	}
	if p, ok := localImagePath("/doc/page.html", "img/a%20b.png#frag"); !ok || p != filepath.FromSlash("/doc/img/a b.png") {
		t.Fatalf("relative src resolves to %q (%v)", p, ok)
	}
}

// TestPictureUsesImgFallback: a <picture> shows its <img>, drawn inline.
func TestPictureUsesImgFallback(t *testing.T) {
	dir := t.TempDir()
	writeImage(t, dir, "a.png", 16, 16)
	m := newImaged(dir, `<picture><source srcset="a.webp"><img src="a.png" alt="pic"></picture>`, true)
	if len(m.ImageIDs()) != 1 {
		t.Fatal("the picture's <img> should be placed")
	}
}

// TestImagesDisabledSetting: preview.html_images off keeps every image its
// placeholder, and turning it back on re-renders the pixels.
func TestImagesDisabledSetting(t *testing.T) {
	dir := t.TempDir()
	writeImage(t, dir, "a.png", 16, 16)
	m := newImaged(dir, `<img src="a.png" alt="pic">`, true)
	m.SyncSeqs()
	m.SetImagesEnabled(false)
	m.Flush()
	if m.HasImages() || len(m.ImageIDs()) != 0 || len(m.TransmittedIDs()) != 0 {
		t.Fatal("disabled images must leave no placement behind")
	}
	if !strings.Contains(plainText(m.Lines()), "[pic]") {
		t.Fatal("disabled images render their placeholder")
	}
	m.SetImagesEnabled(true)
	m.Flush()
	if len(m.ImageIDs()) != 1 || placeholders(m.Lines()) == 0 {
		t.Fatal("re-enabling must draw the image again")
	}
}

// TestUnreferencedImageLosesItsPlacement: editing the image out drops it
// from the live set and forgets its transmission, so the reconcile deletes it.
func TestUnreferencedImageLosesItsPlacement(t *testing.T) {
	dir := t.TempDir()
	writeImage(t, dir, "a.png", 8, 8)
	m := newImaged(dir, `<img src="a.png">`, true)
	m.SyncSeqs()
	if len(m.TransmittedIDs()) != 1 {
		t.Fatal("the image should be resident before the edit")
	}
	m.SetSourceImmediate("<p>no image any more</p>")
	m.Flush()
	if ids := m.ImageIDs(); len(ids) != 0 {
		t.Fatalf("the dropped image must leave the live set, got %v", ids)
	}
	if ids := m.TransmittedIDs(); len(ids) != 0 {
		t.Fatalf("its transmission state must be forgotten, got %v", ids)
	}
}

// TestResetImagesRetransmits: after a park released the placements, the
// next reconcile transmits again.
func TestResetImagesRetransmits(t *testing.T) {
	dir := t.TempDir()
	writeImage(t, dir, "a.png", 8, 8)
	m := newImaged(dir, `<img src="a.png">`, true)
	m.SyncSeqs()
	m.ResetImages()
	if len(m.TransmittedIDs()) != 0 {
		t.Fatal("Reset must forget the transmission")
	}
	if seqs := m.SyncSeqs(); len(seqs) != 1 || !strings.Contains(seqs[0], "a=T") {
		t.Fatalf("resume must transmit again, got %d sequences", len(seqs))
	}
}

// TestCursorSyncAroundImageBlock: the source map keeps the caret sync exact
// with a block in the flow — a caret below the image lands past it.
func TestCursorSyncAroundImageBlock(t *testing.T) {
	dir := t.TempDir()
	writeImage(t, dir, "wide.png", 80, 80)
	m := newImaged(dir, "<p>top</p>\n<img src=\"wide.png\">\n<p>bottom</p>\n", true)
	line, ok := m.doc.LineForSourceLine(2)
	if !ok || !strings.Contains(ansi.Strip(m.Lines()[line]), "bottom") {
		t.Fatalf("source line 2 maps to %d (%v): %q", line, ok, plainText(m.Lines()))
	}
	if placeholders(m.Lines()[:line]) == 0 {
		t.Fatal("the image block should sit above the bottom paragraph")
	}
}
