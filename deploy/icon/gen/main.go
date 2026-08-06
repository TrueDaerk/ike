// Command gen renders IKE's application icon source (#1610):
// deploy/icon/ike-1024.png — an egg-shaped mascot on a white rounded tile:
// beige upper section with two black oval eyes, teal lower section, and a
// slanted white gap between the two. Pure stdlib, deterministic; the
// installer's platform icon sets (.icns, hicolor PNGs) derive from this
// file via `make icons` (macOS: sips + iconutil).
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

const (
	size  = 1024
	ss    = 4 // supersampling factor for cheap anti-aliasing
	world = size * ss
)

var (
	bg    = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF} // white tile
	shell = color.NRGBA{R: 0xDD, G: 0xC1, B: 0xA9, A: 0xFF} // beige upper section
	body  = color.NRGBA{R: 0x6B, G: 0xAA, B: 0xB2, A: 0xFF} // teal lower section
	eye   = color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xFF} // black eyes
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
			if !inRoundedRect(x, y, margin, world-margin, radius) {
				continue
			}
			img.SetNRGBA(x, y, eggColor(x, y))
		}
	}
	return img
}

// eggColor picks the color for a tile pixel: white outside the egg, and
// inside it a beige upper section (with the eyes), a teal lower section,
// and the white slanted gap that separates the two.
func eggColor(x, y int) color.NRGBA {
	if !inEgg(x, y) {
		return bg
	}
	if inEyes(x, y) {
		return eye
	}
	// The beige section's lower edge slants upward to the right; the teal
	// section starts on a horizontal line below it, leaving a white gap.
	beigeBottom := 0.575*world - 0.06*float64(x)
	const tealTop = world * 60 / 100
	switch {
	case float64(y) < beigeBottom:
		return shell
	case y >= tealTop:
		return body
	default:
		return bg
	}
}

// inEyes reports whether (x, y) lies inside one of the two vertical oval
// eyes in the beige section; the right eye sits slightly lower.
func inEyes(x, y int) bool {
	const (
		rx, ry = world * 16 / 1000, world * 30 / 1000 // eye half-axes
	)
	for _, e := range [2]struct{ cx, cy float64 }{
		{world * 44 / 100, world * 41 / 100},
		{world * 56 / 100, world * 43 / 100},
	} {
		dx, dy := (float64(x)-e.cx)/rx, (float64(y)-e.cy)/ry
		if dx*dx+dy*dy <= 1 {
			return true
		}
	}
	return false
}

// inEgg reports whether (x, y) lies inside the egg mark: a vertical oval
// whose half-width shrinks toward the top, so the blunt end sits at the
// bottom and the narrow end at the top.
func inEgg(x, y int) bool {
	const (
		cx, cy = world * 50 / 100, world * 50 / 100 // egg center
		b      = world * 30 / 100                   // vertical half-axis
		a      = world * 21 / 100                   // horizontal half-axis
		taper  = 0.18                               // top/bottom asymmetry
	)
	t := (float64(y) - cy) / b // -1 at the top tip, +1 at the bottom
	if t < -1 || t > 1 {
		return false
	}
	halfW := a * math.Sqrt(1-t*t) * (1 + taper*t)
	dx := float64(x) - cx
	return dx*dx <= halfW*halfW
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
