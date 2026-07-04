package lsp

import (
	"testing"

	"ike/internal/config"
	"ike/internal/lang"
)

// resolveSpec must merge the language plugin's baseline with the user's config
// overlay: config wins per field it sets, the baseline fills the rest. This is the
// "add a language = register it; config only overrides" contract.
func TestResolveSpecMergesBaselineAndOverride(t *testing.T) {
	lang.Register(lang.Language{
		ID: "faketest",
		Server: &lang.ServerSpec{
			Language:    "faketest",
			Command:     "base-cmd",
			Args:        []string{"--baseline"},
			RootMarkers: []string{".git"},
		},
	})

	c := &config.Config{}
	c.LSP.Enabled = true
	c.LSP.Servers = map[string]map[string]any{
		"faketest": {"command": "override-cmd"}, // override only the command
	}
	config.Set(c)

	spec, ok := resolveSpec("faketest")
	if !ok {
		t.Fatal("expected a resolved spec")
	}
	if spec.Command != "override-cmd" {
		t.Errorf("command: config should win, got %q", spec.Command)
	}
	if len(spec.Args) != 1 || spec.Args[0] != "--baseline" {
		t.Errorf("args: baseline should survive, got %v", spec.Args)
	}
}

func TestResolveSpecDisabledWhenLSPOff(t *testing.T) {
	c := &config.Config{}
	c.LSP.Enabled = false
	config.Set(c)
	if _, ok := resolveSpec("faketest"); ok {
		t.Error("resolveSpec should fail when LSP disabled")
	}
}
