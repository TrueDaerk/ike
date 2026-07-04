package explorer

import (
	"image/color"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
)

// colorTable maps extension/glob keys to colour names (or hex). It is built from
// the [explorer.colors] config section. The "dir" and "default" keys are the
// required fallbacks; everything else is a bare extension ("go") or a glob
// ("*.test.go").
type colorTable map[string]string

// defaultColors is the built-in fallback used when no [explorer.colors] config
// is supplied. It mirrors the roadmap's example table so the tree is never
// monochrome out of the box.
var defaultColors = colorTable{
	"dir":     "blue",
	"default": "white",
	"go":      "cyan",
	"md":      "green",
	"toml":    "yellow",
	"json":    "yellow",
	"yaml":    "yellow",
	"lock":    "gray",
}

// namedColors maps the human colour names the config accepts to lipgloss colour
// values. Anything not found here is passed to lipgloss verbatim, so hex
// ("#1f6feb") and raw ANSI indices ("39") work too.
var namedColors = map[string]string{
	"black":   "#000000",
	"red":     "#800000",
	"green":   "#008000",
	"yellow":  "#808000",
	"blue":    "#000080",
	"magenta": "#800080",
	"cyan":    "#008080",
	"white":   "#c0c0c0",
	"gray":    "#585858",
	"grey":    "#585858",
}

// color resolves a config colour token (name, hex, or ANSI index) to a lipgloss
// colour.
func (t colorTable) color(token string) color.Color {
	if v, ok := namedColors[strings.ToLower(token)]; ok {
		return lipgloss.Color(v)
	}
	return lipgloss.Color(token)
}

// style resolves a node to its base lipgloss style. Resolution order matches the
// roadmap: exact glob match (globs sorted for determinism), then the "dir"
// fallback for directories, then a bare-extension match, then "default".
func (t colorTable) style(n *node) lipgloss.Style {
	base := lipgloss.NewStyle()
	for _, pat := range t.globs() {
		if ok, _ := filepath.Match(pat, n.name); ok {
			return base.Foreground(t.color(t[pat]))
		}
	}
	if n.isDir {
		if c, ok := t["dir"]; ok {
			return base.Foreground(t.color(c))
		}
		return base
	}
	if ext := strings.TrimPrefix(filepath.Ext(n.name), "."); ext != "" {
		if c, ok := t[ext]; ok {
			return base.Foreground(t.color(c))
		}
	}
	if c, ok := t["default"]; ok {
		return base.Foreground(t.color(c))
	}
	return base
}

// globs returns the glob keys (those containing a wildcard) sorted, so glob
// resolution is deterministic regardless of map iteration order.
func (t colorTable) globs() []string {
	var out []string
	for k := range t {
		if strings.ContainsAny(k, "*?[") {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
