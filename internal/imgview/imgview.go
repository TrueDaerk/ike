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
	// applied transmission state, owned by the app's reconcile pass.
	sentCols, sentRows int
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

// Grid returns the placement size for the current pane interior.
func (m *Model) Grid() (cols, rows int) {
	if m.imgRef == nil {
		return 0, 0
	}
	return FitGrid(m.imgW, m.imgH, m.w, m.h)
}

// SyncSeqs returns the raw sequences bringing the terminal's placement in
// line with the current grid — a transmit on first show, delete + transmit
// after a resize, nothing when already current — and records the applied
// state. Called by the app's reconcile pass, only on supporting terminals.
func (m *Model) SyncSeqs() []string {
	if m.imgRef == nil {
		return nil
	}
	cols, rows := m.Grid()
	if cols == m.sentCols && rows == m.sentRows {
		return nil
	}
	var out []string
	if m.sentCols > 0 {
		out = append(out, Delete(m.id))
	}
	seq, err := Transmit(m.id, *m.imgRef, cols, rows)
	if err != nil {
		return nil
	}
	m.sentCols, m.sentRows = cols, rows
	return append(out, seq)
}

// Transmitted reports whether the terminal currently holds a placement.
func (m *Model) Transmitted() bool { return m.sentCols > 0 }

// Reset forgets the applied transmission state (#1547): the app deleted the
// pane's placement (workspace parked or torn down), so the next reconcile
// pass must transmit again instead of assuming the terminal still holds it.
func (m *Model) Reset() { m.sentCols, m.sentRows = 0, 0 }

// View renders the pane interior: the placeholder grid (centered) on a
// supporting terminal, the metadata summary otherwise.
func (m *Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	if m.gfx && m.err == nil && m.imgRef != nil {
		cols, rows := m.Grid()
		grid := PlaceholderGrid(m.id, cols, rows)
		pad := strings.Repeat(" ", (m.w-cols)/2)
		lines := make([]string, 0, m.h)
		top := (m.h - rows) / 2
		for i := 0; i < m.h; i++ {
			if i >= top && i-top < rows {
				lines = append(lines, pad+grid[i-top])
			} else {
				lines = append(lines, "")
			}
		}
		return strings.Join(lines, "\n")
	}
	return m.metadataView()
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
