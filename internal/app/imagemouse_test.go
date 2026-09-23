package app

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi/kitty"

	"ike/internal/imgview"
)

// imageApp opens a 640×320 PNG in an image pane on a Kitty-capable terminal
// and returns the model, the pane key and the pane's content origin.
func imageApp(t *testing.T) (Model, string, int, int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "big.png")
	img := image.NewRGBA(image.Rect(0, 0, 640, 320))
	for y := 0; y < 320; y += 8 {
		for x := 0; x < 640; x += 8 {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x40, A: 0xff})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
	m := dismissOnboarding(newSized())
	tm, _ := m.Update(OpenImageMsg{Path: path})
	m = tm.(Model)
	// Acknowledge the capability probe: the pane renders placeholders and
	// takes zoom input from now on.
	tm, _ = m.Update(uv.KittyGraphicsEvent{Options: kitty.Options{ID: imgview.QueryID}, Payload: []byte("OK")})
	m = tm.(Model)
	keys := imageKeys(m)
	if len(keys) != 1 {
		t.Fatalf("expected one image pane, got %v", keys)
	}
	r, ok := m.lay.Panes[keys[0]]
	if !ok {
		t.Fatalf("no layout rect for %q", keys[0])
	}
	return m, keys[0], r.X + paneContentX, r.Y + paneContentY
}

func imageOf(m Model, key string) *imgview.Model {
	return m.bodyContent(key).Image()
}

// TestImageWheelPansOnlyWhenZoomed: the plain wheel and shift+wheel are inert
// at fit; once zoomed they pan the crop vertically and horizontally.
func TestImageWheelPansOnlyWhenZoomed(t *testing.T) {
	m, key, x, y := imageApp(t)
	iv := imageOf(m, key)
	full := iv.Crop()
	m = step(m, tea.MouseWheelMsg{X: x + 5, Y: y + 3, Button: tea.MouseWheelDown})
	m = step(m, tea.MouseWheelMsg{X: x + 5, Y: y + 3, Button: tea.MouseWheelDown, Mod: tea.ModShift})
	if iv.Crop() != full {
		t.Fatalf("the wheel must not pan at fit: %v", iv.Crop())
	}
	// Zoom in far enough that both axes are cropped.
	for i := 0; i < 8; i++ {
		iv.ZoomIn()
	}
	before := iv.Crop()
	m = step(m, tea.MouseWheelMsg{X: x + 5, Y: y + 3, Button: tea.MouseWheelDown})
	after := iv.Crop()
	if after.Min.Y <= before.Min.Y || after.Min.X != before.Min.X {
		t.Fatalf("the wheel pans vertically: %v → %v", before, after)
	}
	m = step(m, tea.MouseWheelMsg{X: x + 5, Y: y + 3, Button: tea.MouseWheelDown, Mod: tea.ModShift})
	shifted := iv.Crop()
	if shifted.Min.X <= after.Min.X || shifted.Min.Y != after.Min.Y {
		t.Fatalf("shift+wheel pans horizontally: %v → %v", after, shifted)
	}
	m = step(m, tea.MouseWheelMsg{X: x + 5, Y: y + 3, Button: tea.MouseWheelUp})
	if c := iv.Crop(); c.Min.Y >= shifted.Min.Y {
		t.Fatalf("wheel up pans back: %v → %v", shifted, c)
	}
	_ = m
}

// TestImageAltWheelZooms: alt+wheel zooms in and out around the pointer.
func TestImageAltWheelZooms(t *testing.T) {
	m, key, x, y := imageApp(t)
	iv := imageOf(m, key)
	m = step(m, tea.MouseWheelMsg{X: x + 5, Y: y + 3, Button: tea.MouseWheelUp, Mod: tea.ModAlt})
	if iv.AtFit() {
		t.Fatal("alt+wheel up must zoom in")
	}
	if iv.Zoom() < imgview.ZoomStep-1e-9 {
		t.Fatalf("one tick zooms one step, got %v", iv.Zoom())
	}
	m = step(m, tea.MouseWheelMsg{X: x + 5, Y: y + 3, Button: tea.MouseWheelDown, Mod: tea.ModAlt})
	if !iv.AtFit() {
		t.Fatalf("alt+wheel down zooms back out to fit, got %v", iv.Zoom())
	}
	m = step(m, tea.MouseWheelMsg{X: x + 5, Y: y + 3, Button: tea.MouseWheelDown, Mod: tea.ModAlt})
	if !iv.AtFit() {
		t.Fatal("alt+wheel down at fit is a no-op")
	}
	_ = m
}

