package lsp

import (
	"testing"

	"ike/internal/config"
	"ike/internal/lang"
)

// TestResolveSpecHonorsPerServerEnable guards the settings page's per-server
// switch (#130): [lsp.servers.<id>] enabled = false yields no spec while the
// subsystem stays on; absent or true keeps the baseline resolving.
func TestResolveSpecHonorsPerServerEnable(t *testing.T) {
	lang.Register(lang.Language{
		ID:     "enabletest",
		Server: &lang.ServerSpec{Language: "enabletest", Command: "enable-ls"},
	})

	c := &config.Config{}
	c.LSP.Enabled = true
	c.LSP.Servers = map[string]map[string]any{"enabletest": {"enabled": false}}
	config.Set(c)
	if _, ok := resolveSpec("enabletest"); ok {
		t.Fatal("enabled=false must suppress the server")
	}

	c.LSP.Servers["enabletest"]["enabled"] = true
	config.Set(c)
	if spec, ok := resolveSpec("enabletest"); !ok || spec.Command != "enable-ls" {
		t.Fatalf("enabled=true must resolve the baseline, got %v %v", spec, ok)
	}

	delete(c.LSP.Servers, "enabletest")
	config.Set(c)
	if _, ok := resolveSpec("enabletest"); !ok {
		t.Fatal("no overlay at all must keep the server enabled")
	}
}

// TestResolveSpecRespectsPluginToggle guards #133: a disabled lang.<id>
// plugin takes its language server with it.
func TestResolveSpecRespectsPluginToggle(t *testing.T) {
	lang.Register(lang.Language{
		ID:     "toggletest",
		Server: &lang.ServerSpec{Language: "toggletest", Command: "toggle-ls"},
	})
	c, _ := config.Load(config.Options{})
	c.Plugins = map[string]map[string]any{"lang-toggletest": {"enabled": false}}
	config.Set(c)
	t.Cleanup(func() { fresh, _ := config.Load(config.Options{}); config.Set(fresh) })

	if _, ok := resolveSpec("toggletest"); ok {
		t.Fatal("a disabled lang-toggletest plugin must disable its server")
	}
	c.Plugins["lang-toggletest"]["enabled"] = true
	config.Set(c)
	if _, ok := resolveSpec("toggletest"); !ok {
		t.Fatal("re-enabling the plugin must restore the server")
	}
}
