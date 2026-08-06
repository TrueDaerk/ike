package shotpng

import (
	"image"
	"image/color"
	"image/draw"
)

// Box-drawing and block characters are painted procedurally instead of taken
// from the font. A font glyph is designed for the font's own line box, not for
// the taller cell a screenshot uses, so borders drawn from glyphs show hairline
// gaps between rows. Painted rectangles meet exactly, which is what a terminal
// shows and what makes a pane border in a screenshot look like a border.

// Stem weights of one box-drawing character, per direction.
const (
	stemNone = iota
	stemLight
	stemHeavy
	stemDouble
)

// Directions of a stem table entry.
const (
	dirUp = iota
	dirRight
	dirDown
	dirLeft
)

// boxStems maps a line-drawing rune to its four stems (up, right, down, left).
// It covers the subset IKE draws: light and heavy lines, the light corners
// including the rounded ones, and the double-line set.
var boxStems = map[rune][4]int{
	// Light lines, corners, tees and cross.
	'─': {stemNone, stemLight, stemNone, stemLight},
	'│': {stemLight, stemNone, stemLight, stemNone},
	'┌': {stemNone, stemLight, stemLight, stemNone},
	'┐': {stemNone, stemNone, stemLight, stemLight},
	'└': {stemLight, stemLight, stemNone, stemNone},
	'┘': {stemLight, stemNone, stemNone, stemLight},
	'├': {stemLight, stemLight, stemLight, stemNone},
	'┤': {stemLight, stemNone, stemLight, stemLight},
	'┬': {stemNone, stemLight, stemLight, stemLight},
	'┴': {stemLight, stemLight, stemNone, stemLight},
	'┼': {stemLight, stemLight, stemLight, stemLight},
	// Rounded corners are drawn as square ones: at a 15px cell the arc is a
	// single pixel of difference, and a mitred join keeps borders seamless.
	'╭': {stemNone, stemLight, stemLight, stemNone},
	'╮': {stemNone, stemNone, stemLight, stemLight},
	'╰': {stemLight, stemLight, stemNone, stemNone},
	'╯': {stemLight, stemNone, stemNone, stemLight},
	// Heavy.
	'━': {stemNone, stemHeavy, stemNone, stemHeavy},
	'┃': {stemHeavy, stemNone, stemHeavy, stemNone},
	'┏': {stemNone, stemHeavy, stemHeavy, stemNone},
	'┓': {stemNone, stemNone, stemHeavy, stemHeavy},
	'┗': {stemHeavy, stemHeavy, stemNone, stemNone},
	'┛': {stemHeavy, stemNone, stemNone, stemHeavy},
	'┣': {stemHeavy, stemHeavy, stemHeavy, stemNone},
	'┫': {stemHeavy, stemNone, stemHeavy, stemHeavy},
	'┳': {stemNone, stemHeavy, stemHeavy, stemHeavy},
	'┻': {stemHeavy, stemHeavy, stemNone, stemHeavy},
	'╋': {stemHeavy, stemHeavy, stemHeavy, stemHeavy},
	// Double.
	'═': {stemNone, stemDouble, stemNone, stemDouble},
	'║': {stemDouble, stemNone, stemDouble, stemNone},
	'╔': {stemNone, stemDouble, stemDouble, stemNone},
	'╗': {stemNone, stemNone, stemDouble, stemDouble},
	'╚': {stemDouble, stemDouble, stemNone, stemNone},
	'╝': {stemDouble, stemNone, stemNone, stemDouble},
	'╠': {stemDouble, stemDouble, stemDouble, stemNone},
	'╣': {stemDouble, stemNone, stemDouble, stemDouble},
	'╦': {stemNone, stemDouble, stemDouble, stemDouble},
	'╩': {stemDouble, stemDouble, stemNone, stemDouble},
	'╬': {stemDouble, stemDouble, stemDouble, stemDouble},
}

