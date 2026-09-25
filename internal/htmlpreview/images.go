package htmlpreview

// images.go shows the local images an HTML document references inline
// (0530/5, #2743), through the Kitty graphics path the markdown preview's
// images use (internal/preview/images.go, #2180): each referenced file is
// decoded once and held as a virtual placement (imgview.PlacedImage), and the
// render core's Options.ImageBlock hook stands a block of Unicode placeholder
// cells in for the <img>, which the terminal composites the pixels over.
// Everything here is local-only — a remote src is never fetched, it stays the
// core's "[alt]" placeholder — and so is every image on a terminal without
// Kitty graphics, or with preview.html_images off.

import (
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"

	_ "golang.org/x/image/webp"

	"ike/internal/htmlrender"
	"ike/internal/imgview"
	"ike/internal/preview"
)

// nextImageID mints session-unique Kitty image ids for HTML previews. The
// base sits above the image pane's (9000), the markdown preview's (30000) and
// the notebook's (60000) ranges, so no two features collide in one
// terminal's graphics memory.
var nextImageID atomic.Int64

func init() { nextImageID.Store(90000) }

// localImagePath resolves an <img> src to the file it names: relative to the
// document's directory, absolute, or a file:// URL. Remote sources — any
// other scheme (http, https, data, …) and the scheme-relative "//host/x" —
// resolve to nothing, so the preview never does network I/O. A relative src
// is a URL path: its query and fragment are dropped and its escapes decoded.
func localImagePath(docPath, src string) (string, bool) {
	if src == "" || strings.HasPrefix(src, "//") {
		return "", false
	}
	if preview.Remote(src) {
		u, err := url.Parse(src)
		if err != nil || !strings.EqualFold(u.Scheme, "file") || (u.Host != "" && u.Host != "localhost") || u.Path == "" {
			return "", false
		}
		return filepath.Clean(filepath.FromSlash(u.Path)), true
	}
	if i := strings.IndexAny(src, "?#"); i >= 0 {
		src = src[:i]
	}
	if p, err := url.PathUnescape(src); err == nil {
		src = p
	}
	if src == "" {
		return "", false
	}
	path := filepath.FromSlash(src)
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(docPath), path)
	}
	return filepath.Clean(path), true
}

// imageRun is the image state of one off-loop render (#2745). The render
// goroutine must not touch the pane's cache or the placements the app's
// reconcile pass mutates, so it works on a private copy of the cache (the
// decoded pixels and ids of a PlacedImage never change, only its grid and
// sent state do) and records the grids it decides in its own map; apply
// adopts all of it on the loop, with the document.
type imageRun struct {
	path    string // the previewed document, which relative srcs resolve against
	h       int    // pane height, the block's height bound
	on, gfx bool   // preview.html_images and Kitty graphics support

	cache  map[string]*imgview.PlacedImage // the pane's cache plus this run's decodes
	placed []*imgview.PlacedImage          // drawn or decodable, reading order, each once
	grids  map[*imgview.PlacedImage][2]int // cols, rows of each drawn block
}

// newImageRun snapshots the pane's image inputs for one render.
func (m *Model) newImageRun() *imageRun {
	return &imageRun{path: m.path, h: m.h, on: m.imagesOn, gfx: m.gfx, cache: maps.Clone(m.images)}
}

// load decodes the image src references, through the run's cache. A remote
// src, or a file that cannot be opened or decoded, returns nil and keeps the
// placeholder. Failures are not cached, so an image added or fixed while the
// preview is open shows up on the next render.
func (r *imageRun) load(src string) *imgview.PlacedImage {
	path, ok := localImagePath(r.path, src)
	if !ok {
		return nil
	}
	if im := r.cache[path]; im != nil {
		return im
	}
	im := decodeImage(path)
	if im == nil {
		return nil
	}
	if r.cache == nil {
		r.cache = map[string]*imgview.PlacedImage{}
	}
	r.cache[path] = im
	return im
}

// decodeImage reads and decodes one image file, or returns nil.
func decodeImage(path string) *imgview.PlacedImage {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || st.IsDir() {
		return nil
	}
	img, _, err := image.Decode(f)
	if err != nil {
		return nil
	}
	return &imgview.PlacedImage{ID: int(nextImageID.Add(1)), Img: img}
}

