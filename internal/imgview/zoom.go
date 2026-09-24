package imgview

import (
	"fmt"
	"image"
	"math"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/image/draw"
)

// Zoom and pan (#2688). The pane keeps one Kitty placement filling its body
// and shows a *source rectangle* of the image: zooming shrinks the crop,
// panning moves it. The terminal ignores a placement's source rectangle for
// Unicode placeholders (#2730), so the crop is cut from the pixels and those
// are what the pane transmits (cropPixels). The zoom factor is relative to
// the fit size (1 = fit); steps are multiplicative (ZoomStep per step) and
// capped at the larger of MaxZoomFactor× fit and the 1:1 level, where one
// image pixel maps to one terminal pixel assuming CellPxW-pixel-wide cells.
// View state only — never persisted.

const (
	// cropCellPx is the pixel budget per placement cell width for a
	// transmitted crop (twice the nominal cell, so HiDPI cells stay sharp);
	// a larger crop is downscaled before it is sent, bounding the encode
	// cost of every zoom or pan step. Heights follow the 2:1 cell aspect.
	cropCellPx = 2 * CellPxW
	// ZoomStep is the multiplicative zoom step per key press or wheel tick.
	ZoomStep = 1.25
	// MaxZoomFactor is the zoom cap relative to fit when the 1:1 level is
	// smaller than it.
	MaxZoomFactor = 8.0
	// CellPxW is the nominal pixel width of a terminal cell used to derive
	// the 1:1 zoom level; the height follows the 2:1 cell aspect FitGrid
	// assumes.
	CellPxW = 10
	// panStep is the keyboard pan distance in cells for h/j/k/l and arrows.
	panStep = 3
)

// viewport is the resolved geometry for the current zoom: the grid the
// placement fills and the crop it shows.
type viewport struct {
	cols, rows int             // placement grid in cells
	left, top  int             // grid origin inside the body (centred)
	crop       image.Rectangle // source rectangle in image pixels
}

// bodyRows is the pane height minus the footer line carrying the zoom label.
func (m *Model) bodyRows() int { return max(0, m.h-1) }

// fitGrid is the grid the whole image fits into at zoom 1.
func (m *Model) fitGrid() (cols, rows int) {
	return FitGrid(m.imgW, m.imgH, m.w, m.bodyRows())
}

// oneToOne is the zoom factor at which one image pixel maps to one terminal
// pixel (nominal cell width), relative to fit.
func (m *Model) oneToOne() float64 {
	fc, _ := m.fitGrid()
	if fc <= 0 {
		return 1
	}
	return float64(m.imgW) / float64(CellPxW) / float64(fc)
}

// maxZoom is the cap ZoomIn stops at.
func (m *Model) maxZoom() float64 { return math.Max(MaxZoomFactor, m.oneToOne()) }

// zoomFactor is the effective zoom: 0 (unset) and anything below 1 mean fit.
func (m *Model) zoomFactor() float64 {
	if m.zoom < 1 {
		return 1
	}
	return m.zoom
}

// AtFit reports whether the whole image is on screen (zoom 1).
func (m *Model) AtFit() bool { return m.zoomFactor() == 1 }

// Zoom returns the zoom factor relative to fit (1 at fit).
func (m *Model) Zoom() float64 { return m.zoomFactor() }

// ZoomLabel is the footer's zoom state: "fit", "1:1" at the pixel-exact level,
// otherwise the factor relative to fit ("2.4×").
func (m *Model) ZoomLabel() string {
	z := m.zoomFactor()
	switch {
	case z == 1:
		return "fit"
	case math.Abs(z-m.oneToOne()) < 1e-9:
		return "1:1"
	default:
		return fmt.Sprintf("%.1f×", z)
	}
}

