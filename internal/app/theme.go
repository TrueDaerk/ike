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
// name and persist it as a user setting (#667). Dispatched by the
// "themes.select.*" palette commands.
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
	m.activeWS().Panes.SetPalette(p)
	m.palette.SetPalette(p)
	m.finder.SetPalette(p)
	m.shell.SetPalette(p)
	m.help.SetPalette(p)
	m.menu.SetPalette(p)
	m.ctxMenu.SetPalette(p)
	m.settings.SetPalette(p)
	m.commitUI.SetPalette(p)
}

// selectTheme switches the active color scheme by name: resolve against
// built-ins + plugin themes, apply immediately, and persist the choice as a
// USER setting (#667) — theme.name in ~/.ike/settings.toml, the same write
// the Settings → Appearance page performs. The returned command carries the
// write + reload; a theme is a user preference, so it follows the user across
// projects instead of living in the per-project session.
func (m *Model) selectTheme(name string) tea.Cmd {
	sel, found := theme.Select(name, m.reg.Themes())
	m.applyTheme(theme.NewPalette(sel))
	if !found {
		m.host.Notify(host.Warn, "unknown theme "+strconvQuote(name)+", using "+theme.DefaultName)
		return nil
	}
	m.host.Notify(host.Info, "theme: "+sel.Name)
	return config.WriteAndReload(m.cfgOpts, config.UserScope, "theme.name", sel.Name)
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
	// Theme: config is the single source of truth (#667 dropped the runtime
	// override) — re-resolve and re-thread on every reload so a [theme].name
	// change lands without a restart.
	pal, warning := resolveTheme(m.reg, hcfg)
	m.applyTheme(pal)
	// Persist a config-driven show_hidden change like the runtime `.` toggle
	// does (#642): Configure applies it live, but until now only the toggle and
	// a clean quit wrote the session, so after a kill/crash restoreSession
	// re-applied the stale value over the settings edit. Comparing before and
	// after keeps unrelated reloads from touching session.json.
	prevHidden := m.explorer().ShowingHidden()
	m.activeWS().Panes.Reconfigure(hcfg)
	if m.explorer().ShowingHidden() != prevHidden {
		saveSession(m.snapshotSession())
	}
	// [backup] edits apply live too: interval changes re-arm, disabling purges
	// existing snapshots (Roadmap 0210, #167).
	m.reconfigureBackup(hcfg)
	// editor.auto_save edits too: an idle-interval change re-arms, leaving
	// idle mode drops pending marks (#731).
	m.reconfigureAutosaveIdle(hcfg)
	// Rebuild the key resolver so keymap.bindings.* edits (the settings keymap
	// page, #93) re-resolve live, like every other config change.
	m.keys = buildKeymap(hcfg, m.bindings)
	// ui.popup_max_width (#932) applies live to the capped popups — including
	// the settings panel currently hosting the edit.
	m.shell.SetMaxWidth(popupMaxWidth())
	m.palette.SetMaxWidth(popupMaxWidth())
	if m.settings.IsOpen() {
		w, h := m.settingsSize()
		m.settings.SetSize(w, h)
	}
	// Re-plan the terminal toolchain activation (#98, #652): shims regenerate
	// or sweep and the overlay recomputes, so NEW terminals pick up an
	// interpreter change. Running sessions keep their environment (a PATH
	// prepend cannot retarget a live shell); only surviving shims — being
	// re-read per invocation — retarget live sessions too.
	terminalEnv()
	// Drop the cached toolchain labels (#101): an interpreter change must
	// re-resolve the status line's toolchain segment. Keys are deleted in
	// place — the map pointer is shared across value-model copies.
	for k := range m.toolchainSeg {
		delete(m.toolchainSeg, k)
	}
	if warning != "" {
		m.host.Notify(host.Warn, warning)
	}
}
