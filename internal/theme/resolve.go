package theme

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// namedColors maps the human colour names the config accepts to lipgloss colour
// values. Anything not found here is passed to lipgloss verbatim, so hex
// ("#1f6feb") and raw ANSI indices ("39") work too. The values favour bright,
// high-contrast tones over classic dark ANSI ones, since the default palette
// paints a dark background and these are foreground colours shown on top of it.
var namedColors = map[string]string{
	"black":   "#000000",
	"red":     "#ff5555",
	"green":   "#5fd75f",
	"yellow":  "#ffd75f",
	"blue":    "#5fafff",
	"magenta": "#d787ff",
	"cyan":    "#5fd7d7",
	"white":   "#e4e4e4",
	"gray":    "#8a8a8a",
	"grey":    "#8a8a8a",
	"orange":  "#ff8700",
}

// Resolve resolves a colour token (name, hex, or ANSI index) to a lipgloss
// colour. It is the single resolver shared by highlight, explorer, and the
// chrome renderers.
func Resolve(token string) color.Color {
	if v, ok := namedColors[strings.ToLower(token)]; ok {
		return lipgloss.Color(v)
	}
	return lipgloss.Color(token)
}
