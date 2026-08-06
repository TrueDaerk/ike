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
	return NewThemeKeys(defaults, get, nil)
}

// NewThemeKeys is NewTheme with enumeration: keys lists every configuration
// key, so an override may name a capture the active theme does not define
// itself ("function.builtin", a grammar-specific capture) instead of being
// limited to the palette's own table (#1318). A nil keys behaves like
// NewTheme.
func NewThemeKeys(defaults map[string]string, get func(key string) (string, bool), keys func() []string) Theme {
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
	if keys != nil {
		const prefix = "theme.captures."
		for _, k := range keys() {
			name, isCapture := strings.CutPrefix(k, prefix)
			if !isCapture || name == "" {
				continue
			}
			if v, ok := get(k); ok && v != "" {
				colors[name] = v
			}
		}
	}
	// Rainbow brackets (#789): each cycle slot derives from an existing
	// capture colour of the active palette unless the theme (or a
	// theme.captures.rainbow.N config key) sets it explicitly.
	for i, src := range rainbowSources {
		key := RainbowCapture(i)
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
	// Diff-format captures (#1630) and regex mini-grammar captures (#1631)
	// derive the same way: their coloring follows the active palette without
	// every theme declaring the slots. An explicit theme table entry or a
	// theme.captures.* config key overrides a slot.
	for _, table := range []map[string]string{diffSources, regexSources} {
		for key, src := range table {
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
	}
	return Theme{colors: colors, cache: make(map[string]styleHit)}
}

// diffSources maps each unified-diff capture (#1630) to an existing theme
// capture it derives from, mirroring rainbowSources: added lines take the
// string green, removed lines the variable.builtin red, @@ hunk headers the
// function color, file headers the keyword color, and git meta lines (index,
// mode, rename …) read as comments. The word-level emphasis captures
// diff.plus.emph / diff.minus.emph resolve through the dotted-prefix fallback
// to their line's color; the editor adds the changed-range background.
var diffSources = map[string]string{
	"diff.plus":   "string",
	"diff.minus":  "variable.builtin",
	"diff.delta":  "function",
	"diff.header": "keyword",
	"diff.meta":   "comment",
}

// regexSources maps each regex mini-grammar capture (#1631) to the existing
// theme capture it derives from: character classes read as types, escapes as
// escapes, quantifiers and alternation as keywords, anchors as functions,
// group names and inline flags as attributes. "regex.group" is the flat
// group-paren colour used when rainbow brackets are disabled — enabled, group
// pairs colour by depth through the rainbow.N slots instead.
var regexSources = map[string]string{
	"regex.class":       "type",
	"regex.escape":      "escape",
	"regex.quantifier":  "keyword",
	"regex.alternation": "keyword",
	"regex.anchor":      "function",
	"regex.group":       "function",
	"regex.group.name":  "attribute",
	"regex.flags":       "attribute",
	"regex.comment":     "comment",
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
