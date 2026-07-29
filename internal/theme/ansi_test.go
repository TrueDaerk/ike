package theme

import (
	"image/color"
	"testing"
)

// ansi_test.go covers the terminal palette (#1363): resolution of a theme's
// own values, the derived fallback, the readability floor every built-in has
// to clear, and the `[theme.terminal]` config overrides.

// hex renders a color as #rrggbb for readable failures.
func hex(c color.Color) string {
	if c == nil {
		return "<nil>"
	}
	r, g, b, _ := c.RGBA()
	const digits = "0123456789abcdef"
	out := []byte("#000000")
	for i, v := range []uint32{r >> 8, g >> 8, b >> 8} {
		out[1+i*2] = digits[(v>>4)&0xf]
		out[2+i*2] = digits[v&0xf]
	}
	return string(out)
}

// TestBuiltinTerminalPalettesReadable (#1363): every built-in theme resolves a
// complete 16-color palette whose entries clear the contrast floor against
// that theme's own terminal background — the readability guarantee the whole
// issue is about.
func TestBuiltinTerminalPalettesReadable(t *testing.T) {
	for _, th := range Builtins() {
		p := NewPalette(th)
		if p.TerminalBg == nil || p.TerminalFg == nil {
			t.Fatalf("%s: terminal fg/bg must resolve", th.Name)
		}
		for i, c := range p.ANSI {
			if c == nil {
				t.Fatalf("%s: ansi %s unresolved", th.Name, ANSIName(i))
			}
			floor := ansiFloor(i)
			if got := ContrastRatio(c, p.TerminalBg); got < floor {
				t.Errorf("%s: ansi %s (%s) contrast %.2f on %s, want >= %.2f",
					th.Name, ANSIName(i), hex(c), got, hex(p.TerminalBg), floor)
			}
		}
	}
}

// TestTerminalPaletteKeepsThemeValues (#1363): a value the theme declares is
// used verbatim as long as it is readable — the palette must not "improve"
// colors a theme deliberately picked.
func TestTerminalPaletteKeepsThemeValues(t *testing.T) {
	th := Theme{
		Name: "x", Dark: true,
		UI:       UI{Background: "#000000", Foreground: "#ffffff"},
		Terminal: ansiSet("#808080", "#ff5555"),
	}
	p := NewPalette(th)
	if got := hex(p.ANSI[ANSIRed]); got != "#ff5555" {
		t.Errorf("declared red = %s, want #ff5555", got)
	}
	if got := hex(p.ANSI[ANSIBlack]); got != "#808080" {
		t.Errorf("declared black = %s, want #808080", got)
	}
}

// TestTerminalPaletteLiftsUnreadableValues (#1363): a declared color that
// would be invisible on the theme's terminal background is lifted until it
// clears the floor, instead of being rendered as mud.
func TestTerminalPaletteLiftsUnreadableValues(t *testing.T) {
	th := Theme{
		Name: "x", Dark: true,
		UI:       UI{Background: "#101010", Foreground: "#e0e0e0"},
		Terminal: ansiSet("", "#150505"), // a red barely off the background
	}
	p := NewPalette(th)
	if got := ContrastRatio(p.ANSI[ANSIRed], p.TerminalBg); got < minTerminalContrast {
		t.Errorf("unreadable red left at contrast %.2f (%s), want >= %.2f",
			got, hex(p.ANSI[ANSIRed]), minTerminalContrast)
	}
}

// TestTerminalPaletteDerivesFromTheme (#1363): a theme with no terminal block
// still gets a full palette, derived from its own semantic colors, on dark and
// light backgrounds alike.
func TestTerminalPaletteDerivesFromTheme(t *testing.T) {
	for _, tc := range []struct {
		name   string
		dark   bool
		bg, fg string
	}{
		{"dark", true, "#0d0d15", "#e6e6f0"},
		{"light", false, "#fdfdfd", "#1c1c1c"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			th := Theme{
				Name: tc.name, Dark: tc.dark,
				UI: UI{
					Background: tc.bg, Foreground: tc.fg,
					Error: "#c04040", Success: "#40a040", Warning: "#c09030",
					Info: "#4070c0", Accent: "#a050c0", Secondary: "#30a0a0",
				},
			}
			p := NewPalette(th)
			if hex(p.TerminalBg) != tc.bg || hex(p.TerminalFg) != tc.fg {
				t.Fatalf("terminal fg/bg = %s/%s, want %s/%s",
					hex(p.TerminalFg), hex(p.TerminalBg), tc.fg, tc.bg)
			}
			for i, c := range p.ANSI {
				if got := ContrastRatio(c, p.TerminalBg); got < ansiFloor(i) {
					t.Errorf("derived %s (%s) contrast %.2f, want >= %.2f",
						ANSIName(i), hex(c), got, ansiFloor(i))
				}
			}
			// The bright half must be distinguishable from its base, otherwise
			// programs that use both look flat.
			for i := ANSIRed; i <= ANSICyan; i++ {
				if hex(p.ANSI[i]) == hex(p.ANSI[i+8]) {
					t.Errorf("%s and its bright variant are identical (%s)", ANSIName(i), hex(p.ANSI[i]))
				}
			}
		})
	}
}

// TestApplyTerminalConfig (#1363): `[theme.terminal]` keys override the
// resolved palette by slot name, verbatim — a user's explicit choice is not
// second-guessed by the contrast floor.
func TestApplyTerminalConfig(t *testing.T) {
	p := NewPalette(Default())
	before := hex(p.ANSI[ANSIGreen])
	cfg := map[string]string{
		"theme.terminal.red":          "#123456",
		"theme.terminal.bright_white": "#abcdef",
		"theme.terminal.foreground":   "#fefefe",
		"theme.terminal.background":   "#010101",
		"theme.terminal.blue":         "", // empty values never override
	}
	ApplyTerminalConfig(p, func(k string) (string, bool) { v, ok := cfg[k]; return v, ok })
	if got := hex(p.ANSI[ANSIRed]); got != "#123456" {
		t.Errorf("red = %s, want #123456", got)
	}
	if got := hex(p.ANSI[ANSIBrightWhite]); got != "#abcdef" {
		t.Errorf("bright_white = %s, want #abcdef", got)
	}
	if got := hex(p.TerminalFg); got != "#fefefe" {
		t.Errorf("foreground = %s, want #fefefe", got)
	}
	if got := hex(p.TerminalBg); got != "#010101" {
		t.Errorf("background = %s, want #010101", got)
	}
	if got := hex(p.ANSI[ANSIGreen]); got != before {
		t.Errorf("unmentioned green changed to %s, want %s", got, before)
	}
	// A nil getter (no configuration at all) must leave the palette alone.
	ApplyTerminalConfig(p, nil)
	if got := hex(p.ANSI[ANSIRed]); got != "#123456" {
		t.Errorf("nil getter changed red to %s", got)
	}
}

// TestANSINames (#1363): the slot names are the config keys, in palette order.
func TestANSINames(t *testing.T) {
	names := ANSINames()
	if len(names) != ANSICount {
		t.Fatalf("got %d names, want %d", len(names), ANSICount)
	}
	if names[ANSIRed] != "red" || names[ANSIBrightBlack] != "bright_black" {
		t.Errorf("unexpected names: %v", names)
	}
	if ANSIName(-1) != "" || ANSIName(ANSICount) != "" {
		t.Error("out-of-range indexes must name nothing")
	}
}
