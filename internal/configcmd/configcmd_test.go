package configcmd

import (
	"path/filepath"
	"testing"

	"ike/internal/config"
)

func TestWriteAndReloadDeliversFreshConfig(t *testing.T) {
	userDir := t.TempDir()
	opts := config.Options{UserPath: filepath.Join(userDir, "config.toml")}
	msg := WriteAndReload(opts, config.UserScope, "editor.scroll_off", 7)()
	rm, ok := msg.(config.ConfigReloadedMsg)
	if !ok {
		t.Fatalf("expected ConfigReloadedMsg, got %#v", msg)
	}
	if rm.Config.Editor.ScrollOff != 7 {
		t.Fatalf("reload must carry the written value, got %d", rm.Config.Editor.ScrollOff)
	}
	// A write error (no layer path) surfaces as a diagnostic, not a crash.
	msg = WriteAndReload(config.Options{}, config.ProjectScope, "editor.tab_width", 2)()
	if rm = msg.(config.ConfigReloadedMsg); len(rm.Diags) == 0 {
		t.Fatal("write failure must surface as a diagnostic")
	}
}

func TestApplyAndReloadWritesAndRemovesOnce(t *testing.T) {
	userDir := t.TempDir()
	opts := config.Options{UserPath: filepath.Join(userDir, "config.toml")}
	if err := config.WriteKey(opts, config.UserScope, "editor.tab_width", 2); err != nil {
		t.Fatal(err)
	}
	msg := ApplyAndReload(opts, []config.Mutation{
		{Scope: config.UserScope, Key: "editor.scroll_off", Value: 9},
		{Scope: config.UserScope, Key: "editor.tab_width", Remove: true},
	})()
	rm := msg.(config.ConfigReloadedMsg)
	if rm.Config.Editor.ScrollOff != 9 {
		t.Fatalf("scroll_off = %d, want 9", rm.Config.Editor.ScrollOff)
	}
	if rm.Config.Editor.TabWidth == 2 {
		t.Fatal("removed key must fall back to the default")
	}
	if len(rm.Diags) != 0 {
		t.Fatalf("unexpected diags: %v", rm.Diags)
	}
}
