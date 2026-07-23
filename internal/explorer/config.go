package explorer

import (
	"strconv"
	"strings"

	"ike/internal/host"
	"ike/internal/theme"
	"ike/internal/vcs"
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
	// Apply show_hidden only when the config value actually changed since the
	// last Configure (or on first configure). Live reloads fire on unrelated
	// events (plugin toggle, interpreter change, project switch); re-applying an
	// unchanged default would clobber the runtime `.` toggle every time (#629).
	if v, ok := cfg.Get(cfgShowHidden); ok && v != m.hiddenCfg {
		m.showHidden = v == "true"
		m.hiddenCfg = v
	}
	if v, ok := cfg.Get(cfgTreeIndent); ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			m.indent = n
		}
	}
	if v, ok := cfg.Get(cfgSort); ok && v != "" {
		switch v {
		case "name", "type", "modified":
			if v != m.sort {
				m.sort = v
				// Re-sort the loaded tree live (#1037); new scans sort on
				// arrival anyway.
				m.resortAll(m.root)
				m.rebuild()
			}
		default:
			// Unknown value: keep the current (default "name") ordering.
		}
	}
	if v, ok := cfg.Get(cfgAutoRefresh); ok {
		m.autoRefresh = v != "false"
	}
	m.cfgColors = readColors(cfg)
	m.mergeColors()
}

// SetPalette threads the active theme palette in (Roadmap 0110): its file
// colors become the defaults under any [explorer.colors] overrides, and chrome
// (selection, scrollbar, hover) reads its ui slots.
func (m *Model) SetPalette(p *theme.Palette) {
	m.pal = p
	m.mergeColors()
}

// SetVCS threads the current git status snapshot in (Roadmap 0320): files and
// dirty directories render in their status color. A nil snapshot (not a git
// repo) restores plain filetype coloring.
func (m *Model) SetVCS(snap *vcs.Snapshot) {
	m.vcsSnap = snap
}

// mergeColors rebuilds the colour table: the palette's file colors (default
// theme when none is set) overlaid with the retained [explorer.colors] config
// entries, so per-key config always wins over the named theme.
func (m *Model) mergeColors() {
	merged := colorTable{}
	base := theme.Default().Files
	if m.pal != nil {
		base = m.pal.Files
	}
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range m.cfgColors {
		merged[k] = v
	}
	m.colors = merged
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
