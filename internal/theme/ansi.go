package theme

import "image/color"

// ansi.go gives every theme the 16 standard ANSI colors plus the terminal's
// default foreground/background (#1363). Programs running in the integrated
// terminal paint with indexed SGR colors (30–37 / 90–97, 256-color indexes
// 0–15); without a theme-owned palette those resolve against the *outer*
// terminal's colors while their background comes from IKE's own wash, and the
// two can land close enough to be unreadable. internal/terminal rewrites the
// grid's indexed colors to the values resolved here.

// ANSI palette indexes, in the standard order. Bright variants are the base
// index plus 8.
const (
	ANSIBlack = iota
	ANSIRed
	ANSIGreen
	ANSIYellow
	ANSIBlue
	ANSIMagenta
	ANSICyan
	ANSIWhite
	ANSIBrightBlack
	ANSIBrightRed
	ANSIBrightGreen
	ANSIBrightYellow
	ANSIBrightBlue
	ANSIBrightMagenta
	ANSIBrightCyan
	ANSIBrightWhite
	// ANSICount is the number of indexed colors a theme defines.
	ANSICount
)

// Terminal is a theme's terminal palette: the 16 standard colors as color
// tokens (name, hex or ANSI index — whatever Resolve accepts) plus the
// terminal's default foreground and background. Every field is optional; an
// empty one is derived (see deriveANSI) from the theme's own colors.
type Terminal struct {
	ANSI       [ANSICount]string
	Foreground string // terminal default foreground (SGR 39)
	Background string // terminal default background (SGR 49)
}

// ansiNames are the configuration/lookup names of the 16 indexed colors, in
// palette order. They double as the `[theme.terminal]` config keys.
var ansiNames = [ANSICount]string{
	"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
	"bright_black", "bright_red", "bright_green", "bright_yellow",
	"bright_blue", "bright_magenta", "bright_cyan", "bright_white",
}

// ANSIName returns the palette name of index i ("red", "bright_black"), or ""
// when i is out of range. It is what configuration keys and error messages
// spell.
func ANSIName(i int) string {
	if i < 0 || i >= ANSICount {
		return ""
	}
	return ansiNames[i]
}

// ANSINames lists the 16 color names in palette order.
func ANSINames() []string { return ansiNames[:] }

// minTerminalContrast is the WCAG ratio every derived palette entry reaches
// against the terminal background. 3.0 is the large-text/UI-component floor —
// terminal output is dense, so anything below reads as mud.
const minTerminalContrast = 3.0

// dimContrast is the softer floor for the four grey slots (black/white and
// their bright variants): one end of that ramp always sits near the
// background — black on a dark theme, white on a light one — and shells use
// it for de-emphasised chrome. Those stay quiet, but never invisible.
const dimContrast = 1.7

// ansiFloor is the contrast floor index i must clear against the terminal
// background: the six hues have to be readable, the grey ramp only visible.
func ansiFloor(i int) float64 {
	switch i {
	case ANSIBlack, ANSIWhite, ANSIBrightBlack, ANSIBrightWhite:
		return dimContrast
	}
	return minTerminalContrast
}

// resolveTerminal fills p.ANSI / p.TerminalFg / p.TerminalBg from t, deriving
// every entry the theme leaves empty. It runs last in NewPalette: the
// derivation reads the already-resolved semantic slots.
func (p *Palette) resolveTerminal(t Terminal) {
	p.TerminalBg = firstColor(t.Background, p.Background)
	p.TerminalFg = firstColor(t.Foreground, p.Foreground)
	derived := p.deriveANSI()
	for i := range p.ANSI {
		c := derived[i]
		if t.ANSI[i] != "" {
			// A theme's own value is kept verbatim unless it would be
			// unreadable on that theme's terminal background — several
			// upstream palettes ship a "normal" half that is genuinely too
			// dim to read, which is the whole complaint behind #1363.
			c = lift(Resolve(t.ANSI[i]), p.TerminalFg, p.TerminalBg, ansiFloor(i))
		}
		p.ANSI[i] = c
	}
}

