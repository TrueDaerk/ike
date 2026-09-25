package htmlrender

import (
	"image/color"
	"strconv"
	"strings"

	"ike/internal/theme"
)

// Text attributes a style can carry, as bits of style.attrs.
const (
	attrBold uint8 = 1 << iota
	attrFaint
	attrItalic
	attrUnderline
	attrStrike
)

// style is one run's look. It is a small comparable value so adjacent runs
// with the same look merge and each distinct look's SGR sequence is built
// once. Colours are packed 0xRRGGBB with bit 24 set; zero means "terminal
// default".
type style struct {
	fg, bg uint32
	attrs  uint8
}

// rgb packs a palette colour for style; a nil colour stays unset.
func rgb(c color.Color) uint32 {
	if c == nil {
		return 0
	}
	r, g, b, _ := c.RGBA()
	return 1<<24 | (r>>8)<<16 | (g>>8)<<8 | b>>8
}

// with returns s with the attribute bits in a added.
func (s style) with(a uint8) style {
	s.attrs |= a
	return s
}

// sgr is the escape sequence that switches the terminal to s ("" for the
// default look).
func (s style) sgr() string {
	if s == (style{}) {
		return ""
	}
	var p []string
	for _, a := range []struct {
		bit  uint8
		code string
	}{{attrBold, "1"}, {attrFaint, "2"}, {attrItalic, "3"}, {attrUnderline, "4"}, {attrStrike, "9"}} {
		if s.attrs&a.bit != 0 {
			p = append(p, a.code)
		}
	}
	if s.fg != 0 {
		p = append(p, "38;2;"+rgbParams(s.fg))
	}
	if s.bg != 0 {
		p = append(p, "48;2;"+rgbParams(s.bg))
	}
	return "\x1b[" + strings.Join(p, ";") + "m"
}

func rgbParams(c uint32) string {
	return strconv.Itoa(int(c>>16&0xff)) + ";" + strconv.Itoa(int(c>>8&0xff)) + ";" + strconv.Itoa(int(c&0xff))
}

// sgrReset returns the terminal to its default look after a styled run.
const sgrReset = "\x1b[0m"

// styles is the renderer's palette-derived look for every element family. It
// follows the markdown preview's glamour styling (internal/preview) so the two
// previews read as one system: accent headings (h1 as an accent block, lower
// levels behind "##" markers), accent link labels, inline code on a surface
// background, blockquotes behind a "│" bar.
type styles struct {
	h1, heading       style
	link              style
	code, pre         style
	mark              style
	rule, quote, edge style // hr, blockquote bar, pre overflow marker
	image, caption    style
	sep, summary      style // table borders and separators, details marker
}

// newStyles derives the element looks from pal (nil: the default theme).
func newStyles(pal *theme.Palette) styles {
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	accent, border := rgb(pal.Accent), rgb(pal.Border)
	return styles{
		h1:      style{fg: rgb(pal.Background), bg: accent, attrs: attrBold},
		heading: style{fg: accent, attrs: attrBold},
		link:    style{fg: accent, attrs: attrUnderline},
		code:    style{fg: rgb(pal.Warning), bg: rgb(pal.Surface)},
		pre:     style{fg: rgb(pal.Foreground)},
		mark:    style{fg: rgb(pal.Background), bg: rgb(pal.Warning)},
		rule:    style{fg: border},
		quote:   style{fg: border},
		edge:    style{fg: border},
		image:   style{fg: rgb(pal.Hint), attrs: attrItalic},
		caption: style{attrs: attrItalic | attrFaint},
		sep:     style{fg: border},
		summary: style{fg: accent, attrs: attrBold},
	}
}
