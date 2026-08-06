// Command gen renders IKE's application icon source (#1608):
// deploy/icon/ike-1024.png — a black egg-shaped mark centered on a white
// rounded tile. Pure stdlib, deterministic; the installer's platform icon
// sets (.icns, hicolor PNGs) derive from this file via `make icons`
// (macOS: sips + iconutil).
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
	bg  = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF} // white tile
	egg = color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xFF} // black mark
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
			if inEgg(x, y) {
				img.SetNRGBA(x, y, egg)
			} else {
				img.SetNRGBA(x, y, bg)
			}
		}
	}
	return img
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
