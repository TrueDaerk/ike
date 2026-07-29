package terminal

import (
	"image/color"
	"strconv"
	"strings"

	"ike/internal/theme"
)

// ansipalette.go maps the grid's indexed colors onto the active theme (#1363).
// The emulator keeps a cell's color exactly as the program set it, so a shell
// painting with SGR 30–37 / 90–97 (or a 256-color index below 16) renders in
// the *outer* terminal's palette while its background comes from IKE's own
// theme wash — a combination that regularly lands unreadable. Every rendered
// line therefore passes through gridPalette.remap, which rewrites those
// parameters into the theme's truecolor values before the line reaches the
// screen. Indexes 16–255 are the fixed xterm cube and pass through untouched.

// gridPalette holds the active theme's terminal colors pre-rendered as SGR
// parameter strings ("r;g;b"), so remap never formats a color per cell.
type gridPalette struct {
	ansi [theme.ANSICount]string
	fg   string // terminal default foreground, for SGR 39
	bg   string // terminal default background, for SGR 49
}

// newGridPalette pre-renders p's terminal colors. A nil palette yields nil —
// remap is then a no-op and the grid renders exactly as before.
func newGridPalette(p *theme.Palette) *gridPalette {
	if p == nil {
		return nil
	}
	g := &gridPalette{fg: rgbParams(p.TerminalFg), bg: rgbParams(p.TerminalBg)}
	for i, c := range p.ANSI {
		g.ansi[i] = rgbParams(c)
	}
	return g
}

// rgbParams renders c as the "r;g;b" tail of an SGR 38;2 / 48;2 sequence.
// A nil color yields "", which remap treats as "leave this parameter alone".
func rgbParams(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return strconv.Itoa(int(r>>8)) + ";" + strconv.Itoa(int(g>>8)) + ";" + strconv.Itoa(int(b>>8))
}

// remap rewrites the indexed color parameters of every SGR sequence in s.
// Non-SGR escape sequences (cursor moves, hyperlinks) and plain text are
// copied verbatim.
func (g *gridPalette) remap(s string) string {
	if g == nil || !strings.Contains(s, "\x1b[") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + len(s)/4)
	for i := 0; i < len(s); {
		start := strings.Index(s[i:], "\x1b[")
		if start < 0 {
			b.WriteString(s[i:])
			break
		}
		start += i
		b.WriteString(s[i:start])
		end := sgrEnd(s, start+2)
		if end < 0 { // unterminated: nothing left to rewrite
			b.WriteString(s[start:])
			break
		}
		if s[end] == 'm' {
			b.WriteString("\x1b[")
			b.WriteString(g.remapParams(s[start+2 : end]))
			b.WriteByte('m')
		} else {
			b.WriteString(s[start : end+1])
		}
		i = end + 1
	}
	return b.String()
}

// sgrEnd returns the index of the CSI sequence's final byte (0x40–0x7e)
// starting the scan at i, or -1 when the sequence is unterminated.
func sgrEnd(s string, i int) int {
	for ; i < len(s); i++ {
		if s[i] >= 0x40 && s[i] <= 0x7e {
			return i
		}
	}
	return -1
}

// remapParams rewrites the parameter list of one SGR sequence. The grid is
// rendered by ultraviolet, which only ever emits ';'-separated parameters, so
// a parameter carrying ':' subparameters comes from somewhere else (a program
// writing through, a decorated line) and is passed through untouched.
func (g *gridPalette) remapParams(params string) string {
	if params == "" {
		return params // "\x1b[m" — a reset, nothing to map
	}
	fields := strings.Split(params, ";")
	out := make([]string, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if strings.ContainsRune(f, ':') {
			out = append(out, f)
			continue
		}
		n, err := strconv.Atoi(f)
		if err != nil {
			out = append(out, f)
			continue
		}
		switch {
		case n >= 30 && n <= 37:
			out = append(out, g.fgParams(n-30, f)...)
		case n >= 90 && n <= 97:
			out = append(out, g.fgParams(n-90+8, f)...)
		case n >= 40 && n <= 47:
			out = append(out, g.bgParams(n-40, f)...)
		case n >= 100 && n <= 107:
			out = append(out, g.bgParams(n-100+8, f)...)
		case n == 39:
			out = append(out, expandDefault(g.fg, "38", f)...)
		case n == 49:
			out = append(out, expandDefault(g.bg, "48", f)...)
		case n == 38 || n == 48 || n == 58:
			consumed, mapped := g.remapExtended(fields[i:])
			out = append(out, mapped...)
			i += consumed - 1
		default:
			out = append(out, f)
		}
	}
	return strings.Join(out, ";")
}

// fgParams renders palette index i as a foreground parameter triplet, falling
// back to the original parameter when the palette has no color for it.
func (g *gridPalette) fgParams(i int, orig string) []string {
	return expandDefault(g.ansi[i], "38", orig)
}

// bgParams is fgParams for the background.
func (g *gridPalette) bgParams(i int, orig string) []string {
	return expandDefault(g.ansi[i], "48", orig)
}

// expandDefault turns pre-rendered "r;g;b" params into a truecolor SGR
// parameter run under kind ("38" or "48"); an empty rgb keeps orig.
func expandDefault(rgb, kind, orig string) []string {
	if rgb == "" {
		return []string{orig}
	}
	return append([]string{kind, "2"}, strings.Split(rgb, ";")...)
}

// remapExtended handles a 38/48/58 parameter run. It returns how many
// parameters the run consumed and its replacement. Only the 5;n (indexed)
// form with n below 16 is rewritten — the 256-color cube above it and the
// 2;r;g;b truecolor form already say exactly what they mean.
func (g *gridPalette) remapExtended(fields []string) (int, []string) {
	kind := fields[0]
	if len(fields) < 3 {
		return len(fields), fields
	}
	switch fields[1] {
	case "5":
		n, err := strconv.Atoi(fields[2])
		if err != nil || n < 0 || n >= theme.ANSICount || kind == "58" || g.ansi[n] == "" {
			return 3, fields[:3]
		}
		return 3, expandDefault(g.ansi[n], kind, fields[2])
	case "2":
		if len(fields) < 5 {
			return len(fields), fields
		}
		return 5, fields[:5]
	}
	return 2, fields[:2]
}
