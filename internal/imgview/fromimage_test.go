package imgview

import (
	"image"
	"strings"
	"testing"

	"ike/internal/theme"
)

// fromimage_test.go covers the pieces the HTML preview's browser screenshot
// mode (#2746) adds: a model over already-decoded pixels, the width-filling
// zoom a tall page opens at, and carrying zoom and pan to a new model.

func TestNewFromImage(t *testing.T) {
	m := NewFromImage("k", "page.html (browser)", image.NewNRGBA(image.Rect(0, 0, 300, 1200)), "png", 4096, theme.DefaultPalette())
	m.SetSize(60, 20)
	m.SetGraphics(true)
	if v := m.View(); !strings.Contains(v, "page.html (browser) · PNG · 300×1200 px") {
		t.Fatalf("footer must name the label and size:\n%s", v)
	}
	if m.ID() == 0 || len(m.SyncSeqs()) == 0 {
		t.Fatal("the model must place its pixels like a decoded file")
	}
	if nilImg := NewFromImage("k", "x", nil, "png", 0, theme.DefaultPalette()); nilImg.err == nil {
		t.Fatal("no pixels must be an error, not a panic")
	}
}

func TestZoomWidthAndViewState(t *testing.T) {
	m := NewFromImage("k", "p", image.NewNRGBA(image.Rect(0, 0, 300, 1200)), "png", 0, theme.DefaultPalette())
	m.SetSize(60, 20)
	m.SetGraphics(true)
	m.ZoomWidth()
	if cols, _ := m.Grid(); cols != 60 || m.AtFit() || m.Crop().Min != (image.Point{}) {
		t.Fatalf("ZoomWidth: cols %d, fit %v, crop %v; want the pane width from the top", cols, m.AtFit(), m.Crop())
	}
	m.Pan(0, 10)
	vs := m.ViewState()
	n := NewFromImage("k", "p", image.NewNRGBA(image.Rect(0, 0, 300, 1300)), "png", 0, theme.DefaultPalette())
	n.SetSize(60, 20)
	n.SetViewState(vs)
	if n.ViewState() != vs {
		t.Fatalf("SetViewState = %+v, want %+v", n.ViewState(), vs)
	}
	// A wide image already fills the width at fit and stays there.
	w := NewFromImage("k", "p", image.NewNRGBA(image.Rect(0, 0, 2000, 100)), "png", 0, theme.DefaultPalette())
	w.SetSize(60, 20)
	w.ZoomWidth()
	if !w.AtFit() {
		t.Fatal("a wide image must stay at fit")
	}
}
