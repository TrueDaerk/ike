package highlight

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// namedColors maps the human colour names the config accepts to lipgloss colour
// values. Anything not found here is passed to lipgloss verbatim, so hex
// ("#1f6feb") and raw ANSI indices ("39") work too. Mirrors explorer/colors.go.
var namedColors = map[string]string{
	"black":   "0",
	"red":     "1",
	"green":   "2",
	"yellow":  "3",
	"blue":    "4",
	"magenta": "5",
	"cyan":    "6",
	"white":   "7",
	"gray":    "240",
	"grey":    "240",
	"orange":  "208",
}

// defaultCaptures is the built-in capture→colour table so code is never
// monochrome out of the box. Keys are Tree-sitter capture names; lookup falls
// back from a dotted name ("function.builtin") to its head ("function").
var defaultCaptures = map[string]string{
	"keyword":          "magenta",
	"operator":         "white",
	"string":           "green",
	"number":           "orange",
	"comment":          "gray",
	"function":         "blue",
	"type":             "cyan",
	"constant":         "orange",
	"constant.builtin": "orange",
	"variable":         "white",
	"variable.builtin": "red",
	"property":         "white",
	"label":            "magenta",
	"attribute":        "yellow",
	"punctuation":      "gray",
	"escape":           "orange",
	"boolean":          "orange",
	"tag":              "red",
	"embedded":         "white",
}

// Theme resolves Tree-sitter capture names to lipgloss styles. It is built from
// the [theme] config (theme.captures.<name> keys) layered over the built-in
// defaults, then memoises resolved styles.
type Theme struct {
	colors map[string]string
	cache  map[string]styleHit
}

// styleHit memoises a resolved capture: ok=false records a known miss so the
// fallback walk runs only once per capture name.
type styleHit struct {
	style lipgloss.Style
	ok    bool
}

// NewTheme builds a theme. get reads a config key (theme.captures.keyword, …);
// pass nil to use only the built-in defaults.
func NewTheme(get func(key string) (string, bool)) Theme {
	colors := make(map[string]string, len(defaultCaptures))
	for k, v := range defaultCaptures {
		colors[k] = v
	}
	if get != nil {
		for k := range defaultCaptures {
			if v, ok := get("theme.captures." + k); ok && v != "" {
				colors[k] = v
			}
		}
	}
	return Theme{colors: colors, cache: make(map[string]styleHit)}
}

// Style returns the style for a capture and whether a colour was found. Lookup
// tries the full dotted capture, then progressively shorter prefixes
// ("function.builtin" → "function"), so unknown sub-captures inherit their head.
func (t Theme) Style(capture string) (lipgloss.Style, bool) {
	if capture == "" {
		return lipgloss.Style{}, false
	}
	if hit, ok := t.cache[capture]; ok {
		return hit.style, hit.ok
	}
	name := capture
	for {
		if tok, ok := t.colors[name]; ok {
			st := lipgloss.NewStyle().Foreground(resolveColor(tok))
			t.cache[capture] = styleHit{style: st, ok: true}
			return st, true
		}
		i := strings.LastIndex(name, ".")
		if i < 0 {
			break
		}
		name = name[:i]
	}
	t.cache[capture] = styleHit{ok: false}
	return lipgloss.Style{}, false
}

// resolveColor resolves a config colour token (name, hex, or ANSI index) to a
// lipgloss colour.
func resolveColor(token string) color.Color {
	if v, ok := namedColors[strings.ToLower(token)]; ok {
		return lipgloss.Color(v)
	}
	return lipgloss.Color(token)
}
