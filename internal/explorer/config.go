package explorer

import (
	"strconv"
	"strings"

	"ike/internal/host"
)

// Config keys consumed from the merged [explorer] section (see internal/config).
const (
	cfgShowHidden  = "explorer.show_hidden"
	cfgTreeIndent  = "explorer.tree_indent"
	cfgSort        = "explorer.sort"
	cfgColorsPfx   = "explorer.colors."
	cfgAutoRefresh = "explorer.auto_refresh"
)

// Configure applies the [explorer] configuration section to the model: initial
// hidden-file visibility, indent width, sort mode, and the per-filetype colour
// table. Unset keys keep their built-in defaults. It triggers no scan; call it
// before Init so the first render already reflects the config.
func (m *Model) Configure(cfg host.Config) {
	if cfg == nil {
		return
	}
	if v, ok := cfg.Get(cfgShowHidden); ok {
		m.showHidden = v == "true"
	}
	if v, ok := cfg.Get(cfgTreeIndent); ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			m.indent = n
		}
	}
	if v, ok := cfg.Get(cfgSort); ok && v != "" {
		m.sort = v
	}
	if v, ok := cfg.Get(cfgAutoRefresh); ok {
		m.autoRefresh = v != "false"
	}
	if colors := readColors(cfg); len(colors) > 0 {
		// Start from the defaults so the required "dir"/"default" fallbacks always
		// exist, then overlay the configured entries.
		merged := colorTable{}
		for k, v := range defaultColors {
			merged[k] = v
		}
		for k, v := range colors {
			merged[k] = v
		}
		m.colors = merged
	}
}

// readColors collects every "explorer.colors.<key>" entry into a colour table.
func readColors(cfg host.Config) colorTable {
	out := colorTable{}
	for _, k := range cfg.Keys() {
		if ext, ok := strings.CutPrefix(k, cfgColorsPfx); ok {
			if v, found := cfg.Get(k); found {
				out[ext] = v
			}
		}
	}
	return out
}
