package editor

import (
	"regexp"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// colorswatch.go is the inline color preview (#790): recognized color
// literals — #rrggbb, #rgb, rgb()/rgba(), hsl()/hsla() — render with the
// literal's own color as the cell background and a black/white contrast
// foreground picked by luminance. The tint approach (rather than extra ██
// swatch cells) is deliberate: it adds no display columns, so motions, mouse
// clicks, soft wrap and the #881 conceal mapping are untouched, and the color
// is visible at the width of the literal itself. Detection runs per rendered
// line inside the (line-cached) render path — only visible lines are ever
// scanned, so large files cost nothing extra. Toggle: editor.color_preview.

// colorSpan is one detected literal: [Start, End) rune columns and its color.
type colorSpan struct {
	Start, End int
	R, G, B    uint8
}

var (
	hexColorRe  = regexp.MustCompile(`#(?:[0-9a-fA-F]{6}|[0-9a-fA-F]{3})\b`)
	funcColorRe = regexp.MustCompile(`(?i)\b(rgba?|hsla?)\(([^)]*)\)`)
)

// parseColorLiterals scans one line for color literals. Invalid values (out
// of range, wrong arity) yield no span — better a missed swatch than a wrong
// color.
func parseColorLiterals(line string) []colorSpan {
	var out []colorSpan
	for _, loc := range hexColorRe.FindAllStringIndex(line, -1) {
		hex := line[loc[0]+1 : loc[1]]
		var r, g, b uint8
		switch len(hex) {
		case 6:
			r, g, b = hexByte(hex[0:2]), hexByte(hex[2:4]), hexByte(hex[4:6])
		case 3:
			r, g, b = hexNibble(hex[0]), hexNibble(hex[1]), hexNibble(hex[2])
		}
		out = append(out, colorSpan{
			Start: len([]rune(line[:loc[0]])),
			End:   len([]rune(line[:loc[1]])),
			R:     r, G: g, B: b,
		})
	}
	for _, loc := range funcColorRe.FindAllStringSubmatchIndex(line, -1) {
		fn := strings.ToLower(line[loc[2]:loc[3]])
		args := line[loc[4]:loc[5]]
		var r, g, b uint8
		var ok bool
		switch fn {
		case "rgb", "rgba":
			r, g, b, ok = parseRGBArgs(args)
		case "hsl", "hsla":
			r, g, b, ok = parseHSLArgs(args)
		}
		if !ok {
			continue
		}
		out = append(out, colorSpan{
			Start: len([]rune(line[:loc[0]])),
			End:   len([]rune(line[:loc[1]])),
			R:     r, G: g, B: b,
		})
	}
	return out
}

func hexByte(s string) uint8 {
	v, _ := strconv.ParseUint(s, 16, 8)
	return uint8(v)
}

func hexNibble(c byte) uint8 {
	v, _ := strconv.ParseUint(string(c), 16, 8)
	return uint8(v*16 + v)
}

// parseRGBArgs parses "255, 0, 100" / "100%, 0%, 50%" (an optional fourth
// alpha component is accepted and ignored — the tint has no alpha channel).
func parseRGBArgs(args string) (r, g, b uint8, ok bool) {
	parts := splitColorArgs(args)
	if len(parts) != 3 && len(parts) != 4 {
		return 0, 0, 0, false
	}
	var vals [3]uint8
	for i := 0; i < 3; i++ {
		p := parts[i]
		if strings.HasSuffix(p, "%") {
			f, err := strconv.ParseFloat(strings.TrimSuffix(p, "%"), 64)
			if err != nil || f < 0 || f > 100 {
				return 0, 0, 0, false
			}
			vals[i] = uint8(f*255/100 + 0.5)
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return 0, 0, 0, false
		}
		vals[i] = uint8(n)
	}
	return vals[0], vals[1], vals[2], true
}

// parseHSLArgs parses "120, 50%, 50%" (optional ignored alpha) and converts
// to RGB.
func parseHSLArgs(args string) (r, g, b uint8, ok bool) {
	parts := splitColorArgs(args)
	if len(parts) != 3 && len(parts) != 4 {
		return 0, 0, 0, false
	}
	h, err := strconv.ParseFloat(strings.TrimSuffix(parts[0], "deg"), 64)
	if err != nil {
		return 0, 0, 0, false
	}
	s, err := parsePercent(parts[1])
	if err != nil {
		return 0, 0, 0, false
	}
	l, err := parsePercent(parts[2])
	if err != nil {
		return 0, 0, 0, false
	}
	h = h - 360*float64(int(h/360))
	if h < 0 {
		h += 360
	}
	c := (1 - abs(2*l-1)) * s
	x := c * (1 - abs(mod2(h/60)-1))
	m := l - c/2
	var rf, gf, bf float64
	switch {
	case h < 60:
		rf, gf, bf = c, x, 0
	case h < 120:
		rf, gf, bf = x, c, 0
	case h < 180:
		rf, gf, bf = 0, c, x
	case h < 240:
		rf, gf, bf = 0, x, c
	case h < 300:
		rf, gf, bf = x, 0, c
	default:
		rf, gf, bf = c, 0, x
	}
	return uint8((rf+m)*255 + 0.5), uint8((gf+m)*255 + 0.5), uint8((bf+m)*255 + 0.5), true
}

func parsePercent(p string) (float64, error) {
	f, err := strconv.ParseFloat(strings.TrimSuffix(p, "%"), 64)
	if err != nil || f < 0 || f > 100 {
		if err == nil {
			err = strconv.ErrRange
		}
		return 0, err
	}
	return f / 100, nil
}

// splitColorArgs splits on commas and slashes (the CSS4 space syntax
// "0 128 255 / 50%" also splits on spaces when no comma is present).
func splitColorArgs(args string) []string {
	args = strings.TrimSpace(args)
	sep := ","
	if !strings.Contains(args, ",") {
		args = strings.ReplaceAll(args, "/", " ")
		return strings.Fields(args)
	}
	var out []string
	for _, p := range strings.Split(args, sep) {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func mod2(f float64) float64 { return f - 2*float64(int(f/2)) }

// lineColorSwatches returns the color spans for one line when the preview is
// enabled; nil otherwise.
func (m Model) lineColorSwatches(line int) []colorSpan {
	if !m.colorPreview {
		return nil
	}
	return parseColorLiterals(m.buf.Line(line))
}

// swatchStyle is the tint for a literal cell: the literal's color as
// background with a black/white foreground picked by luminance, so the text
// stays readable on any color.
func swatchStyle(c colorSpan) lipgloss.Style {
	fg := lipgloss.Color("#ffffff")
	if 299*int(c.R)+587*int(c.G)+114*int(c.B) > 128*1000 {
		fg = lipgloss.Color("#000000")
	}
	return lipgloss.NewStyle().
		Background(lipgloss.Color(rgbHex(c))).
		Foreground(fg)
}

func rgbHex(c colorSpan) string {
	const digits = "0123456789abcdef"
	b := []byte{'#', 0, 0, 0, 0, 0, 0}
	for i, v := range []uint8{c.R, c.G, c.B} {
		b[1+2*i] = digits[v>>4]
		b[2+2*i] = digits[v&0xf]
	}
	return string(b)
}
