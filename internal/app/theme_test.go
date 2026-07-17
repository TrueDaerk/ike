package app

import (
	"os"
	"path/filepath"
	"testing"

	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/theme"
)

// themeReg is a registry carrying the built-in theme provider, mirroring what
// init() puts into the global one.
func themeReg() *registry.Registry {
	r := registry.New()
	r.Add(themeProvider{})
	return r
}

func TestResolveTheme(t *testing.T) {
	// Named built-in resolves without warning.
	pal, warn := resolveTheme(themeReg(), host.MapConfig{"theme.name": "tokyo-night"})
	if pal.Name != "tokyo-night" || warn != "" {
		t.Errorf("got %q, warn %q", pal.Name, warn)
	}
	// Empty name is the default, no warning.
	pal, warn = resolveTheme(themeReg(), host.MapConfig{})
	if pal.Name != theme.DefaultName || warn != "" {
		t.Errorf("empty name: got %q, warn %q", pal.Name, warn)
	}
	// Unknown name falls back with a non-fatal warning.
	pal, warn = resolveTheme(themeReg(), host.MapConfig{"theme.name": "bogus"})
	if pal.Name != theme.DefaultName || warn == "" {
		t.Errorf("unknown name: got %q, warn %q", pal.Name, warn)
	}
}

// TestRegistryThemes: a plugin-registered theme is visible to resolution.
func TestRegistryThemes(t *testing.T) {
	r := themeReg()
	r.Add(fakeThemePlugin{})
	pal, warn := resolveTheme(r, host.MapConfig{"theme.name": "plugin-theme"})
	if pal.Name != "plugin-theme" || warn != "" {
		t.Fatalf("got %q, warn %q", pal.Name, warn)
	}
	if pal.Accent != lipgloss.Color("#123456") {
		t.Errorf("plugin ui slot not honored: %v", pal.Accent)
	}
	// Slots the plugin left empty backfill from the default palette.
	if pal.Background != theme.DefaultPalette().Background {
		t.Errorf("empty slot should backfill: %v", pal.Background)
	}
}

type fakeThemePlugin struct{}

func (fakeThemePlugin) ID() string { return "fake-theme" }

func (fakeThemePlugin) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{Themes: []theme.Theme{{
		Name: "plugin-theme",
		Dark: true,
		UI:   theme.UI{Accent: "#123456"},
	}}}
}

// TestSelectThemeCommand: the palette command dispatches SelectThemeMsg and the
// root switches the live palette, session-only.
func TestSelectThemeCommand(t *testing.T) {
	m := NewWith(themeReg(), host.MapConfig{})
	if m.pal().Name != theme.DefaultName {
		t.Fatalf("start theme = %q", m.pal().Name)
	}
	// One command per built-in is registered.
	if _, ok := m.reg.Command("themes.select.nord"); !ok {
		t.Fatal("themes.select.nord command not registered")
	}
	next, _ := m.Update(SelectThemeMsg{Name: "nord"})
	m = next.(Model)
	if m.pal().Name != "nord" {
		t.Errorf("after select: theme = %q, want nord", m.pal().Name)
	}
	// Unknown name falls back to default, no crash.
	next, _ = m.Update(SelectThemeMsg{Name: "bogus"})
	m = next.(Model)
	if m.pal().Name != theme.DefaultName {
		t.Errorf("unknown name: theme = %q, want %s", m.pal().Name, theme.DefaultName)
	}
}

