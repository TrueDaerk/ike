package lsp

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/plugin"
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

// interpTC maps an explicit interpreter into settings (the python shape).
type interpTC struct{}

func (interpTC) Detect(string) (map[string]any, bool) { return nil, false }
func (interpTC) Explicit(p string) map[string]any {
	return map[string]any{"x": map[string]any{"path": p}}
}

// TestResolveSpecInjectsExplicitInterpreter guards the #94 seam: an explicit
// [lang.<id>] interpreter flows into the server settings via the toolchain's
// Explicit mapping and wins over colliding overlay settings.
func TestResolveSpecInjectsExplicitInterpreter(t *testing.T) {
	lang.Register(lang.Language{
		ID:        "interptest",
		Server:    &lang.ServerSpec{Language: "interptest", Command: "cmd"},
		Toolchain: interpTC{},
	})
	c := &config.Config{}
	c.LSP.Enabled = true
	c.Lang = map[string]map[string]string{"interptest": {"interpreter": "/proj/bin/x"}}
	config.Set(c)

	spec, ok := resolveSpec("interptest")
	if !ok {
		t.Fatal("expected a resolved spec")
	}
	x, _ := spec.Settings["x"].(map[string]any)
	if x == nil || x["path"] != "/proj/bin/x" {
		t.Fatalf("explicit interpreter missing from settings: %v", spec.Settings)
	}
}

// TestRestartRunsAsyncWithoutSynchronousSend guards #123: lsp.restart's Run
// resolves on the Update goroutine, so it must neither block on Shutdown nor
// call host.Send there — bubbletea's Send writes to an unbuffered channel only
// the event loop drains, so a synchronous Send from Update deadlocks the IDE.
func TestRestartRunsAsyncWithoutSynchronousSend(t *testing.T) {
	var restart plugin.Command
	for _, c := range (Plugin{}).Capabilities().Commands {
		if c.ID == "lsp.restart" {
			restart = c
		}
	}
	if restart.Run == nil {
		t.Fatal("lsp.restart must be registered")
	}
	h := host.New(nil)
	sent := 0
	h.SetSender(func(tea.Msg) { sent++ })

	cmd := restart.Run(h)
	if sent != 0 {
		t.Fatal("Run must not Send synchronously (deadlocks the Update loop)")
	}
	if cmd == nil {
		t.Fatal("Run must return the async restart command")
	}
	msg, ok := cmd().(ilsp.ServerStatusMsg)
	if !ok || msg.Kind != ilsp.ServerEventInfo {
		t.Fatalf("expected an info ServerStatusMsg from the command, got %#v", msg)
	}
	if sent != 0 {
		t.Fatal("the status must be returned as a message, never Sent")
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
