package terminal

import (
	"strings"
	"testing"

	"ike/internal/theme"
)

// ansipalette_test.go covers the indexed→truecolor rewrite of the grid
// (#1363): the shell's SGR colors must come out as the theme's values, while
// everything the theme has no opinion about passes through untouched.

// testPalette is a palette with recognisable values per slot: ansi N is
// #0N0N0N-ish, so a remapped parameter is readable in a failure message.
func testPalette() *gridPalette {
	p := &theme.Palette{}
	for i := range p.ANSI {
		p.ANSI[i] = rgb(uint8(i), uint8(i*2), uint8(i*3))
	}
	p.TerminalFg = rgb(200, 201, 202)
	p.TerminalBg = rgb(10, 11, 12)
	return newGridPalette(p)
}

// rgb builds an opaque color from 8-bit components.
func rgb(r, g, b uint8) *rgbColor { return &rgbColor{r, g, b} }

type rgbColor struct{ r, g, b uint8 }

func (c *rgbColor) RGBA() (r, g, b, a uint32) {
	f := func(v uint8) uint32 { return uint32(v)<<8 | uint32(v) }
	return f(c.r), f(c.g), f(c.b), 0xffff
}

func TestRemapBasicColors(t *testing.T) {
	g := testPalette()
	for _, tc := range []struct{ in, want string }{
		// Standard foregrounds and backgrounds.
		{"\x1b[31mx", "\x1b[38;2;1;2;3mx"},
		{"\x1b[41mx", "\x1b[48;2;1;2;3mx"},
		// Bright variants map to indexes 8–15.
		{"\x1b[91mx", "\x1b[38;2;9;18;27mx"},
		{"\x1b[101mx", "\x1b[48;2;9;18;27mx"},
		// 256-color indexes below 16 are the same colors by another spelling.
		{"\x1b[38;5;1mx", "\x1b[38;2;1;2;3mx"},
		{"\x1b[48;5;9mx", "\x1b[48;2;9;18;27mx"},
		// The terminal defaults become the theme's terminal fg/bg.
		{"\x1b[39mx", "\x1b[38;2;200;201;202mx"},
		{"\x1b[49mx", "\x1b[48;2;10;11;12mx"},
		// Attributes travel with the colors and keep their order.
		{"\x1b[1;31;4mx", "\x1b[1;38;2;1;2;3;4mx"},
	} {
		if got := g.remap(tc.in); got != tc.want {
			t.Errorf("remap(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRemapLeavesEverythingElseAlone(t *testing.T) {
	g := testPalette()
	for _, s := range []string{
		"plain text, no escapes",
		"\x1b[0m",                    // reset
		"\x1b[m",                     // implicit reset
		"\x1b[1;4mx",                 // attributes only
		"\x1b[38;5;200mx",            // the 256-color cube above the palette
		"\x1b[48;5;16mx",             // first cube entry, just past the palette
		"\x1b[38;2;1;2;3mx",          // already truecolor
		"\x1b[58;5;3mx",              // underline color, not a text color
		"\x1b[2Kx",                   // a non-SGR CSI sequence
		"\x1b]8;;http://x\x1b\\link", // an OSC hyperlink
		"\x1b[4:3mx",                 // sub-parameters pass through
		"\x1b[31",                    // unterminated: nothing to rewrite
	} {
		if got := g.remap(s); got != s {
			t.Errorf("remap(%q) = %q, want it unchanged", s, got)
		}
	}
}

// TestRemapNilPalette (#1363): before a theme is threaded in (and in every
// test that never sets one) the grid renders exactly as the emulator wrote it.
func TestRemapNilPalette(t *testing.T) {
	var g *gridPalette
	const in = "\x1b[31mred\x1b[0m"
	if got := g.remap(in); got != in {
		t.Errorf("nil palette changed the render: %q", got)
	}
	if newGridPalette(nil) != nil {
		t.Error("newGridPalette(nil) must stay nil")
	}
}

// TestRemapKeepsGridText (#1363): the rewrite touches escape sequences only —
// the visible characters, including wide runes, come through byte-identical.
func TestRemapKeepsGridText(t *testing.T) {
	g := testPalette()
	const in = "\x1b[32mgrün\x1b[0m 世界 \x1b[94mblue\x1b[0m"
	out := g.remap(in)
	strip := func(s string) string {
		var b strings.Builder
		for i := 0; i < len(s); {
			if s[i] == 0x1b {
				for i < len(s) && !(s[i] >= 0x40 && s[i] <= 0x7e && i > 0 && s[i-1] != 0x1b) {
					i++
				}
				i++
				continue
			}
			b.WriteByte(s[i])
			i++
		}
		return b.String()
	}
	if got, want := strip(out), strip(in); got != want {
		t.Errorf("text changed: %q, want %q", got, want)
	}
	if !strings.Contains(out, "38;2;2;4;6") {
		t.Errorf("green was not remapped: %q", out)
	}
}

// TestSessionPaletteRemapsHistory (#1363): the palette reaches the rendered
// grid — a shell painting with SGR 31 comes out as the theme's truecolor red.
func TestSessionPaletteRemapsHistory(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	s.SetPalette(theme.NewPalette(theme.Default()))
	for _, r := range "printf '\\033[31mred\\033[0m\\n'\r" {
		s.SendKey(keyFor(r))
	}
	// Wait for the remapped truecolor sequence itself: plain "red" also
	// appears in the echoed command line, which races ahead of the output.
	waitFor(t, "the coloured output", func() bool {
		return strings.Contains(s.View(), "38;2;")
	})
	view := s.View()
	if strings.Contains(view, "\x1b[31m") {
		t.Errorf("indexed color survived the remap: %q", view)
	}
	if !strings.Contains(view, "38;2;") {
		t.Errorf("no truecolor in the rendered grid: %q", view)
	}
}
