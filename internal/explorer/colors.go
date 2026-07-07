package explorer

import (
	"path/filepath"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

	"ike/internal/theme"
)

// colorTable maps extension/glob keys to colour names (or hex). It is built from
// the [explorer.colors] config section. The "dir" and "default" keys are the
// required fallbacks; everything else is a bare extension ("go") or a glob
// ("*.test.go").
type colorTable map[string]string

// defaultColors returns the file-color table of the built-in default theme,
// used when no palette has been threaded in yet (Roadmap 0110); the tree is
// never monochrome out of the box.
func defaultColors() colorTable {
	return colorTable(theme.Default().Files)
}

// style resolves a node to its base lipgloss style. Resolution order matches the
// roadmap: exact glob match (globs sorted for determinism), then the "dir"
// fallback for directories, then a bare-extension match, then "default".
func (t colorTable) style(n *node) lipgloss.Style {
	base := lipgloss.NewStyle()
	for _, pat := range t.globs() {
		if ok, _ := filepath.Match(pat, n.name); ok {
			return base.Foreground(theme.Resolve(t[pat]))
		}
	}
	if n.isDir {
		if c, ok := t["dir"]; ok {
			return base.Foreground(theme.Resolve(c))
		}
		return base
	}
	if ext := strings.TrimPrefix(filepath.Ext(n.name), "."); ext != "" {
		if c, ok := t[ext]; ok {
			return base.Foreground(theme.Resolve(c))
		}
	}
	if c, ok := t["default"]; ok {
		return base.Foreground(theme.Resolve(c))
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
