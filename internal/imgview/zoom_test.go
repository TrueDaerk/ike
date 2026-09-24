package imgview

import (
	"image"
	"math"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/theme"
)

// zoomModel is a 640×320 image in a 60×11 pane (10 body rows + footer) with
// graphics on: the fit grid is 40×10, so there is room to zoom in.
func zoomModel(t *testing.T) Model {
	t.Helper()
	m := New("image", writePNG(t, 640, 320), theme.DefaultPalette())
	m.SetSize(60, 11)
	m.SetGraphics(true)
	return m
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "left", "right", "up", "down":
		codes := map[string]rune{"left": tea.KeyLeft, "right": tea.KeyRight, "up": tea.KeyUp, "down": tea.KeyDown}
		return tea.KeyPressMsg{Code: codes[s]}
	}
	if s == "ctrl+d" || s == "ctrl+u" {
		return tea.KeyPressMsg{Code: rune(s[len(s)-1]), Mod: tea.ModCtrl}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func TestZoomStepsAreMultiplicativeAndCapped(t *testing.T) {
	m := zoomModel(t)
	if !m.AtFit() || m.ZoomLabel() != "fit" {
		t.Fatalf("a fresh pane is at fit, got %q", m.ZoomLabel())
	}
	// - at fit is a no-op.
	m.Update(key("-"))
	if !m.AtFit() {
		t.Fatal("- at fit must be a no-op")
	}
	m.Update(key("+"))
	if got := m.Zoom(); math.Abs(got-ZoomStep) > 1e-9 {
		t.Fatalf("+ zooms one step: %v", got)
	}
	m.Update(key("="))
	if got := m.Zoom(); math.Abs(got-ZoomStep*ZoomStep) > 1e-9 {
		t.Fatalf("= zooms one more step: %v", got)
	}
	if lbl := m.ZoomLabel(); lbl != "1.6×" {
		t.Fatalf("label after two steps: %q", lbl)
	}
	m.Update(key("-"))
	if got := m.Zoom(); math.Abs(got-ZoomStep) > 1e-9 {
		t.Fatalf("- steps back down: %v", got)
	}
	// The loop ends: zooming in stops at the cap.
	for i := 0; i < 100; i++ {
		m.ZoomIn()
	}
	if got, want := m.Zoom(), m.maxZoom(); math.Abs(got-want) > 1e-9 {
		t.Fatalf("zoom in caps at %v, got %v", want, got)
	}
	// 640 px / 10 px per cell = 64 cells at 1:1, over a 40-col fit → 1.6×,
	// below the 8× floor: the cap is 8× and the 1:1 label never shows here.
	if got := m.maxZoom(); got != MaxZoomFactor {
		t.Fatalf("cap = max(8, 1:1): got %v", got)
	}
	m.Update(key("0"))
	if !m.AtFit() || m.Crop() != image.Rect(0, 0, 640, 320) {
		t.Fatalf("0 restores fit, got zoom %v crop %v", m.Zoom(), m.Crop())
	}
}

func TestOneToOneLabel(t *testing.T) {
	// A wide image: 4000 px / 10 = 400 cells over a 20-col fit → 1:1 at 20×,
	// above the 8× floor, so the cap is the pixel-exact level.
	m := New("image", writePNG(t, 4000, 200), theme.DefaultPalette())
	m.SetSize(20, 11)
	m.SetGraphics(true)
	for i := 0; i < 100; i++ {
		m.ZoomIn()
	}
	if got := m.ZoomLabel(); got != "1:1" {
		t.Fatalf("at the cap the label reads 1:1, got %q (zoom %v)", got, m.Zoom())
	}
}

func TestZoomedCropFillsBodyAndClamps(t *testing.T) {
	m := zoomModel(t)
	if cols, rows := m.Grid(); cols != 40 || rows != 10 {
		t.Fatalf("fit grid = %d×%d, want 40×10", cols, rows)
	}
	m.ZoomIn()
	// 1.25× fit: the virtual grid (50×13) exceeds the body (60×10)
	// vertically only, so the placement is 50 cols × 10 rows and the crop
	// keeps the full width.
	cols, rows := m.Grid()
	if cols != 50 || rows != 10 {
		t.Fatalf("zoomed grid = %d×%d, want 50×10", cols, rows)
	}
	crop := m.Crop()
	if crop.Dx() != 640 || crop.Dy() >= 320 || crop.Dy() < 100 {
		t.Fatalf("crop = %v, want full width and a shorter height", crop)
	}
	// Centred zoom keeps the middle of the image in view.
	if crop.Min.Y == 0 {
		t.Fatalf("centre zoom must not pin to the top: %v", crop)
	}
	// Pan down clamps at the bottom edge, up at the top.
	for i := 0; i < 50; i++ {
		m.Update(key("j"))
	}
	if c := m.Crop(); c.Max.Y != 320 {
		t.Fatalf("pan down clamps to the image bottom: %v", c)
	}
	for i := 0; i < 50; i++ {
		m.Update(key("ctrl+u"))
	}
	if c := m.Crop(); c.Min.Y != 0 {
		t.Fatalf("pan up clamps to the image top: %v", c)
	}
	// Horizontal pan is a no-op while the whole width is in view.
	m.Update(key("l"))
	if c := m.Crop(); c.Min.X != 0 {
		t.Fatalf("no horizontal room, yet the crop moved: %v", c)
	}
	// Zoom further: now the width is cropped too and h/l move it.
	for i := 0; i < 3; i++ {
		m.ZoomIn()
	}
	if c := m.Crop(); c.Dx() >= 640 {
		t.Fatalf("at %v the width must be cropped: %v", m.Zoom(), c)
	}
	before := m.Crop()
	m.Update(key("l"))
	if c := m.Crop(); c.Min.X <= before.Min.X {
		t.Fatalf("l pans right: %v → %v", before, c)
	}
	m.Update(key("h"))
	if c := m.Crop(); c != before {
		t.Fatalf("h pans back: %v vs %v", c, before)
	}
	for i := 0; i < 200; i++ {
		m.Update(key("right"))
	}
	if c := m.Crop(); c.Max.X != 640 || c.Min.X < 0 {
		t.Fatalf("pan right clamps to the image edge: %v", c)
	}
}

func TestPanAndDragAreInertAtFit(t *testing.T) {
	m := zoomModel(t)
	full := image.Rect(0, 0, 640, 320)
	m.Wheel(3)
	m.WheelX(3)
	m.Update(key("j"))
	m.MousePress(5, 5)
	m.MouseDrag(2, 2)
	m.MouseRelease()
	if c := m.Crop(); c != full {
		t.Fatalf("nothing pans at fit, crop = %v", c)
	}
	cols, rows := m.Grid()
	if fc, fr := FitGrid(640, 320, 60, 10); cols != fc || rows != fr {
		t.Fatalf("fit grid %d×%d, want %d×%d", cols, rows, fc, fr)
	}
}

func TestMouseDragPansWithPointer(t *testing.T) {
	m := zoomModel(t)
	for i := 0; i < 8; i++ {
		m.ZoomIn()
	}
	before := m.Crop()
	m.MousePress(20, 5)
	m.MouseDrag(15, 3) // pointer moved left/up → picture follows → crop moves right/down
	after := m.Crop()
	if after.Min.X <= before.Min.X || after.Min.Y <= before.Min.Y {
		t.Fatalf("drag must move the crop right/down: %v → %v", before, after)
	}
	m.MouseRelease()
	m.MouseDrag(0, 0)
	if c := m.Crop(); c != after {
		t.Fatalf("a released drag must not pan: %v vs %v", c, after)
	}
}

func TestWheelZoomCentresOnPointer(t *testing.T) {
	m := zoomModel(t)
	v := m.geometry()
	// Zoom in on the top-left cell of the grid: the crop stays pinned there.
	m.ZoomAt(4, v.left, v.top)
	c := m.Crop()
	if c.Min.X != 0 || c.Min.Y != 0 {
		t.Fatalf("zoom at the top-left corner keeps it in view: %v", c)
	}
	m.ZoomFit()
	v = m.geometry()
	m.ZoomAt(4, v.left+v.cols-1, v.top+v.rows-1)
	c = m.Crop()
	if c.Max.X != 640 || c.Max.Y != 320 {
		t.Fatalf("zoom at the bottom-right corner keeps it in view: %v", c)
	}
	// Alt+wheel out all the way lands back at fit.
	m.ZoomAt(-10, 3, 3)
	if !m.AtFit() {
		t.Fatalf("wheel out past fit clamps at fit, zoom %v", m.Zoom())
	}
}

func TestSyncSeqsCropChange(t *testing.T) {
	m := zoomModel(t)
	first := m.SyncSeqs()
	if len(first) != 2 || !strings.Contains(first[0], "a=t") || !strings.Contains(first[1], "a=p") {
		t.Fatalf("first show transmits the pixels then places them: %q", first)
	}
	m.ZoomIn()
	seqs := m.SyncSeqs()
	// The placement cannot carry the crop — Kitty and Ghostty ignore x/y/w/h
	// on a Unicode-placeholder placement (#2730) — so a crop change frees the
	// old pixels and transmits the crop's.
	if len(seqs) != 3 {
		t.Fatalf("a zoom must delete + retransmit + place, got %q", seqs)
	}
	if !strings.Contains(seqs[0], "a=d") || !strings.Contains(seqs[0], "d=I") {
		t.Fatalf("the old pixels are freed: %q", seqs[0])
	}
	if !strings.Contains(seqs[1], "a=t") {
		t.Fatalf("the crop's pixels are transmitted: %q", seqs[1])
	}
	cols, rows := m.Grid()
	for _, want := range []string{"a=p", "U=1", "c=" + strconv.Itoa(cols), "r=" + strconv.Itoa(rows)} {
		if !strings.Contains(seqs[2], want) {
			t.Errorf("placement lacks %q: %q", want, seqs[2])
		}
	}
	if strings.Contains(seqs[2], "x=") || strings.Contains(seqs[2], "w=") {
		t.Fatalf("the placement must not rely on a protocol source rectangle: %q", seqs[2])
	}
	if again := m.SyncSeqs(); again != nil {
		t.Fatalf("unchanged crop must be idempotent, got %q", again)
	}
	// A pan changes only the crop origin: the moved crop is sent again.
	m.Update(key("j"))
	if seqs := m.SyncSeqs(); len(seqs) != 3 || !strings.Contains(seqs[1], "a=t") {
		t.Fatalf("a pan retransmits the moved crop: %q", seqs)
	}
	// Back at fit the whole image is sent again.
	m.Update(key("0"))
	if seqs := m.SyncSeqs(); len(seqs) != 3 || !strings.Contains(seqs[1], "a=t") || m.Crop() != image.Rect(0, 0, 640, 320) {
		t.Fatalf("0 retransmits the whole image: %q", seqs)
	}
	// Reset forgets the resident pixels: the next sync transmits again
	// without a delete.
	m.Reset()
	if seqs := m.SyncSeqs(); len(seqs) != 2 || !strings.Contains(seqs[0], "a=t") {
		t.Fatalf("after Reset the pixels are sent again: %q", seqs)
	}
}

// TestSyncSeqsCropOnlyChange pins the #2730 path: once the placement fills
// the body, a zoom step keeps cols/rows and only shrinks the crop — the
// placement must still be re-emitted with the new pixels.
func TestSyncSeqsCropOnlyChange(t *testing.T) {
	m := zoomModel(t)
	for i := 0; i < 5; i++ {
		m.ZoomIn()
	}
	m.SyncSeqs()
	cols, rows := m.Grid()
	if cols != 60 || rows != 10 {
		t.Fatalf("at %v the placement fills the 60×10 body, got %d×%d", m.Zoom(), cols, rows)
	}
	before := m.Crop()
	m.ZoomIn()
	if c, r := m.Grid(); c != cols || r != rows {
		t.Fatalf("grid must stay %d×%d, got %d×%d", cols, rows, c, r)
	}
	if m.Crop() == before {
		t.Fatalf("the crop must shrink: %v", before)
	}
	seqs := m.SyncSeqs()
	if len(seqs) != 3 || !strings.Contains(seqs[1], "a=t") || !strings.Contains(seqs[2], "a=p") {
		t.Fatalf("a crop-only change re-emits the placement with new pixels: %q", seqs)
	}
}

// TestCropShrinksEveryStepPastBody walks the zoom from fit to the cap for a
// portrait image (fit limited by the body height) and a landscape one (fit
// limited by the width): once an axis fills the body the placement stops
// growing on it, and every further step must still shrink the crop so the
// picture keeps magnifying (#2730).
func TestCropShrinksEveryStepPastBody(t *testing.T) {
	for _, tc := range []struct {
		name       string
		imgW, imgH int
		w, h       int
	}{
		{"portrait", 300, 900, 80, 21},
		{"landscape", 1600, 400, 40, 31},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New("image", writePNG(t, tc.imgW, tc.imgH), theme.DefaultPalette())
			m.SetSize(tc.w, tc.h)
			m.SetGraphics(true)
			full := image.Rect(0, 0, tc.imgW, tc.imgH)
			prevCols, prevRows := m.Grid()
			prev := m.Crop()
			filled := false
			for steps := 0; m.Zoom() < m.maxZoom(); steps++ {
				if steps > 100 {
					t.Fatal("zoom never reached the cap")
				}
				m.ZoomIn()
				cols, rows := m.Grid()
				c := m.Crop()
				shrank := c.Dx()*c.Dy() < prev.Dx()*prev.Dy()
				if cols == tc.w || rows == tc.h-1 {
					filled = true
					if prev != full && !shrank {
						t.Fatalf("zoom %v past the body must shrink the crop: %v → %v", m.Zoom(), prev, c)
					}
				}
				if cols <= prevCols && rows <= prevRows && !shrank {
					t.Fatalf("zoom %v changed nothing visible: grid %d×%d crop %v", m.Zoom(), cols, rows, c)
				}
				// The crop keeps the placement's aspect (cells are 1:2).
				want := float64(cols) / float64(2*rows)
				if got := float64(c.Dx()) / float64(c.Dy()); math.Abs(got-want)/want > 0.15 {
					t.Fatalf("zoom %v crop aspect %.3f vs grid %.3f", m.Zoom(), got, want)
				}
				prevCols, prevRows, prev = cols, rows, c
			}
			if !filled {
				t.Fatal("the zoom never filled the body")
			}
			m.Update(key("0"))
			if m.Crop() != full {
				t.Fatalf("0 shows the whole image again: %v", m.Crop())
			}
		})
	}
}