// blockFills maps a block-element rune to the fraction of the cell it covers,
// as left/top/right/bottom in the 0..1 range.
var blockFills = map[rune][4]float64{
	'█': {0, 0, 1, 1},
	'▀': {0, 0, 1, 0.5},
	'▄': {0, 0.5, 1, 1},
	'▌': {0, 0, 0.5, 1},
	'▐': {0.5, 0, 1, 1},
	'▁': {0, 7.0 / 8, 1, 1},
	'▂': {0, 6.0 / 8, 1, 1},
	'▃': {0, 5.0 / 8, 1, 1},
	'▅': {0, 3.0 / 8, 1, 1},
	'▆': {0, 2.0 / 8, 1, 1},
	'▇': {0, 1.0 / 8, 1, 1},
	'▔': {0, 0, 1, 1.0 / 8},
	'▉': {0, 0, 7.0 / 8, 1},
	'▊': {0, 0, 6.0 / 8, 1},
	'▋': {0, 0, 5.0 / 8, 1},
	'▍': {0, 0, 3.0 / 8, 1},
	'▎': {0, 0, 2.0 / 8, 1},
	'▏': {0, 0, 1.0 / 8, 1},
	'▕': {7.0 / 8, 0, 1, 1},
}

// shades maps the shade blocks to their coverage of the foreground colour.
var shades = map[rune]float64{'░': 0.25, '▒': 0.5, '▓': 0.75}

// drawCellRune paints a box-drawing, block or shade character into the cell
// rectangle and reports whether it handled the rune. Anything else is left to
// the font.
func drawCellRune(dst *image.RGBA, cell image.Rectangle, r rune, fg, bg color.RGBA) bool {
	if stems, ok := boxStems[r]; ok {
		drawStems(dst, cell, stems, fg)
		return true
	}
	if f, ok := blockFills[r]; ok {
		w, h := cell.Dx(), cell.Dy()
		rect := image.Rect(
			cell.Min.X+int(f[0]*float64(w)+0.5),
			cell.Min.Y+int(f[1]*float64(h)+0.5),
			cell.Min.X+int(f[2]*float64(w)+0.5),
			cell.Min.Y+int(f[3]*float64(h)+0.5),
		)
		fill(dst, rect, fg)
		return true
	}
	if a, ok := shades[r]; ok {
		fill(dst, cell, blend(bg, fg, a))
		return true
	}
	return false
}

// drawStems paints the four stems of a line-drawing character. Every stem runs
// from the cell edge to the centre line of the opposite axis, so neighbouring
// cells join without a gap.
func drawStems(dst *image.RGBA, cell image.Rectangle, stems [4]int, fg color.RGBA) {
	w, h := cell.Dx(), cell.Dy()
	light := max(1, (h+13)/14)
	gap := max(1, light)  // spacing between the two strokes of a double line
	cx := cell.Min.X + w/2
	cy := cell.Min.Y + h/2

	// strokes returns the perpendicular offsets and thickness for a weight.
	strokes := func(weight int) ([]int, int) {
		switch weight {
		case stemHeavy:
			return []int{0}, light * 2
		case stemDouble:
			return []int{-(gap + light), gap}, light
		default:
			return []int{0}, light
		}
	}
	for dir, weight := range stems {
		if weight == stemNone {
			continue
		}
		offsets, t := strokes(weight)
		for _, off := range offsets {
			switch dir {
			case dirUp:
				fill(dst, image.Rect(cx-t/2+off, cell.Min.Y, cx-t/2+off+t, cy+t/2), fg)
			case dirDown:
				fill(dst, image.Rect(cx-t/2+off, cy-t/2, cx-t/2+off+t, cell.Max.Y), fg)
			case dirLeft:
				fill(dst, image.Rect(cell.Min.X, cy-t/2+off, cx+t/2, cy-t/2+off+t), fg)
			case dirRight:
				fill(dst, image.Rect(cx-t/2, cy-t/2+off, cell.Max.X, cy-t/2+off+t), fg)
			}
		}
	}
}

// fill paints a solid rectangle, clipped to the image.
func fill(dst *image.RGBA, r image.Rectangle, c color.RGBA) {
	r = r.Intersect(dst.Bounds())
	if r.Empty() {
		return
	}
	draw.Draw(dst, r, image.NewUniform(c), image.Point{}, draw.Src)
}

// blend mixes fg over bg with the given alpha.
func blend(bg, fg color.RGBA, alpha float64) color.RGBA {
	mix := func(b, f uint8) uint8 {
		return uint8(float64(b)*(1-alpha) + float64(f)*alpha + 0.5)
	}
	return color.RGBA{mix(bg.R, fg.R), mix(bg.G, fg.G), mix(bg.B, fg.B), 255}
}
