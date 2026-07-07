package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteKeyPreservesUnrelatedContent(t *testing.T) {
	proj := writeProject(t, "[editor]\ntab_width = 2\n\n[custom]\nmystery = \"keep me\"\n")
	opts := Options{ProjectRoot: proj}
	if err := WriteKey(opts, ProjectScope, "editor.tab_width", 8); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(proj, dotDir, fileName))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "tab_width = 8") {
		t.Fatalf("key not updated: %s", s)
	}
	if !strings.Contains(s, "mystery = \"keep me\"") {
		t.Fatalf("unknown key must survive the round-trip: %s", s)
	}
	c, _ := Load(opts)
	if c.Editor.TabWidth != 8 {
		t.Fatalf("reload sees %d, want 8", c.Editor.TabWidth)
	}
}

func TestWriteKeyScopeRoutingAndFileCreation(t *testing.T) {
	userDir := t.TempDir()
	projRoot := t.TempDir()
	opts := Options{UserPath: filepath.Join(userDir, fileName), ProjectRoot: projRoot}

	if err := WriteKey(opts, UserScope, "theme.name", "tokyo-night"); err != nil {
		t.Fatal(err)
	}
	if err := WriteKey(opts, ProjectScope, "editor.tab_width", 3); err != nil {
		t.Fatal(err)
	}
	user, _ := os.ReadFile(opts.UserPath)
	proj, _ := os.ReadFile(filepath.Join(projRoot, dotDir, fileName))
	if !strings.Contains(string(user), "tokyo-night") || strings.Contains(string(user), "tab_width") {
		t.Fatalf("user layer wrong: %s", user)
	}
	if !strings.Contains(string(proj), "tab_width = 3") || strings.Contains(string(proj), "tokyo-night") {
		t.Fatalf("project layer wrong: %s", proj)
	}
	c, _ := Load(opts)
	if c.Theme.Name != "tokyo-night" || c.Editor.TabWidth != 3 {
		t.Fatalf("merged reload wrong: %+v %+v", c.Theme, c.Editor)
	}
}

func TestRemoveKeyResetsThroughLayers(t *testing.T) {
	userDir := t.TempDir()
	opts := Options{UserPath: filepath.Join(userDir, fileName)}
	if err := WriteKey(opts, UserScope, "editor.tab_width", 9); err != nil {
		t.Fatal(err)
	}
	if c, _ := Load(opts); c.Editor.TabWidth != 9 {
		t.Fatal("precondition: override applied")
	}
	if err := RemoveKey(opts, UserScope, "editor.tab_width"); err != nil {
		t.Fatal(err)
	}
	if c, _ := Load(opts); c.Editor.TabWidth != 4 {
		t.Fatalf("reset must fall back to the default 4, got %d", c.Editor.TabWidth)
	}
	// The emptied [editor] table is pruned, not left dangling.
	data, _ := os.ReadFile(opts.UserPath)
	if strings.Contains(string(data), "[editor]") {
		t.Fatalf("empty section must be pruned: %s", data)
	}
	// Removing from a missing file is a no-op.
	if err := RemoveKey(Options{UserPath: filepath.Join(userDir, "none", fileName)}, UserScope, "a.b"); err != nil {
		t.Fatalf("remove on missing file: %v", err)
	}
}

func TestWriteKeyRefusesBrokenFile(t *testing.T) {
	proj := writeProject(t, "not [valid toml =\n")
	err := WriteKey(Options{ProjectRoot: proj}, ProjectScope, "editor.tab_width", 2)
	if err == nil {
		t.Fatal("write over a broken file must fail, not destroy it")
	}
	data, _ := os.ReadFile(filepath.Join(proj, dotDir, fileName))
	if !strings.Contains(string(data), "not [valid toml") {
		t.Fatalf("broken file must be left untouched: %s", data)
	}
}

func TestWriteAndReloadDeliversFreshConfig(t *testing.T) {
	userDir := t.TempDir()
	opts := Options{UserPath: filepath.Join(userDir, fileName)}
	msg := WriteAndReload(opts, UserScope, "editor.scroll_off", 7)()
	rm, ok := msg.(ConfigReloadedMsg)
	if !ok {
		t.Fatalf("expected ConfigReloadedMsg, got %#v", msg)
	}
	if rm.Config.Editor.ScrollOff != 7 {
		t.Fatalf("reload must carry the written value, got %d", rm.Config.Editor.ScrollOff)
	}
	// A write error (no layer path) surfaces as a diagnostic, not a crash.
	msg = WriteAndReload(Options{}, ProjectScope, "editor.tab_width", 2)()
	if rm = msg.(ConfigReloadedMsg); len(rm.Diags) == 0 {
		t.Fatal("write failure must surface as a diagnostic")
	}
}

func TestDefaultScope(t *testing.T) {
	cases := map[string]Scope{
		"theme.name":                 UserScope,
		"keymap.preset":              UserScope,
		"editor.tab_width":           UserScope,
		"project.history":            ProjectScope,
		"lsp.servers.go.command":     ProjectScope,
		"toolchain.python.path":      ProjectScope,
		"notifications.min_severity": UserScope,
	}
	for key, want := range cases {
		if got := DefaultScope(key); got != want {
			t.Errorf("DefaultScope(%q) = %v, want %v", key, got, want)
		}
	}
}
