package editor

// capturecolor_test.go is the end-to-end guard for #1318: a [theme.captures]
// override has to travel the *real* path — TOML file → config.Load → Flat() →
// host.Config → the editor's capture table. The highlight package's own tests
// stub the lookup function, which is exactly why the missing schema wiring
// went unnoticed.

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/theme"
)

func TestCaptureOverrideReachesTheEditor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	if err := os.WriteFile(path, []byte("[theme.captures]\nkeyword = \"#ff8800\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, diags := config.Load(config.Options{UserPath: path})
	for _, d := range diags {
		t.Fatalf("clean config must not warn: %v", d)
	}

	m := New()
	m.SetPalette(theme.DefaultPalette())
	m.Configure(host.FromConfig(cfg))

	style, ok := m.hlTheme.Style("keyword")
	if !ok {
		t.Fatal("keyword must resolve to a style")
	}
	plain := New()
	plain.SetPalette(theme.DefaultPalette())
	base, _ := plain.hlTheme.Style("keyword")
	if style.GetForeground() == base.GetForeground() {
		t.Fatal("the override must change the keyword colour")
	}

	// Clearing the override falls back to the theme's own colour.
	if err := config.RemoveKey(config.Options{UserPath: path}, config.UserScope, "theme.captures.keyword"); err != nil {
		t.Fatal(err)
	}
	cleared, _ := config.Load(config.Options{UserPath: path})
	m.Configure(host.FromConfig(cleared))
	after, _ := m.hlTheme.Style("keyword")
	if after.GetForeground() != base.GetForeground() {
		t.Fatal("clearing the override must restore the theme colour")
	}
}