// geometry resolves the viewport for the current zoom and pan, clamping the
// pan so the crop never leaves the image and recording the clamped pan back.
func (m *Model) geometry() viewport {
	if m.imgRef == nil || m.w <= 0 || m.bodyRows() <= 0 {
		return viewport{}
	}
	fc, fr := m.fitGrid()
	z := m.zoomFactor()
	// The virtual grid the whole image would occupy at this zoom.
	vcols := max(fc, int(math.Round(float64(fc)*z)))
	vrows := max(fr, int(math.Round(float64(fr)*z)))
	v := viewport{cols: min(vcols, m.w), rows: min(vrows, m.bodyRows())}
	v.left = (m.w - v.cols) / 2
	v.top = (m.bodyRows() - v.rows) / 2
	cw, ch := m.imgW, m.imgH
	if v.cols < vcols {
		cw = max(1, int(math.Round(float64(m.imgW)*float64(v.cols)/float64(vcols))))
	}
	if v.rows < vrows {
		ch = max(1, int(math.Round(float64(m.imgH)*float64(v.rows)/float64(vrows))))
	}
	m.panX = clampF(m.panX, 0, float64(m.imgW-cw))
	m.panY = clampF(m.panY, 0, float64(m.imgH-ch))
	x, y := int(math.Round(m.panX)), int(math.Round(m.panY))
	v.crop = image.Rect(x, y, x+cw, y+ch)
	return v
}

