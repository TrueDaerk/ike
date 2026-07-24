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
	cfgAutoReveal  = "explorer.auto_reveal"
	cfgIcons       = "explorer.icons"
	cfgExclude     = "explorer.exclude"
)

// Configure applies the [explorer] configuration section to the model: initial
// hidden-file visibility, indent width, sort mode, and the per-filetype colour
// table. Unset keys keep their built-in defaults. It triggers no scan; call it
// before Init so the first render already reflects the config.
func (m *Model) Configure(cfg host.Config) {
	// Indent width, icons and the colour table all change row text (#1096).
	defer m.invalidateWidth()
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
	if v, ok := cfg.Get(cfgAutoReveal); ok {
		// Auto-reveal-on-focus (#1042), the JetBrains "autoscroll from
		// source". Off by default.
		m.autoReveal = v == "true"
	}
	if v, ok := cfg.Get(cfgExclude); ok && v != m.excludeCfg {
		// The exclude glob list (#1139), delivered as the Flat comma-joined
		// form of the [explorer] exclude TOML array. Only a genuine change
		// re-applies and rebuilds, so unrelated live reloads stay cheap; the
		// rebuild makes an edit take effect without restart and re-clamps a
		// now-out-of-range scroll offset (#1096 width invalidation happens in
		// rebuild and the deferred invalidateWidth above).
		m.exclude = parseExclude(v)
		m.excludeCfg = v
		m.rebuild()
	}
	if v, ok := cfg.Get(cfgIcons); ok {
		// One-cell file-type marker glyphs before each name (#1046). Off by
		// default: plain trees stay compact.
		m.icons = v == "true"
	}
	m.cfgColors = readColors(cfg)
	m.mergeColors()
}

// parseExclude splits the comma-joined explorer.exclude value into the glob
// pattern list (#1139), trimming whitespace and dropping empties — so both
// ".git,.idea" and ".git, .idea" (and an empty string: no exclusions) parse
// as expected.
func parseExclude(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
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
	m.rebuildColorIndex() // keep the glob/colour index in step (#1098)
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
