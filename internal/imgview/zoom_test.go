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
	if len(first) != 2 || strings.Contains(first[1], "x=") {
		t.Fatalf("the fit placement carries no crop: %q", first)
	}
	m.ZoomIn()
	seqs := m.SyncSeqs()
	if len(seqs) != 2 {
		t.Fatalf("a zoom must delete + re-place, got %q", seqs)
	}
	if !strings.Contains(seqs[0], "a=d") || !strings.Contains(seqs[0], "d=i") {
		t.Fatalf("the old placement is deleted (data kept): %q", seqs[0])
	}
	crop := m.Crop()
	for _, want := range []string{"a=p", "U=1", "x=" + strconv.Itoa(crop.Min.X), "y=" + strconv.Itoa(crop.Min.Y), "w=" + strconv.Itoa(crop.Dx()), "h=" + strconv.Itoa(crop.Dy())} {
		if !strings.Contains(seqs[1], want) {
			t.Errorf("crop placement lacks %q: %q", want, seqs[1])
		}
	}
	if strings.Contains(seqs[0]+seqs[1], "a=t") {
		t.Fatal("a zoom must not retransmit the pixels")
	}
	if again := m.SyncSeqs(); again != nil {
		t.Fatalf("unchanged crop must be idempotent, got %q", again)
	}
	// A pan changes only the crop origin: delete + re-place again.
	m.Update(key("j"))
	if seqs := m.SyncSeqs(); len(seqs) != 2 || !strings.Contains(seqs[1], "y="+strconv.Itoa(m.Crop().Min.Y)) {
		t.Fatalf("a pan re-places with the new origin: %q", seqs)
	}
	// Reset forgets the resident pixels: the next sync transmits again.
	m.Reset()
	if seqs := m.SyncSeqs(); len(seqs) != 2 || !strings.Contains(seqs[0], "a=t") {
		t.Fatalf("after Reset the pixels are sent again: %q", seqs)
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
