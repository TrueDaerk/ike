package app

import (
	"testing"

	"charm.land/lipgloss/v2"

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
