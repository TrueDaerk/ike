package app

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/config"
	"ike/internal/project"
	"ike/internal/watch"
)

// switch_settings_test.go covers 0380 #795: project settings overrides apply
// on a project switch and drop on the way out, and an external edit of
// .ike/settings.toml reloads the config live.

// writeProjectSettings drops a .ike/settings.toml into root.
func writeProjectSettings(t *testing.T, root, content string) string {
	t.Helper()
	dir := filepath.Join(root, ".ike")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestSwitchAppliesAndDropsProjectOverrides (#795): switching into a project
// applies its .ike/settings.toml over the global layer; switching on to a
// project without one drops the override again.
func TestSwitchAppliesAndDropsProjectOverrides(t *testing.T) {
	restore := config.Get()
	t.Cleanup(func() { config.Set(restore) })
	base := t.TempDir()
	src, with, without := filepath.Join(base, "src"), filepath.Join(base, "with"), filepath.Join(base, "without")
	for _, d := range []string{src, with, without} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeProjectSettings(t, with, "[editor]\ntab_width = 13\n")
	t.Chdir(src)
	m := switchModel(t)

	out, _ := m.Update(project.SwitchProjectMsg{Root: with})
	m = out.(Model)
	if got := config.Get().Editor.TabWidth; got != 13 {
		t.Fatalf("switch must apply the project override, tab_width = %d", got)
	}
	if got := config.Origin(config.Discover("."), "editor.tab_width"); got != "project" {
		t.Fatalf("origin in the override project = %q, want project", got)
	}

	out, _ = m.Update(project.SwitchProjectMsg{Root: without})
	m = out.(Model)
	if got := config.Get().Editor.TabWidth; got == 13 {
		t.Fatal("switch must drop the outgoing project's override")
	}
	if got := config.Origin(config.Discover("."), "editor.tab_width"); got == "project" {
		t.Fatal("origin must no longer be project after the switch")
	}
	_ = m
}

// TestExternalSettingsEditReloadsConfig (#795): a watcher ConfigChanged event
// re-runs the reload pipeline, applying the edited project settings without
// restart.
func TestExternalSettingsEditReloadsConfig(t *testing.T) {
	restore := config.Get()
	t.Cleanup(func() { config.Set(restore) })
	root := t.TempDir()
	t.Chdir(root)
	m := switchModel(t)
	m.cfgOpts = config.Discover(".")

	path := writeProjectSettings(t, root, "[editor]\ntab_width = 11\n")
	out, cmd := m.Update(watch.EventMsg{Kind: watch.ConfigChanged, Path: path})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("ConfigChanged must return the reload command")
	}
	msg, ok := cmd().(config.ConfigReloadedMsg)
	if !ok {
		t.Fatalf("reload must deliver ConfigReloadedMsg, got %#v", msg)
	}
	out, _ = m.Update(msg)
	m = out.(Model)
	if got := config.Get().Editor.TabWidth; got != 11 {
		t.Fatalf("external edit must apply live, tab_width = %d", got)
	}
	_ = m
}
