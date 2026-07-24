package explorer

import (
	"image/color"
	"path/filepath"
	"sort"
	"strings"

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

// suffixColor resolves the filetype tint for a file's extension suffix
// (#1051): exact glob match first (globs sorted for determinism), then a
// bare-extension match. Directories and files without a match return nil —
// the suffix-tint model colours only the extension of clean files; the "dir"
// and "default" keys of older configs are accepted but no longer paint whole
// rows (the row body renders in the plain foreground, the colour channel
// belongs to the VCS status).
//
// The sorted glob list and the resolved colours come precomputed from the
// model's colour index (#1098): sorting the table and re-parsing colour
// strings per row per frame showed up in the profile.
func (t colorTable) suffixColor(n *node, globs []string, vals map[string]color.Color) color.Color {
	if n.isDir {
		return nil
	}
	for _, pat := range globs {
		if ok, _ := filepath.Match(pat, n.name); ok {
			return vals[pat]
		}
	}
	if ext := strings.TrimPrefix(filepath.Ext(n.name), "."); ext != "" {
		if _, ok := t[ext]; ok {
			return vals[ext]
		}
	}
	return nil
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
