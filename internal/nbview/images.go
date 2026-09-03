package nbview

// images.go renders a notebook's image outputs as pixels (#2425) through the
// same Kitty graphics path the image pane (#1479) and the markdown preview's
// inline images (#2180) use: each image/png or image/jpeg output is decoded
// once, held as a virtual placement, and drawn as a block of Unicode
// placeholder cells the terminal composites over. Where the terminal cannot
// do that, the metadata label above the block is the whole output.
//
// The bytes come out of the notebook itself (base64 in the MIME bundle), so
// nothing here touches the file system or the network.

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strconv"
	"sync/atomic"

	"ike/internal/imgview"
)

// nextImageID mints session-unique Kitty image ids for notebook outputs. The
// base sits above both other users of the protocol (imgview from 9000, the
// markdown preview from 30000) so the three can never collide in one
// terminal's graphics memory.
var nextImageID atomic.Int64

func init() { nextImageID.Store(60000) }

// imgKey addresses one image output inside the notebook.
type imgKey struct{ cell, out int }

// cellImage is one decoded image output plus the state of its Kitty
// placement: the grid the latest render wants and the grid the terminal
// actually holds, diffed by SyncSeqs exactly like the preview's inline
// images.
type cellImage struct {
	id     int
	img    image.Image
	imgW   int
	imgH   int
	format string
	size   int

	cols, rows         int // grid the latest render placed, 0 when unplaced
	sentCols, sentRows int // grid the terminal holds, 0 when nothing is resident
}

// meta is the label row above the placement: MIME type, pixel size and byte
// size — and on a terminal without graphics support it is the output.
func (im *cellImage) meta(mime string) string {
	return mime + " · " + strconv.Itoa(im.imgW) + "×" + strconv.Itoa(im.imgH) +
		" px · " + imgview.HumanSize(int64(im.size))
}

// image returns the decoded image for one output, decoding and caching it on
// first use. An undecodable payload yields nil and is not cached, so a
// re-render after a reload tries again.
func (m *Model) image(key imgKey, o Output) *cellImage {
	if m.images == nil {
		m.images = map[imgKey]*cellImage{}
	}
	if im, ok := m.images[key]; ok {
		return im
	}
	img, format, err := image.Decode(bytes.NewReader(o.Image))
	if err != nil {
		return nil
	}
	b := img.Bounds()
	im := &cellImage{id: int(nextImageID.Add(1)), img: img,
		imgW: b.Dx(), imgH: b.Dy(), format: format, size: len(o.Image)}
	m.images[key] = im
	return im
}

// forgetUnplaced drops the terminal-side state of every cached image the
// latest render did not place — a fold collapsing an image output, a reload
// removing it. The app's reconcile pass sees the id leave ImageIDs and
// deletes it; clearing the sent grid here makes a later render transmit again
// instead of trusting a placement that is gone.
func (m *Model) forgetUnplaced(placed map[imgKey]bool) {
	for key, im := range m.images {
		if placed[key] {
			continue
		}
		im.cols, im.rows = 0, 0
		im.sentCols, im.sentRows = 0, 0
	}
}

// SetGraphics pushes the terminal's Kitty graphics capability, re-rendering
// when it changes: a placement block and its metadata label occupy different
// numbers of rows, so the scroll positions have to be rebuilt with them.
func (m *Model) SetGraphics(ok bool) {
	if ok == m.gfx {
		return
	}
	m.gfx = ok
	m.render()
}

// HasImages reports whether the notebook holds at least one decodable image
// output — the signal the app uses to fire the Kitty capability probe.
func (m *Model) HasImages() bool {
	for _, c := range m.nb.Cells {
		for _, o := range c.Outputs {
			if o.HasImage() {
				return true
			}
		}
	}
	return false
}

// ImageIDs returns the Kitty image ids the latest render placed as pixels —
// the desired live set the app's reconcile pass diffs against. It is empty
// while the terminal's support is unknown or absent, so the metadata
// fallback never leaves phantom ids behind.
func (m *Model) ImageIDs() []int {
	var out []int
	for _, im := range m.images {
		if im.cols > 0 {
			out = append(out, im.id)
		}
	}
	return out
}

// TransmittedIDs returns the ids the terminal currently holds a placement for.
func (m *Model) TransmittedIDs() []int {
	var out []int
	for _, im := range m.images {
		if im.sentCols > 0 {
			out = append(out, im.id)
		}
	}
	return out
}

// SyncSeqs returns the raw sequences bringing the terminal's placements in
// line with the latest render — transmit on first show, delete + transmit
// after a resize, nothing when already current — and records the applied
// state. Called by the app's reconcile pass, only on supporting terminals.
func (m *Model) SyncSeqs() []string {
	var out []string
	for _, im := range m.images {
		if im.cols == 0 || (im.cols == im.sentCols && im.rows == im.sentRows) {
			continue
		}
		if im.sentCols > 0 {
			out = append(out, imgview.Delete(im.id))
		}
		seq, err := imgview.Transmit(im.id, im.img, im.cols, im.rows)
		if err != nil {
			continue
		}
		im.sentCols, im.sentRows = im.cols, im.rows
		out = append(out, seq)
	}
	return out
}

// ResetImages forgets every applied transmission (#1547's rule for the image
// pane): the app deleted this notebook's placements because the workspace
// parked or was torn down, so the next reconcile pass must transmit again.
func (m *Model) ResetImages() {
	for _, im := range m.images {
		im.sentCols, im.sentRows = 0, 0
	}
}
