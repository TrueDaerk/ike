package shotpng

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// testFonts loads the embedded Go Mono set: the system fonts differ per
// machine, the embedded one is always there, so the metrics assertions below
// stay stable on any platform.
func testFonts(t *testing.T) *Fonts {
	t.Helper()
	f, err := LoadFonts(FontSpec{Size: 12})
	if err != nil {
		t.Fatalf("LoadFonts: %v", err)
	}
	return f
}

// TestRenderSizeAndDefaults guards the geometry: the image is the cell grid
// plus padding, and untouched cells carry the screen's default background.
func TestRenderSizeAndDefaults(t *testing.T) {
	f := testFonts(t)
	bg := color.RGBA{0x2d, 0x2a, 0x2e, 0xff} // monokai-pro background
	img, err := Render("hi", Options{Cols: 10, Rows: 3, Fonts: f, Padding: 4, Bg: bg, Fg: color.White})
	if err != nil {
		t.Fatal(err)
	}
	wantW, wantH := 8+10*f.CellW, 8+3*f.CellH
	if got := img.Bounds().Dx(); got != wantW {
		t.Errorf("width = %d, want %d", got, wantW)
	}
	if got := img.Bounds().Dy(); got != wantH {
		t.Errorf("height = %d, want %d", got, wantH)
	}
	// The padding corner is never written by a cell, so it must be the default
	// background.
	if got := img.RGBAAt(0, 0); got != bg {
		t.Errorf("padding pixel = %v, want the default background %v", got, bg)
	}
}

// TestRenderCellColors guards the SGR path: a cell's background colour, the
// reverse attribute and true-colour foregrounds must all reach the pixels.
func TestRenderCellColors(t *testing.T) {
	f := testFonts(t)
	red := color.RGBA{0xff, 0x00, 0x00, 0xff}
	// Cell (0,0): red background. Cell (1,0): reverse video on a red
	// foreground, so its background becomes red too.
	frame := "\x1b[48;2;255;0;0m \x1b[0m\x1b[38;2;255;0;0m\x1b[7m \x1b[0m"
	img, err := Render(frame, Options{Cols: 4, Rows: 1, Fonts: f, Bg: color.Black, Fg: color.White})
	if err != nil {
		t.Fatal(err)
	}
	for i, x := range []int{f.CellW / 2, f.CellW + f.CellW/2} {
		if got := img.RGBAAt(x, f.CellH/2); got != red {
			t.Errorf("cell %d pixel = %v, want %v", i, got, red)
		}
	}
	// A cell nobody painted keeps the default background.
	if got := img.RGBAAt(3*f.CellW, f.CellH/2); got != (color.RGBA{0, 0, 0, 0xff}) {
		t.Errorf("untouched cell = %v, want black", got)
	}
}

// TestRenderRegionCrops guards the crop path: a region renders only its cells,
// at the size of the region.
func TestRenderRegionCrops(t *testing.T) {
	f := testFonts(t)
	// A green cell at column 2, everything else default.
	frame := "  \x1b[48;2;0;255;0m \x1b[0m"
	img, err := Render(frame, Options{
		Cols: 6, Rows: 2, Fonts: f, Bg: color.Black,
		Region: image.Rect(2, 0, 4, 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := img.Bounds().Dx(), 2*f.CellW; got != want {
		t.Errorf("cropped width = %d, want %d", got, want)
	}
	if got, want := img.Bounds().Dy(), f.CellH; got != want {
		t.Errorf("cropped height = %d, want %d", got, want)
	}
	// The green cell is now the crop's first cell.
	if got, want := img.RGBAAt(f.CellW/2, f.CellH/2), (color.RGBA{0, 0xff, 0, 0xff}); got != want {
		t.Errorf("first cell of the crop = %v, want %v", got, want)
	}
}

// TestRenderBoxCharactersAreSeamless guards the procedural border painting: a
// vertical light line must be continuous across the row boundary, which is
// exactly what font glyphs fail at.
func TestRenderBoxCharactersAreSeamless(t *testing.T) {
	f := testFonts(t)
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	img, err := Render("│\n│", Options{Cols: 1, Rows: 2, Fonts: f, Fg: white, Bg: color.Black})
	if err != nil {
		t.Fatal(err)
	}
	x := f.CellW / 2
	for y := 0; y < 2*f.CellH; y++ {
		if img.RGBAAt(x, y) != white {
			t.Fatalf("gap in the vertical line at y=%d", y)
		}
	}
}

// TestRenderRejectsBadSize keeps a zero-sized frame from producing an empty
// PNG that would silently land in the docs.
func TestRenderRejectsBadSize(t *testing.T) {
	if _, err := Render("x", Options{Cols: 0, Rows: 5}); err == nil {
		t.Fatal("want an error for a zero-column frame")
	}
}

// TestWriteFileEncodesPNG guards the output path end to end.
func TestWriteFileEncodesPNG(t *testing.T) {
	f := testFonts(t)
	img, err := Render("ok", Options{Cols: 4, Rows: 1, Fonts: f})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "nested", "shot.png")
	if err := WriteFile(path, img); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding the written file: %v", err)
	}
	if decoded.Bounds() != img.Bounds() {
		t.Errorf("decoded bounds = %v, want %v", decoded.Bounds(), img.Bounds())
	}
}