// TestReloadKeepsRuntimeTheme (#241): a config reload triggered by an
// unrelated settings edit must not revert a palette-selected theme; only an
// explicit [theme].name change wins (and clears the override).
func TestReloadKeepsRuntimeTheme(t *testing.T) {
	cfg, _ := config.Load(config.Options{})
	m := NewWith(themeReg(), host.FromConfig(cfg))
	next, _ := m.Update(SelectThemeMsg{Name: "nord"})
	m = next.(Model)
	if m.pal().Name != "nord" {
		t.Fatalf("after select: theme = %q, want nord", m.pal().Name)
	}

	// Unrelated edit: theme.name is unchanged, the runtime pick survives.
	cfg2, _ := config.Load(config.Options{})
	cfg2.Editor.TabWidth = 2
	tm, _ := m.Update(config.ConfigReloadedMsg{Config: cfg2})
	m = tm.(Model)
	if m.pal().Name != "nord" {
		t.Errorf("unrelated reload reverted theme to %q, want nord", m.pal().Name)
	}
	if m.themeOverride != "nord" {
		t.Errorf("override = %q, want nord", m.themeOverride)
	}

	// Explicit theme edit: the config choice wins and clears the override.
	cfg3, _ := config.Load(config.Options{})
	cfg3.Theme.Name = "tokyo-night"
	tm, _ = m.Update(config.ConfigReloadedMsg{Config: cfg3})
	m = tm.(Model)
	if m.pal().Name != "tokyo-night" {
		t.Errorf("theme edit: theme = %q, want tokyo-night", m.pal().Name)
	}
	if m.themeOverride != "" {
		t.Errorf("theme edit should clear the override, got %q", m.themeOverride)
	}
}

// TestThemeOverridePersists: a runtime theme selection is snapshotted into the
// session and re-applied on restore, surviving a restart.
func TestThemeOverridePersists(t *testing.T) {
	m := NewWith(themeReg(), host.MapConfig{})
	// No runtime pick yet: nothing persisted, so config still wins next launch.
	if got := m.snapshotSession().Theme; got != "" {
		t.Errorf("fresh session theme = %q, want empty", got)
	}
	next, _ := m.Update(SelectThemeMsg{Name: "nord"})
	m = next.(Model)
	if got := m.snapshotSession().Theme; got != "nord" {
		t.Fatalf("session theme = %q, want nord", got)
	}
	// A fresh model restoring that session lands on nord, not the config default.
	m2 := NewWith(themeReg(), host.MapConfig{})
	m2.restoreTheme("nord")
	if m2.pal().Name != "nord" {
		t.Errorf("restored theme = %q, want nord", m2.pal().Name)
	}
}

// TestReloadPersistsConfigShowHidden (#642): a genuine settings edit to
// explorer.show_hidden applied by a live config reload must persist to the
// session like the runtime `.` toggle does — otherwise a kill/crash (no clean
// quit) leaves a stale session.json that restoreSession re-applies over the
// edit at next boot. An unrelated reload must not touch session.json at all.
func TestReloadPersistsConfigShowHidden(t *testing.T) {
	proj := t.TempDir()
	state := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", state)
	t.Chdir(proj)
	sessionPath := filepath.Join(state, "session.json")

	cfg, _ := config.Load(config.Options{})
	m := NewWith(registry.New(), host.FromConfig(cfg))
	if m.explorer().ShowingHidden() {
		t.Fatal("show_hidden should start off")
	}
	// A previously persisted session holds the old value (e.g. written by an
	// earlier clean quit).
	saveSession(m.snapshotSession())

	// Settings edit: show_hidden flips on and the config reloads live.
	cfg2, _ := config.Load(config.Options{})
	cfg2.Explorer.ShowHidden = true
	tm, _ := m.Update(config.ConfigReloadedMsg{Config: cfg2})
	m = tm.(Model)
	if !m.explorer().ShowingHidden() {
		t.Fatal("config change did not apply live")
	}

	// Simulated kill/crash: no quit. A fresh model restores the session, which
	// must already carry the new value instead of clobbering the edit.
	m2 := NewWith(registry.New(), host.FromConfig(cfg2))
	if !m2.explorer().ShowingHidden() {
		t.Fatal("config-driven show_hidden change did not survive a restart without clean quit (#642)")
	}

	// An unrelated reload (show_hidden unchanged) must not write the session.
	if err := os.Remove(sessionPath); err != nil {
		t.Fatal(err)
	}
	cfg3, _ := config.Load(config.Options{})
	cfg3.Explorer.ShowHidden = true
	cfg3.Editor.TabWidth = 2
	tm, _ = m2.Update(config.ConfigReloadedMsg{Config: cfg3})
	_ = tm
	if _, err := os.Stat(sessionPath); err == nil {
		t.Error("unrelated reload wrote session.json; expected no write")
	}
}
