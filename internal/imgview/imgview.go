// Package imgview is the image preview pane (#1479): it decodes an image
// file (PNG, JPEG, GIF first frame, WebP) and renders it through the Kitty
// graphics protocol's Unicode-placeholder flavour on supporting terminals
// (Ghostty, Kitty, WezTerm), or a metadata summary everywhere else. The
// protocol layer lives in kitty.go; the app reconciles transmissions per
// Update pass (transmit on open/resize, delete on close), so no ghost
// graphics survive a pane's lifecycle.
package imgview

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	_ "golang.org/x/image/webp"

	"ike/internal/theme"
	"ike/internal/ui"
)

// nextID mints session-unique Kitty image ids. The base keeps IKE's ids out
// of the low range other tools tend to use.
var nextID atomic.Int64

func init() { nextID.Store(9000) }

// Model is one image preview pane bound to a file path.
type Model struct {
	key    string
	path   string
	pal    *theme.Palette
	imgRef *image.Image // decoded pixels; pointer so value copies share them
	imgW   int
	imgH   int
	format string
	size   int64
	err    error

	w, h    int
	focused bool

	id int
	// gfx is the terminal's Kitty graphics capability as last pushed by the
	// app: nil-equivalent "unknown" is false — the metadata fallback shows
	// until support is confirmed.
	gfx bool
	// applied transmission state, owned by the app's reconcile pass: the
	// pixels resident under id, and the placement grid + crop the terminal
	// holds (#2688).
	sentData           bool
	sentCols, sentRows int
	sentCrop           image.Rectangle

	// zoom/pan view state (#2688): the factor relative to fit (0 or 1 =
	// fit) and the crop origin in image pixels; dragX/dragY anchor a
	// primary-button drag.
	zoom         float64
	panX, panY   float64
	dragX, dragY int
	dragging     bool
}

