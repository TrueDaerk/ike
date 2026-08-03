package theme

import (
	"image/color"
	"math"
)

// IsDarkColor classifies a terminal background colour (the OSC 11 reply,
// #1480) as dark via its relative luminance: below 0.5 counts as dark. The
// linearised Rec. 709 weighting keeps mid-greys and tinted backgrounds on the
// side a human would sort them.
func IsDarkColor(c color.Color) bool {
	if c == nil {
		return true
	}
	r, g, b, _ := c.RGBA()
	lum := 0.2126*linearise(r) + 0.7152*linearise(g) + 0.0722*linearise(b)
	return lum < 0.5
}

// linearise converts a 16-bit sRGB channel to its linear-light value.
func linearise(v uint32) float64 {
	f := float64(v) / 0xffff
	if f <= 0.04045 {
		return f / 12.92
	}
	return math.Pow((f+0.055)/1.055, 2.4)
}
