package highlight

import (
	"strings"

	"charm.land/lipgloss/v2"

	"ike/internal/theme"
)

// Theme resolves Tree-sitter capture names to lipgloss styles. It is built
// from capture-color defaults (the active theme palette's captures, Roadmap
// 0110) layered under `theme.captures.<name>` config keys, then memoises
// resolved styles.
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

// NewTheme builds a theme from defaults layered under config. defaults is the
// capture→color table of the active palette; nil uses the built-in default
// palette's captures. get reads a config key (theme.captures.keyword, …); pass
// nil to use only the defaults.
func NewTheme(defaults map[string]string, get func(key string) (string, bool)) Theme {
	if defaults == nil {
		defaults = theme.Default().Captures
	}
	colors := make(map[string]string, len(defaults))
	for k, v := range defaults {
		colors[k] = v
	}
	if get != nil {
		for k := range defaults {
			if v, ok := get("theme.captures." + k); ok && v != "" {
				colors[k] = v
			}
		}
	}
	// Rainbow brackets (#789): each cycle slot derives from an existing
	// capture colour of the active palette unless the theme (or a
	// theme.captures.rainbow.N config key) sets it explicitly.
	for i, src := range rainbowSources {
		key := rainbowCapture(i)
		if get != nil {
			if v, ok := get("theme.captures." + key); ok && v != "" {
				colors[key] = v
				continue
			}
		}
		if _, exists := colors[key]; exists {
			continue
		}
		if c, ok := colors[src]; ok {
			colors[key] = c
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
			st := lipgloss.NewStyle().Foreground(theme.Resolve(tok))
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
