package editor

import (
	"ike/internal/highlight"
	"ike/internal/idcolor"
)

// longline.go bounds the per-line decoration scans on very long lines
// (#2734). The colour swatches (#790), the identifier colours (#1626) and the
// hyperlinks (#1655) each scan the *whole* line they decorate, on every
// render epoch — which is every key. On an ordinary line that is nothing; on
// a minified file whose lines run to hundreds of kilobytes it was the whole
// frame: the swatch regex alone took ~100 ms per line, so every cursor move
// on a 4.5 MB single-line HTML file cost more than the render budget.
//
// A line longer than longLineRunes is decorated only around the rendered
// window: the scans cover the columns the span shows plus one window width of
// margin on either side, and their results shift back into line columns. A
// literal cut by the margin edge is missed, which on a line that long is a
// fair price for a frame that stays flat.

// longLineRunes is the line length past which decoration scans are windowed.
const longLineRunes = 4096

// decorWindow returns the rune range [lo, hi) the decoration scans cover for
// a span rendering columns [from, to) (to < 0: through the line end) of a
// line n runes long: the whole line while it is short, else the span plus a
// margin of one span width on either side.
func decorWindow(n, from, to, width int) (lo, hi int) {
	if n <= longLineRunes {
		return 0, n
	}
	if to < 0 || to > n {
		to = from + width
		if to > n {
			to = n
		}
	}
	lo, hi = from-width, to+width
	if lo < 0 {
		lo = 0
	}
	if hi > n {
		hi = n
	}
	if lo > hi {
		lo = hi
	}
	return lo, hi
}

// windowedColorSwatches is lineColorSwatches over the decoration window.
func (m Model) windowedColorSwatches(line int, runes []rune, lo, hi int) []colorSpan {
	if !m.colorPreview {
		return nil
	}
	if lo == 0 && hi == len(runes) {
		return m.lineColorSwatches(line)
	}
	out := parseColorLiterals(string(runes[lo:hi]), m.colorPolicy())
	for i := range out {
		out[i].Start += lo
		out[i].End += lo
	}
	return out
}

// windowedIDColors is lineIDColors over the decoration window.
func (m Model) windowedIDColors(line int, runes []rune, lo, hi int) []idcolor.Span {
	if lo == 0 && hi == len(runes) {
		return m.lineIDColors(line)
	}
	if !m.idColors || !idColorLangs[highlight.Lang(m.langPath())] {
		return nil
	}
	out := idcolor.Scan(string(runes[lo:hi]), m.idColorMin)
	for i := range out {
		out[i].Start += lo
		out[i].End += lo
	}
	return out
}

// windowedLinks is lineLinks over the decoration window.
func (m Model) windowedLinks(runes []rune, lo, hi int) []linkSpan {
	if lo == 0 && hi == len(runes) {
		return m.lineLinks(runes)
	}
	if !m.hyperlinks {
		return nil
	}
	out := scanLinks(runes[lo:hi])
	for i := range out {
		out[i].start += lo
		out[i].end += lo
	}
	return out
}