// firstColor resolves token, falling back to c when it is empty.
func firstColor(token string, c color.Color) color.Color {
	if token == "" {
		return c
	}
	return Resolve(token)
}

// deriveANSI builds a full palette out of the theme's own colors, so a theme
// (built-in or third-party) that declares no terminal block still gets one
// that matches its background. The six hues come from the semantic slots the
// theme already tunes — error is red, success green, warning yellow, info
// blue, accent magenta, secondary cyan — the greys are a foreground↔background
// ramp, and every entry is lifted until it clears the contrast floor. The ramp
// works for dark and light themes alike because it is expressed relative to
// the theme's own two poles rather than to absolute black and white.
func (p *Palette) deriveANSI() [ANSICount]color.Color {
	fg, bg := p.TerminalFg, p.TerminalBg
	ramp := func(frac float64) color.Color { return Mix(fg, bg, frac) }
	base := [ANSICount]color.Color{
		ANSIBlack:   ramp(0.45),
		ANSIRed:     p.Error,
		ANSIGreen:   p.Success,
		ANSIYellow:  p.Warning,
		ANSIBlue:    p.Info,
		ANSIMagenta: p.Accent,
		ANSICyan:    p.Secondary,
		ANSIWhite:   ramp(0.85),
	}
	// The bright half is the base hue pulled toward the foreground: on a dark
	// theme that lightens it, on a light one it darkens — either way it moves
	// away from the background, which is what "bright" has to mean for the
	// palette to stay legible on both.
	for i := ANSIBlack; i <= ANSIWhite; i++ {
		base[i+8] = Mix(base[i], fg, 0.35)
	}
	base[ANSIBrightBlack] = ramp(0.62)
	base[ANSIBrightWhite] = fg
	for i := range base {
		base[i] = lift(base[i], fg, bg, ansiFloor(i))
	}
	return base
}

// lift mixes c toward fg (and, once that is exhausted, toward the pole
// furthest from bg) until it clears the contrast floor against bg. A color
// that already clears it is returned untouched, so an explicitly tuned hue
// keeps its exact value.
func lift(c, fg, bg color.Color, floor float64) color.Color {
	if c == nil {
		return fg
	}
	if ContrastRatio(c, bg) >= floor {
		return c
	}
	// The foreground is the theme's own "maximally readable" tone; walking
	// toward it keeps the hue recognisable while gaining contrast.
	for _, frac := range []float64{0.25, 0.5, 0.75} {
		if m := Mix(fg, c, frac); ContrastRatio(m, bg) >= floor {
			return m
		}
	}
	// Still short (a low-contrast foreground, a mid-grey background): push
	// toward whichever pole is further away, which always has more headroom.
	pole := color.Color(color.White)
	if Luminance(bg) > 0.5 {
		pole = color.Black
	}
	for _, frac := range []float64{0.25, 0.5, 0.75, 1} {
		if m := Mix(pole, c, frac); ContrastRatio(m, bg) >= floor {
			return m
		}
	}
	return pole
}

// ApplyTerminalConfig layers `[theme.terminal]` configuration over an already
// resolved palette: `black`, `bright_blue`, … name the 16 indexed colors and
// `foreground` / `background` the terminal defaults. get reads a config key
// (nil leaves the palette untouched), matching how theme.captures overrides
// layer over a theme's syntax colors.
func ApplyTerminalConfig(p *Palette, get func(key string) (string, bool)) {
	if p == nil || get == nil {
		return
	}
	const prefix = "theme.terminal."
	for i, name := range ansiNames {
		if v, ok := get(prefix + name); ok && v != "" {
			p.ANSI[i] = Resolve(v)
		}
	}
	if v, ok := get(prefix + "foreground"); ok && v != "" {
		p.TerminalFg = Resolve(v)
	}
	if v, ok := get(prefix + "background"); ok && v != "" {
		p.TerminalBg = Resolve(v)
	}
}