// TestCropPixels checks the transmitted pixels are the crop: the decoded
// image itself at fit, a copy of the crop when zoomed, downscaled to the
// per-cell budget when larger.
func TestCropPixels(t *testing.T) {
	m := zoomModel(t)
	if got := m.cropPixels(m.geometry()); got != *m.imgRef {
		t.Fatal("at fit the decoded image is sent as is")
	}
	for i := 0; i < 9; i++ {
		m.ZoomIn()
	}
	m.Pan(4, 2)
	v := m.geometry()
	px := m.cropPixels(v)
	if px.Bounds().Dx() != v.crop.Dx() || px.Bounds().Dy() != v.crop.Dy() {
		t.Fatalf("a small crop is sent unscaled: %v vs crop %v", px.Bounds(), v.crop)
	}
	src := *m.imgRef
	for _, p := range []image.Point{{0, 0}, {v.crop.Dx() - 1, v.crop.Dy() - 1}, {3, 5}} {
		r1, g1, b1, _ := px.At(p.X, p.Y).RGBA()
		r2, g2, b2, _ := src.At(v.crop.Min.X+p.X, v.crop.Min.Y+p.Y).RGBA()
		if r1 != r2 || g1 != g2 || b1 != b2 {
			t.Fatalf("pixel %v differs from the source at crop %v", p, v.crop)
		}
	}
	// A large image in a small pane: the crop is downscaled to the budget.
	big := New("image", writePNG(t, 2000, 1000), theme.DefaultPalette())
	big.SetSize(20, 6)
	big.SetGraphics(true)
	big.ZoomIn()
	v = big.geometry()
	px = big.cropPixels(v)
	if px.Bounds().Dx() > v.cols*cropCellPx || px.Bounds().Dy() > v.rows*2*cropCellPx {
		t.Fatalf("crop %v not bounded to %d×%d cells: %v", v.crop, v.cols, v.rows, px.Bounds())
	}
	if px.Bounds().Dx() >= v.crop.Dx() {
		t.Fatalf("a large crop must be downscaled: %v from %v", px.Bounds(), v.crop)
	}
}

func TestKeysInertWithoutGraphics(t *testing.T) {
	m := New("image", writePNG(t, 640, 320), theme.DefaultPalette())
	m.SetSize(60, 11)
	m.Update(key("+"))
	if !m.AtFit() {
		t.Fatal("zoom keys do nothing without Kitty graphics")
	}
	if v := m.View(); !strings.Contains(v, "no Kitty graphics support") || strings.Contains(v, "fit") {
		t.Fatalf("the metadata card is unchanged:\n%s", v)
	}
}

func TestFooterShowsZoom(t *testing.T) {
	m := zoomModel(t)
	if v := m.View(); !strings.Contains(v, "· fit") {
		t.Fatalf("footer must name the fit level:\n%s", v)
	}
	m.ZoomIn()
	m.ZoomIn()
	if v := m.View(); !strings.Contains(v, "1.6×") {
		t.Fatalf("footer must show the zoom factor:\n%s", v)
	}
	if lines := strings.Count(m.View(), "\n") + 1; lines != 11 {
		t.Fatalf("view fills the pane height with the footer: %d lines", lines)
	}
}
