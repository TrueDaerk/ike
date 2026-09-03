package preview

// images.go renders the local images a markdown buffer references inside the
// preview (#2180) through the same Kitty graphics path the image pane uses
// (internal/imgview, #1479): each referenced file is decoded once, held as a
// virtual placement, and its rendered "Image: alt → target" line is swapped
// for a block of Unicode placeholder cells the terminal composites the pixels
// over. Everything here is local-only — a remote image URL is never fetched,
// it stays the text glamour rendered.

import (
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"charm.land/lipgloss/v2"
	_ "golang.org/x/image/webp"

	"ike/internal/imgview"
)

// nextInlineID mints session-unique Kitty image ids for inline previews. The
// base sits far above the image pane's range (imgview starts at 9000) so the
// two features can never collide in one terminal's graphics memory.
var nextInlineID atomic.Int64

func init() { nextInlineID.Store(30000) }

// imageRefRe matches an inline markdown image and captures its destination.
// A trailing title ("![a](p 'title')") and the angle-bracket form are both
// tolerated; reference-style images resolve to nothing and stay text.
var imageRefRe = regexp.MustCompile(`!\[[^\]]*\]\(\s*<?([^)\s>]+)>?[^)]*\)`)

// schemeRe matches an absolute URL, the marker of a remote image or link.
var schemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]*:`)

// Remote reports whether a markdown destination points outside the file
// system — an http(s) URL, mailto:, anything carrying a scheme. Remote
// targets are never opened for their bytes: the preview does no network I/O.
func Remote(target string) bool {
	return schemeRe.MatchString(target)
}

// inlineImage is one decoded local image plus the state of its Kitty
// placement (imgview.PlacedImage), diffed by imgview.SyncSeqs like the image
// pane's own reconcile.
type inlineImage struct {
	imgview.PlacedImage
	imgW   int
	imgH   int
	format string
	size   int64
}

// meta is the fallback caption: format, pixel size and file size, shown where
// the terminal cannot composite the pixels.
func (im *inlineImage) meta() string {
	return strings.ToUpper(im.format) + " · " +
		strconv.Itoa(im.imgW) + "×" + strconv.Itoa(im.imgH) + " px · " + imgview.HumanSize(im.size)
}

// sourceImages returns the destinations of the buffer's inline images in
// reading order, skipping fenced code (where an image is a literal, not a
// reference).
func sourceImages(src string) []string {
	var out []string
	inFence := false
	for _, line := range strings.Split(src, "\n") {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, m := range imageRefRe.FindAllStringSubmatch(line, -1) {
			out = append(out, m[1])
		}
	}
	return out
}

// loadImage decodes the image target references, resolved against the
// previewed file's directory, and caches the decoded pixels on the model.
// Remote targets are refused outright; a target that cannot be opened or
// decoded returns nil and keeps glamour's text rendering. Failures are not
// cached, so an image added or fixed while the preview is open shows up on
// the next render.
func (m *Model) loadImage(target string) *inlineImage {
	if Remote(target) {
		return nil
	}
	path := target
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(m.path), path)
	}
	path = filepath.Clean(path)
	if im := m.images[path]; im != nil {
		return im
	}
	im := decodeImage(path)
	if im == nil {
		return nil
	}
	if m.images == nil {
		m.images = map[string]*inlineImage{}
	}
	m.images[path] = im
	return im
}

// decodeImage reads and decodes one image file, or returns nil.
func decodeImage(path string) *inlineImage {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		return nil
	}
	img, format, err := image.Decode(f)
	if err != nil {
		return nil
	}
	b := img.Bounds()
	return &inlineImage{
		PlacedImage: imgview.PlacedImage{ID: int(nextInlineID.Add(1)), Img: img},
		imgW:        b.Dx(), imgH: b.Dy(), format: format, size: st.Size(),
	}
}

// placeImages rewrites the rendered lines so every local image the buffer
// references shows as pixels (Kitty-capable terminal) or as its glamour text
// plus a metadata caption (everywhere else), and so every finished diagram
// render (#2421) replaces its sentinel row. It returns the new line slice and
// the rows that became image blocks, which carry no selectable link. Rendered
// image lines pair with source references positionally, the way
// anchorHeadings pairs headings, so repeated targets stay in order.
func (m *Model) placeImages(lines []string, subs map[string]*diagramSub) ([]string, map[int]bool) {
	m.placed = nil
	rows := make(map[int]*inlineImage)
	next := 0
	for _, target := range sourceImages(m.src) {
		row, ok := findLinkRow(lines, target, next)
		if !ok {
			continue
		}
		next = row + 1
		im := m.loadImage(target)
		if im == nil {
			continue
		}
		rows[row] = im
		// One file referenced twice is one placement drawn at both rows —
		// placeholder cells carry the id, so the terminal composites it
		// wherever they appear. It must still be listed once.
		if !slices.Contains(m.placed, im) {
			m.placed = append(m.placed, im)
		}
	}
	// Diagram placements join m.placed before the sweep, so an image-mode
	// diagram reconciles with the terminal exactly like an inline image.
	diags := m.diagramRows(lines, subs)
	m.forgetUnplaced()
	if len(rows) == 0 && len(diags) == 0 {
		return lines, nil
	}
	maxCols := max(1, m.w-4) // glamour's body margin on both sides
	maxRows := max(1, m.h-2)
	out := make([]string, 0, len(lines)+(len(rows)+len(diags))*4)
	blocks := map[int]bool{}
	for i, line := range lines {
		if sub, ok := diags[i]; ok {
			// The sentinel row is the whole diagram: its text, its
			// placeholder cells, or the note under an unrendered fence.
			sublines, gridRows := m.diagramLines(sub)
			for j, l := range sublines {
				if j < gridRows {
					blocks[len(out)] = true
				}
				out = append(out, l)
			}
			continue
		}
		im, ok := rows[i]
		if !ok {
			out = append(out, line)
			continue
		}
		if !m.gfx {
			// Fallback: keep glamour's "Image: alt → target" line and add
			// what the pixels would have shown. Nothing is placed, so the
			// grid is cleared — the id must not look live to the reconcile.
			im.Cols, im.Rows = 0, 0
			out = append(out, line, "  "+m.dim().Render(im.meta()))
			continue
		}
		im.Cols, im.Rows = imgview.FitGrid(im.imgW, im.imgH, maxCols, maxRows)
		for _, grid := range imgview.PlaceholderGrid(im.ID, im.Cols, im.Rows) {
			blocks[len(out)] = true
			out = append(out, "  "+grid)
		}
	}
	return out, blocks
}

// findLinkRow returns the first line at or after from whose OSC 8 hyperlinks
// point at target.
func findLinkRow(lines []string, target string, from int) (int, bool) {
	for i := from; i < len(lines); i++ {
		for _, h := range scanHyperlinks(lines[i]) {
			if h.uri == target {
				return i, true
			}
		}
	}
	return 0, false
}

// forgetUnplaced drops the terminal-side state of every cached image the
// latest render did not place. The app's reconcile pass sees the id leave
// ImageIDs and deletes it; clearing the sent grid here makes a later
// reference transmit again instead of trusting a placement that is gone.
func (m *Model) forgetUnplaced() {
	placed := make(map[int]bool, len(m.placed))
	for _, im := range m.placed {
		placed[im.ID] = true
	}
	for _, im := range m.images {
		if placed[im.ID] {
			continue
		}
		im.Cols, im.Rows = 0, 0
		im.SentCols, im.SentRows = 0, 0
	}
}

// SetGraphics pushes the terminal's Kitty graphics capability, re-rendering
// when it changes: inline pixels and the metadata fallback occupy different
// numbers of lines, so the scroll anchors have to be rebuilt with them.
func (m *Model) SetGraphics(ok bool) {
	if ok == m.gfx {
		return
	}
	m.gfx = ok
	m.render()
}

// HasImages reports whether the latest render placed at least one decodable
// local image — the signal the app uses to fire the Kitty capability probe.
func (m *Model) HasImages() bool { return len(m.placed) > 0 }

// ImageIDs returns the Kitty image ids the latest render placed as pixels —
// the desired live set the app's reconcile pass diffs against. It is empty
// while the terminal's support is unknown or absent, so the fallback never
// leaves phantom ids behind.
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
func (m *Model) SyncSeqs() []string {
	placed := make([]*imgview.PlacedImage, len(m.placed))
	for i, im := range m.placed {
		placed[i] = &im.PlacedImage
	}
	return imgview.SyncSeqs(placed)
}

// ResetImages forgets every applied transmission (#1547's rule for the image
// pane): the app deleted this preview's placements because the workspace
// parked or was torn down, so the next reconcile pass must transmit again.
func (m *Model) ResetImages() {
	for _, im := range m.images {
		im.SentCols, im.SentRows = 0, 0
	}
}

// dim is the muted style of the fallback caption.
func (m Model) dim() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(m.palette().Ghost)
}