// block is the render core's Options.ImageBlock hook: a decodable local
// image becomes a block of placeholder rows fitted to the width the core
// offers and to the pane height; everything else keeps "[alt]". Decoded
// images join r.placed even without graphics support — that is what makes
// HasImages fire the capability probe — but only a drawn block gives them a
// grid, so the fallback never looks live to the reconcile pass. It runs on
// the render goroutine and touches only the run.
func (r *imageRun) block(img htmlrender.Image, maxCols int) ([]string, int) {
	if !r.on {
		return nil, 0
	}
	im := r.load(img.Src)
	if im == nil {
		return nil, 0
	}
	// One file referenced twice is one placement drawn at both places —
	// placeholder cells carry the id — but it must be listed once.
	if !slices.Contains(r.placed, im) {
		r.placed = append(r.placed, im)
	}
	if !r.gfx {
		return nil, 0
	}
	b := im.Img.Bounds()
	cols, rows := imgview.FitGrid(b.Dx(), b.Dy(), max(1, maxCols), max(1, r.h-1))
	if g, ok := r.grids[im]; ok {
		// Drawn twice at different widths (say once inside a list): one
		// placement has one grid, so the later draw reuses the first's.
		cols, rows = g[0], g[1]
	} else {
		if r.grids == nil {
			r.grids = map[*imgview.PlacedImage][2]int{}
		}
		r.grids[im] = [2]int{cols, rows}
	}
	return imgview.PlaceholderGrid(im.ID, cols, rows), cols
}

// adoptImages takes a finished run's image state over on the loop: its cache
// (with the decodes it added), its placements and their grids. An image the
// render did not draw loses its grid.
func (m *Model) adoptImages(r *imageRun) {
	if r == nil {
		return
	}
	if r.cache != nil {
		m.images = r.cache
	}
	for _, im := range m.images {
		im.Cols, im.Rows = 0, 0
	}
	for im, g := range r.grids {
		im.Cols, im.Rows = g[0], g[1]
	}
	m.placed = r.placed
	m.forgetUnplaced()
}

// forgetUnplaced drops the terminal-side state of every cached image the
// latest render did not draw. The app's reconcile pass sees the id leave
// ImageIDs and deletes it; clearing the sent grid here makes a later
// reference transmit again instead of trusting a placement that is gone.
func (m *Model) forgetUnplaced() {
	for _, im := range m.images {
		if im.Cols == 0 {
			im.SentCols, im.SentRows = 0, 0
		}
	}
}

// SetGraphics pushes the terminal's Kitty graphics capability, re-rendering
// when it changes: a placeholder block and "[alt]" occupy different numbers
// of lines, so the source map has to be rebuilt with them.
func (m *Model) SetGraphics(ok bool) {
	if ok == m.gfx {
		return
	}
	m.gfx = ok
	m.invalidate()
}

// SetImagesEnabled applies preview.html_images: off, every <img> stays its
// "[alt]" placeholder and nothing is placed. A change re-renders.
func (m *Model) SetImagesEnabled(on bool) {
	if on == m.imagesOn {
		return
	}
	m.imagesOn = on
	m.invalidate()
}

// ImagesEnabled reports whether preview.html_images is on for this pane.
func (m *Model) ImagesEnabled() bool { return m.imagesOn }

// HasImages reports whether the latest render found at least one decodable
// local image — the signal the app uses to fire the Kitty capability probe.
func (m *Model) HasImages() bool { return len(m.placed) > 0 }

// ImageIDs returns the Kitty image ids the latest render drew as pixels —
// the desired live set the app's reconcile pass diffs against. It is empty
// while the terminal's support is unknown or absent.
func (m *Model) ImageIDs() []int {
	var out []int
	for _, im := range m.placed {
		if im.Cols > 0 {
			out = append(out, im.ID)
		}
	}
	return out
}

// TransmittedIDs returns the ids the terminal currently holds a placement for.
func (m *Model) TransmittedIDs() []int {
	var out []int
	for _, im := range m.placed {
		if im.SentCols > 0 {
			out = append(out, im.ID)
		}
	}
	return out
}

// SyncSeqs returns the raw sequences bringing the terminal's placements in
// line with the latest render (imgview.SyncSeqs). Called by the app's
// reconcile pass, only on supporting terminals.
func (m *Model) SyncSeqs() []string { return imgview.SyncSeqs(m.placed) }

// ResetImages forgets every applied transmission (#1547's rule): the app
// deleted this preview's placements because the workspace parked or was torn
// down, so the next reconcile pass must transmit again.
func (m *Model) ResetImages() {
	for _, im := range m.images {
		im.SentCols, im.SentRows = 0, 0
	}
}
