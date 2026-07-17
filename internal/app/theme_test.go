package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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

// TestSelectThemePersistsUserScope (#667): a palette theme choice applies
// immediately AND lands as theme.name in the USER settings file — the same
// write the Settings page does — so it follows the user across projects and
// restarts instead of living in the per-project session.
func TestSelectThemePersistsUserScope(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	cfg, _ := config.Load(config.Options{})
	m := NewWith(themeReg(), host.FromConfig(cfg))

	tm, cmd := m.Update(SelectThemeMsg{Name: "nord"})
	m = tm.(Model)
	if m.pal().Name != "nord" {
		t.Fatalf("theme must apply immediately, got %q", m.pal().Name)
	}
	if cmd == nil {
		t.Fatal("selection must return the user-scope config write")
	}
	msg := runUntilReload(t, cmd)
	data, err := os.ReadFile(filepath.Join(dir, "settings.toml"))
	if err != nil {
		t.Fatalf("user settings must exist after the write: %v", err)
	}
	if s := string(data); !strings.Contains(s, "nord") {
		t.Fatalf("theme.name missing from the user settings: %q", s)
	}
	// The reload keeps the selection (config is now the source of truth).
	tm, _ = m.Update(msg)
	m = tm.(Model)
	if m.pal().Name != "nord" {
		t.Errorf("after reload: theme = %q, want nord", m.pal().Name)
	}
	// Nothing theme-shaped goes into the per-project session anymore.
	if got := m.snapshotSession().Theme; got != "" {
		t.Errorf("session must not carry a theme, got %q", got)
	}
	// An unknown name applies the fallback and writes nothing.
	tm, _ = m.Update(SelectThemeMsg{Name: "bogus"})
	m = tm.(Model)
	if m.pal().Name != theme.DefaultName {
		t.Errorf("unknown name: theme = %q, want %s", m.pal().Name, theme.DefaultName)
	}
	if cmd := m.selectTheme("bogus"); cmd != nil {
		t.Error("unknown name must not return a config write")
	}
	if data, err := os.ReadFile(filepath.Join(dir, "settings.toml")); err == nil && strings.Contains(string(data), "bogus") {
		t.Error("unknown name leaked into the user settings")
	}
}

// runUntilReload executes cmd (unwrapping a root-Update batch) and returns the
// ConfigReloadedMsg it produces.
func runUntilReload(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			if m := c(); m != nil {
				if _, ok := m.(config.ConfigReloadedMsg); ok {
					return m
				}
			}
		}
		t.Fatal("batch carried no config reload")
	}
	if _, ok := msg.(config.ConfigReloadedMsg); !ok {
		t.Fatalf("write must reload the config, got %T", msg)
	}
	return msg
}

// TestStaleSessionThemeIgnored (#667): a pre-#667 session.json carrying a
// per-project theme override no longer beats the config at startup.
func TestStaleSessionThemeIgnored(t *testing.T) {
	state := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", state)
	t.Chdir(t.TempDir())
	if err := os.WriteFile(filepath.Join(state, "session.json"), []byte(`{"theme":"nord"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(config.Options{})
	m := NewWith(themeReg(), host.FromConfig(cfg))
	if m.pal().Name != theme.DefaultName {
		t.Fatalf("stale session theme must be ignored, got %q", m.pal().Name)
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
