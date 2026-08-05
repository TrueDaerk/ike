// Command gen renders IKE's application icon source (#1567):
// deploy/icon/ike-1024.png — a dark rounded square with a terminal-prompt
// glyph (chevron + underscore). Pure stdlib, deterministic; the installer's
// platform icon sets (.icns, hicolor PNGs) derive from this file via
// `make icons` (macOS: sips + iconutil).
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
)

const (
	size  = 1024
	ss    = 4 // supersampling factor for cheap anti-aliasing
	world = size * ss
)

var (
	bg      = color.NRGBA{R: 0x24, G: 0x27, B: 0x3A, A: 0xFF} // dark slate
	chevron = color.NRGBA{R: 0x8A, G: 0xAD, B: 0xF4, A: 0xFF} // blue
	cursor  = color.NRGBA{R: 0xA6, G: 0xDA, B: 0x95, A: 0xFF} // green
)

func main() {
	img := render()
	out, err := os.Create("deploy/icon/ike-1024.png")
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	if err := png.Encode(out, downsample(img)); err != nil {
		log.Fatal(err)
	}
}

// render paints the icon at supersampled resolution.
func render() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, world, world))
	const (
		margin = world * 6 / 100  // transparent border around the tile
		radius = world * 22 / 100 // macOS-like corner rounding
	)
	for y := 0; y < world; y++ {
		for x := 0; x < world; x++ {
			if inRoundedRect(x, y, margin, world-margin, radius) {
				img.SetNRGBA(x, y, bg)
			}
		}
	}
	// Prompt glyph: a chevron `>` on the left, a cursor underscore to its
	// right, both sized relative to the tile.
	stroke := world * 9 / 100
	cx, cy := world*32/100, world*50/100
	arm := world * 17 / 100
	drawBar(img, cx-arm, cy-arm, cx, cy, stroke, chevron) // upper arm
	drawBar(img, cx-arm, cy+arm, cx, cy, stroke, chevron) // lower arm
	ux0, ux1 := world*44/100, world*72/100                // underscore span
	uy := world * 62 / 100                                // underscore baseline
	fillRect(img, ux0, uy, ux1, uy+stroke, cursor)        // cursor block
	return img
}

// inRoundedRect reports whether (x, y) lies inside the square spanning
// [lo, hi) with the given corner radius.
func inRoundedRect(x, y, lo, hi, r int) bool {
	if x < lo || y < lo || x >= hi || y >= hi {
		return false
	}
	// Corner circles: check the four corner squares against the radius.
	cx, cy := x, y
	switch {
	case x < lo+r && y < lo+r:
		cx, cy = lo+r, lo+r
	case x >= hi-r && y < lo+r:
		cx, cy = hi-r-1, lo+r
	case x < lo+r && y >= hi-r:
		cx, cy = lo+r, hi-r-1
	case x >= hi-r && y >= hi-r:
		cx, cy = hi-r-1, hi-r-1
	default:
		return true
	}
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

// drawBar fills a thick line from (x0, y0) to (x1, y1) by stamping squares
// along it — crude, but supersampling smooths the result.
func drawBar(img *image.NRGBA, x0, y0, x1, y1, w int, c color.NRGBA) {
	steps := max(abs(x1-x0), abs(y1-y0))
	for i := 0; i <= steps; i++ {
		x := x0 + (x1-x0)*i/steps
		y := y0 + (y1-y0)*i/steps
		fillRect(img, x-w/2, y-w/2, x+w/2, y+w/2, c)
	}
}

// fillRect fills [x0, x1) x [y0, y1) clipped to the image.
func fillRect(img *image.NRGBA, x0, y0, x1, y1 int, c color.NRGBA) {
	b := img.Bounds()
	for y := max(y0, b.Min.Y); y < min(y1, b.Max.Y); y++ {
		for x := max(x0, b.Min.X); x < min(x1, b.Max.X); x++ {
			img.SetNRGBA(x, y, c)
		}
	}
}

// downsample box-filters the supersampled image to the final size.
func downsample(src *image.NRGBA) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a int
			for dy := 0; dy < ss; dy++ {
				for dx := 0; dx < ss; dx++ {
					p := src.NRGBAAt(x*ss+dx, y*ss+dy)
					r += int(p.R) * int(p.A)
					g += int(p.G) * int(p.A)
					b += int(p.B) * int(p.A)
					a += int(p.A)
				}
			}
			if a == 0 {
				continue
			}
			dst.SetNRGBA(x, y, color.NRGBA{
				R: uint8(r / a), G: uint8(g / a), B: uint8(b / a),
				A: uint8(a / (ss * ss)),
			})
		}
	}
	return dst
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