// New decodes the image at path into a fresh preview model. Decode errors
// are kept for View — the pane opens either way and explains itself.
func New(key, path string, pal *theme.Palette) Model {
	m := Model{key: key, path: path, pal: pal, id: int(nextID.Add(1))}
	f, err := os.Open(path)
	if err != nil {
		m.err = err
		return m
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil {
		m.size = st.Size()
	}
	img, format, err := image.Decode(f)
	if err != nil {
		m.err = err
		return m
	}
	m.imgRef = &img
	b := img.Bounds()
	m.imgW, m.imgH = b.Dx(), b.Dy()
	m.format = format
	return m
}

// Path returns the previewed file's path.
func (m *Model) Path() string { return m.path }

// ID returns the pane's Kitty image id.
func (m *Model) ID() int { return m.id }

// SetSize records the pane interior in cells.
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

// SetFocused records focus for the chrome; the image itself has no cursor.
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the theme palette.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// SetGraphics pushes the terminal's Kitty graphics capability.
func (m *Model) SetGraphics(ok bool) { m.gfx = ok }

// Grid returns the placement size for the current pane body: the fit grid
// at zoom 1, the body-filling grid of the zoomed crop otherwise (#2688).
func (m *Model) Grid() (cols, rows int) {
	if m.imgRef == nil {
		return 0, 0
	}
	v := m.geometry()
	return v.cols, v.rows
}

// SyncSeqs returns the raw sequences bringing the terminal's placement in
// line with the current grid and crop and records the applied state: the
// shown pixels plus a placement on first show, delete + retransmit + place
// when the crop changed (zoom, pan), delete-placement + re-place when only
// the grid did (a resize at fit), nothing when already current. Called by
// the app's reconcile pass, only on supporting terminals. The terminal holds
// exactly the crop's pixels under the pane's one id: Unicode-placeholder
// placements ignore a source rectangle and always show the whole stored
// image (#2730), so zooming and panning must change the pixels themselves.
func (m *Model) SyncSeqs() []string {
	if m.imgRef == nil {
		return nil
	}
	v := m.geometry()
	if v.cols == 0 || v.rows == 0 {
		return nil
	}
	if m.sentData && v.cols == m.sentCols && v.rows == m.sentRows && v.crop == m.sentCrop {
		return nil
	}
	var out []string
	if !m.sentData || v.crop != m.sentCrop {
		if m.sentData {
			out = append(out, Delete(m.id))
			m.Reset()
		}
		seq, err := TransmitData(m.id, m.cropPixels(v))
		if err != nil {
			return out
		}
		out = append(out, seq)
		m.sentData = true
	} else {
		out = append(out, DeletePlacements(m.id))
	}
	out = append(out, Place(m.id, v.cols, v.rows))
	m.sentCols, m.sentRows, m.sentCrop = v.cols, v.rows, v.crop
	return out
}

// Transmitted reports whether the terminal currently holds the image.
func (m *Model) Transmitted() bool { return m.sentData }

// Reset forgets the applied transmission state (#1547): the app deleted the
// pane's image (workspace parked or torn down), so the next reconcile pass
// must transmit again instead of assuming the terminal still holds it.
func (m *Model) Reset() {
	m.sentData = false
	m.sentCols, m.sentRows = 0, 0
	m.sentCrop = image.Rectangle{}
}

// View renders the pane interior: the placeholder grid (centered) above a
// footer naming the zoom level on a supporting terminal, the metadata
// summary otherwise.
func (m *Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	if m.gfx && m.err == nil && m.imgRef != nil {
		v := m.geometry()
		grid := PlaceholderGrid(m.id, v.cols, v.rows)
		pad := strings.Repeat(" ", v.left)
		lines := make([]string, 0, m.h)
		for i := 0; i < m.bodyRows(); i++ {
			if i >= v.top && i-v.top < v.rows {
				lines = append(lines, pad+grid[i-v.top])
			} else {
				lines = append(lines, "")
			}
		}
		lines = append(lines, m.footer())
		return strings.Join(lines, "\n")
	}
	return m.metadataView()
}

// footer is the status line under the picture: name, format, dimensions and
// the zoom level (#2688), truncated to the pane width.
func (m *Model) footer() string {
	dim := lipgloss.NewStyle().Foreground(m.pal.Ghost)
	s := fmt.Sprintf("%s · %s · %d×%d px · %s", filepath.Base(m.path),
		strings.ToUpper(m.format), m.imgW, m.imgH, m.ZoomLabel())
	return dim.Render(ansi.Truncate(s, m.w, "…"))
}

// metadataView is the fallback body: file name, format, dimensions and size,
// plus the reason no pixels are shown.
func (m *Model) metadataView() string {
	dim := lipgloss.NewStyle().Foreground(m.pal.Ghost)
	var lines []string
	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(m.pal.Foreground).Render(filepath.Base(m.path)))
	switch {
	case m.err != nil:
		lines = append(lines, dim.Render("cannot decode: "+m.err.Error()))
	default:
		lines = append(lines, dim.Render(fmt.Sprintf("%s · %d×%d px · %s",
			strings.ToUpper(m.format), m.imgW, m.imgH, HumanSize(m.size))))
		if !m.gfx {
			lines = append(lines, "", dim.Render("terminal has no Kitty graphics support — showing metadata"))
		}
	}
	body := strings.Join(lines, "\n")
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, body)
}

// HumanSize formats a byte count for the metadata line; the markdown
// preview's inline-image captions (#2180) share it. It delegates to
// ui.HumanSize (which also covers a GB branch this package never hit in
// practice), kept as a thin alias for its external callers.
func HumanSize(n int64) string { return ui.HumanSize(n) }

// NewFromImage wraps pixels decoded elsewhere as a preview model — the HTML
// preview's browser screenshot (#2746), decoded off the update loop from a
// PNG that is gone by the time the model exists. name labels the footer in
// place of a file name; size is the encoded byte count for the metadata card.
func NewFromImage(key, name string, img image.Image, format string, size int64, pal *theme.Palette) Model {
	m := Model{key: key, path: name, pal: pal, id: int(nextID.Add(1)), format: format, size: size}
	if img == nil {
		m.err = fmt.Errorf("no image")
		return m
	}
	m.imgRef = &img
	b := img.Bounds()
	m.imgW, m.imgH = b.Dx(), b.Dy()
	return m
}
