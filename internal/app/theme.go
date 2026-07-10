package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/theme"
)

// SelectThemeMsg asks the root model to switch the active color scheme by
// name, session-only (the config write path belongs to Roadmap 0040/0090).
// Dispatched by the "themes.select.*" palette commands.
type SelectThemeMsg struct{ Name string }

// themeProvider is the compile-in plugin that contributes the built-in color
// schemes (Roadmap 0110) plus one palette command per scheme ("Theme:
// tokyo-night", …). Third-party plugins add more the same way, via
// Capabilities.Themes (and their own commands if they want palette entries).
type themeProvider struct{}

func (themeProvider) ID() string { return "themes" }

func (themeProvider) Capabilities() plugin.Capabilities {
	builtins := theme.Builtins()
	cmds := make([]plugin.Command, 0, len(builtins))
	for _, t := range builtins {
		name := t.Name
		cmds = append(cmds, plugin.Command{
			ID:    "themes.select." + name,
			Title: "Theme: " + name,
			Scope: plugin.GlobalScope(),
			Run: func(h host.API) tea.Cmd {
				return h.Dispatch(SelectThemeMsg{Name: name})
			},
		})
	}
	return plugin.Capabilities{Themes: builtins, Commands: cmds}
}

func init() { registry.Register(themeProvider{}) }

// themeNames lists every registered theme name for the Appearance settings
// enum, sorted by the registry's deterministic theme order.
func themeNames(reg *registry.Registry) []string {
	themes := reg.Themes()
	names := make([]string, len(themes))
	for i, t := range themes {
		names[i] = t.Name
	}
	return names
}

// resolveTheme resolves [theme].name against the built-ins plus every
// plugin-registered theme and returns the ready-to-render palette. An unknown
// name falls back to the default theme with a non-fatal status warning rather
// than crashing or blanking the IDE.
func resolveTheme(reg *registry.Registry, cfg host.Config) (*theme.Palette, string) {
	name := ""
	if cfg != nil {
		if v, ok := cfg.Get("theme.name"); ok {
			name = v
		}
	}
	sel, found := theme.Select(name, reg.Themes())
	warning := ""
	if !found {
		warning = "unknown theme " + strconvQuote(name) + ", using " + theme.DefaultName
	}
	return theme.NewPalette(sel), warning
}

// strconvQuote is a tiny local quote so the warning reads well without pulling
// fmt into the hot path.
func strconvQuote(s string) string { return "\"" + s + "\"" }

// applyTheme threads a freshly resolved palette through the model: pane
// instances (editor highlight defaults, explorer file colors, chrome), the
// command palette overlay, and the root's own chrome. Used at startup and on
// live config reloads.
func (m *Model) applyTheme(p *theme.Palette) {
	m.themePal = p
	m.panes.SetPalette(p)
	m.palette.SetPalette(p)
	m.finder.SetPalette(p)
	m.shell.SetPalette(p)
	m.help.SetPalette(p)
	m.menu.SetPalette(p)
	m.settings.SetPalette(p)
}

// selectTheme switches the active color scheme by name for this session:
// resolve against built-ins + plugin themes, re-thread everywhere, and confirm
// (or warn) with a toast. It does not write the choice back to config.
func (m *Model) selectTheme(name string) {
	sel, found := theme.Select(name, m.reg.Themes())
	m.applyTheme(theme.NewPalette(sel))
	m.themeOverride = sel.Name // persisted in the session so the choice sticks
	if !found {
		m.host.Notify(host.Warn, "unknown theme "+strconvQuote(name)+", using "+theme.DefaultName)
		return
	}
	m.host.Notify(host.Info, "theme: "+sel.Name)
}

// restoreTheme re-applies a session-persisted runtime theme override, if any,
// so a palette-selected scheme survives a restart. It threads the palette
// without touching the status line (startup should be quiet).
func (m *Model) restoreTheme(name string) {
	if name == "" {
		return
	}
	sel, _ := theme.Select(name, m.reg.Themes())
	m.applyTheme(theme.NewPalette(sel))
	m.themeOverride = sel.Name
}

// reloadConfig applies a reloaded configuration (config.ConfigReloadedMsg):
// publishes it process-wide and re-resolves + re-threads the theme palette so
// a [theme].name change takes effect without a restart.
func (m *Model) reloadConfig(cfg *config.Config) {
	if cfg == nil {
		return
	}
	config.Set(cfg)
	hcfg := host.FromConfig(cfg)
	m.host.SetConfig(hcfg)
	// Re-resolve plugin toggles (#133): the palette/menu/help read the
	// registry live, so SetEnabled plus the keymap rebuild below is the whole
	// re-resolution.
	applyPluginConfig(m.reg, hcfg)
	pal, warning := resolveTheme(m.reg, hcfg)
	m.applyTheme(pal)
	m.panes.Reconfigure(hcfg)
	// [backup] edits apply live too: interval changes re-arm, disabling purges
	// existing snapshots (Roadmap 0210, #167).
	m.reconfigureBackup(hcfg)
	// Rebuild the key resolver so keymap.bindings.* edits (the settings keymap
	// page, #93) re-resolve live, like every other config change.
	m.keys = buildKeymap(hcfg, m.bindings)
	// Regenerate the terminal shims (#98): they exec by absolute path and are
	// re-read per invocation, so an interpreter change retargets even the
	// already-running sessions.
	terminalEnv()
	if warning != "" {
		m.host.Notify(host.Warn, warning)
	}
}