// TestImageDragPans: a primary-button drag moves the zoomed picture; at fit
// the press only focuses the pane.
func TestImageDragPans(t *testing.T) {
	m, key, x, y := imageApp(t)
	iv := imageOf(m, key)
	full := iv.Crop()
	m = step(m, tea.MouseClickMsg{X: x + 20, Y: y + 5, Button: tea.MouseLeft})
	m = step(m, tea.MouseMotionMsg{X: x + 10, Y: y + 2, Button: tea.MouseLeft})
	m = step(m, tea.MouseReleaseMsg{X: x + 10, Y: y + 2, Button: tea.MouseLeft})
	if iv.Crop() != full {
		t.Fatalf("a drag at fit must not pan: %v", iv.Crop())
	}
	if m.drag != nil {
		t.Fatal("the release must end the drag")
	}
	for i := 0; i < 8; i++ {
		iv.ZoomIn()
	}
	before := iv.Crop()
	m = step(m, tea.MouseClickMsg{X: x + 20, Y: y + 5, Button: tea.MouseLeft})
	if m.drag == nil || m.drag.kind != dragImagePan {
		t.Fatal("a left press on the zoomed picture arms a pan drag")
	}
	m = step(m, tea.MouseMotionMsg{X: x + 10, Y: y + 2, Button: tea.MouseLeft})
	after := iv.Crop()
	if after.Min.X <= before.Min.X || after.Min.Y <= before.Min.Y {
		t.Fatalf("dragging left/up moves the crop right/down: %v → %v", before, after)
	}
	m = step(m, tea.MouseReleaseMsg{X: x + 10, Y: y + 2, Button: tea.MouseLeft})
	if m.drag != nil {
		t.Fatal("the release must end the drag")
	}
	m = step(m, tea.MouseMotionMsg{X: x + 30, Y: y + 8})
	if iv.Crop() != after {
		t.Fatalf("motion after the release must not pan: %v vs %v", iv.Crop(), after)
	}
}

// TestImageZoomKeysReachFocusedPane: +/-/0 in the focused image pane change
// the zoom through the ordinary key routing.
func TestImageZoomKeysReachFocusedPane(t *testing.T) {
	m, key, _, _ := imageApp(t)
	iv := imageOf(m, key)
	m.setFocus(key)
	m = step(m, tea.KeyPressMsg{Code: '+', Text: "+"})
	if iv.AtFit() {
		t.Fatal("+ must zoom the focused image pane")
	}
	m = step(m, tea.KeyPressMsg{Code: '0', Text: "0"})
	if !iv.AtFit() {
		t.Fatal("0 must restore fit")
	}
	m = step(m, tea.KeyPressMsg{Code: '-', Text: "-"})
	if !iv.AtFit() {
		t.Fatal("- at fit is a no-op")
	}
	// A zoom change flows through the same Update pass's reconcile as
	// delete-placement + re-place with a crop, without a retransmission.
	tm, cmd := m.Update(tea.KeyPressMsg{Code: '=', Text: "="})
	m = tm.(Model)
	raw := rawStrings(cmd)
	if !strings.Contains(raw, "d=i") || !strings.Contains(raw, "a=p") || !strings.Contains(raw, "x=") {
		t.Fatalf("a zoom change must re-place the image with a crop, got %.200q", raw)
	}
	if strings.Contains(raw, "a=t") {
		t.Fatal("a zoom change must not retransmit the pixels")
	}
	if rawStrings(m.imageSyncCmd()) != "" {
		t.Fatal("the reconcile is idempotent once the crop is applied")
	}
}