func clampF(v, lo, hi float64) float64 {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Crop returns the source rectangle the placement shows (the full image at
// fit), in image pixels.
func (m *Model) Crop() image.Rectangle { return m.geometry().crop }

// cropPixels returns the pixels to transmit for viewport v: the decoded image
// itself at fit, otherwise its crop copied out — downscaled to cropCellPx
// pixels per cell when larger — since the placement shows whatever image it
// holds in full (#2730).
func (m *Model) cropPixels(v viewport) image.Image {
	src := *m.imgRef
	full := image.Rect(0, 0, m.imgW, m.imgH)
	if v.crop.Empty() || v.crop == full {
		return src
	}
	r := v.crop.Add(src.Bounds().Min)
	w, h := r.Dx(), r.Dy()
	if maxW, maxH := v.cols*cropCellPx, v.rows*2*cropCellPx; w > maxW || h > maxH {
		s := math.Min(float64(maxW)/float64(w), float64(maxH)/float64(h))
		w = max(1, int(math.Round(float64(w)*s)))
		h = max(1, int(math.Round(float64(h)*s)))
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	if w == r.Dx() && h == r.Dy() {
		draw.Draw(dst, dst.Bounds(), src, r.Min, draw.Src)
	} else {
		draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, r, draw.Src, nil)
	}
	return dst
}

// cellFraction maps a body-local cell to its position across the grid as a
// 0..1 fraction per axis: the first column is the crop's left edge, the last
// its right edge (a one-cell grid is its middle). Zooming pins the image
// point at that fraction, so a corner cell keeps the image corner in view.
func cellFraction(v viewport, cx, cy int) (fx, fy float64) {
	fx, fy = 0.5, 0.5
	if v.cols > 1 {
		fx = clampF(float64(cx-v.left)/float64(v.cols-1), 0, 1)
	}
	if v.rows > 1 {
		fy = clampF(float64(cy-v.top)/float64(v.rows-1), 0, 1)
	}
	return fx, fy
}

// setZoomAt applies zoom z keeping the image point under body-local cell
// (cx, cy) fixed; a negative cell pins the view centre instead.
func (m *Model) setZoomAt(z float64, cx, cy int) {
	if m.imgRef == nil {
		return
	}
	z = clampF(z, 1, m.maxZoom())
	if math.Abs(z-1) < 1e-9 {
		z = 1
	}
	if z == m.zoomFactor() {
		return
	}
	before := m.geometry()
	fx, fy := 0.5, 0.5
	if cx >= 0 && cy >= 0 {
		fx, fy = cellFraction(before, cx, cy)
	}
	px := float64(before.crop.Min.X) + fx*float64(before.crop.Dx())
	py := float64(before.crop.Min.Y) + fy*float64(before.crop.Dy())
	m.zoom = z
	if z == 1 {
		m.panX, m.panY = 0, 0
		return
	}
	// Resolve the new crop size with the pan untouched, then place the
	// pinned point back at the same fraction of the new crop.
	after := m.geometry()
	m.panX = px - fx*float64(after.crop.Dx())
	m.panY = py - fy*float64(after.crop.Dy())
	m.geometry() // clamp
}

// ZoomIn zooms one step in around the view centre; a no-op at the cap.
func (m *Model) ZoomIn() { m.setZoomAt(m.zoomFactor()*ZoomStep, -1, -1) }

// ZoomOut zooms one step out around the view centre; a no-op at fit.
func (m *Model) ZoomOut() { m.setZoomAt(m.zoomFactor()/ZoomStep, -1, -1) }

// ZoomFit restores the fit level (the whole-picture view).
func (m *Model) ZoomFit() { m.setZoomAt(1, -1, -1) }

// ZoomAt zooms dir steps (positive in, negative out) around body-local cell
// (x, y) — the alt+wheel gesture, centred on the pointer.
func (m *Model) ZoomAt(dir, x, y int) {
	if dir == 0 {
		return
	}
	z := m.zoomFactor() * math.Pow(ZoomStep, float64(dir))
	m.setZoomAt(z, x, y)
}

// Pan moves the crop by dx columns and dy rows of the placement grid; a
// no-op at fit, clamped to the image otherwise.
func (m *Model) Pan(dx, dy int) {
	if m.AtFit() || m.imgRef == nil {
		return
	}
	v := m.geometry()
	if v.cols == 0 || v.rows == 0 {
		return
	}
	m.panX += float64(dx) * float64(v.crop.Dx()) / float64(v.cols)
	m.panY += float64(dy) * float64(v.crop.Dy()) / float64(v.rows)
	m.geometry() // clamp
}

// Wheel pans vertically by delta rows (the plain wheel).
func (m *Model) Wheel(delta int) { m.Pan(0, delta) }

// WheelX pans horizontally by delta columns (shift+wheel, horizontal wheel).
func (m *Model) WheelX(delta int) { m.Pan(delta, 0) }

// MousePress anchors a drag at body-local cell (x, y); the following
// MouseDrag calls move the picture with the pointer. At fit there is nothing
// to drag and the press is inert.
func (m *Model) MousePress(x, y int) {
	m.dragX, m.dragY = x, y
	m.dragging = !m.AtFit()
}

// MouseDrag pans so the image follows the pointer from the last drag cell.
func (m *Model) MouseDrag(x, y int) {
	if !m.dragging {
		return
	}
	dx, dy := x-m.dragX, y-m.dragY
	m.dragX, m.dragY = x, y
	if dx != 0 || dy != 0 {
		m.Pan(-dx, -dy)
	}
}

// MouseRelease ends a drag.
func (m *Model) MouseRelease() { m.dragging = false }

// Update handles one key press in the focused pane: the zoom keys, the
// viewer pan motions and the half-page moves. Keys do nothing without
// graphics support or a decoded image — the metadata card has nothing to
// zoom.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok || !m.gfx || m.err != nil || m.imgRef == nil {
		return nil
	}
	half := max(1, m.bodyRows()/2)
	switch key.String() {
	case "+", "=":
		m.ZoomIn()
	case "-":
		m.ZoomOut()
	case "0":
		m.ZoomFit()
	case "h", "left":
		m.Pan(-panStep, 0)
	case "l", "right":
		m.Pan(panStep, 0)
	case "k", "up":
		m.Pan(0, -panStep)
	case "j", "down":
		m.Pan(0, panStep)
	case "ctrl+u":
		m.Pan(0, -half)
	case "ctrl+d":
		m.Pan(0, half)
	}
	return nil
}
