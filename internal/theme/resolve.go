package theme

import (
	"image/color"
	"sort"
	"strconv"
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

// Names lists the accepted colour names, sorted — what a picker offers and
// what an error message can name (#1318).
func Names() []string {
	out := make([]string, 0, len(namedColors))
	for n := range namedColors {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ValidToken reports whether token is a colour this package can resolve: one
// of Names(), a #rgb/#rrggbb hex literal, or an ANSI index 0–255. lipgloss
// silently renders anything else as the terminal default, so configuration
// has to reject it up front rather than leave the user staring at unstyled
// text (#1318).
func ValidToken(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	if _, ok := namedColors[strings.ToLower(token)]; ok {
		return true
	}
	if strings.HasPrefix(token, "#") {
		hex := token[1:]
		if len(hex) != 3 && len(hex) != 6 {
			return false
		}
		for _, r := range strings.ToLower(hex) {
			if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
				return false
			}
		}
		return true
	}
	n, err := strconv.Atoi(token)
	return err == nil && n >= 0 && n <= 255
}
