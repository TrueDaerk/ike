package lsp

// doctor_test.go guards the lsp.doctor entry point (#2164): the command is
// registered globally, and doctorServers hands the doctor the effective specs
// — resolved through the same overlay chain the manager launches with, with
// delegating languages collapsed onto their server language.

import (
	"testing"

	"ike/internal/config"
	"ike/internal/lang"
	"ike/internal/plugin"
)

// TestDoctorCommandRegistered: lsp.doctor exists as a global palette command.
func TestDoctorCommandRegistered(t *testing.T) {
	var cmd plugin.Command
	for _, c := range (Plugin{}).Capabilities().Commands {
		if c.ID == "lsp.doctor" {
			cmd = c
		}
	}
	if cmd.Run == nil {
		t.Fatal("lsp.doctor must be registered")
	}
	if cmd.Title != "LSP: Doctor" {
		t.Fatalf("title = %q", cmd.Title)
	}
	if cmd.Scope != plugin.GlobalScope() {
		t.Fatalf("scope = %#v, want global scope", cmd.Scope)
	}
}

// TestDoctorServersUsesEffectiveSpecs: the probe set carries the resolved
// command (config overlay wins) and the install recipe, and skips languages
// without a launchable server.
func TestDoctorServersUsesEffectiveSpecs(t *testing.T) {
	lang.Register(lang.Language{
		ID: "doctest",
		Server: &lang.ServerSpec{
			Command: "doctest-ls",
			Args:    []string{"--stdio"},
			Install: []string{"npm", "install", "-g", "doctest-ls"},
		},
	})
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	c := &config.Config{}
	c.LSP.Enabled = true
	c.LSP.Servers = map[string]map[string]any{
		"doctest": {"command": "custom-doctest-ls"},
	}
	config.Set(c)

	var found bool
	for _, s := range doctorServers() {
		if s.Lang != "doctest" {
			continue
		}
		found = true
		if s.Command != "custom-doctest-ls" {
			t.Fatalf("command = %q, want the config override", s.Command)
		}
		if len(s.Install) == 0 || s.Install[0] != "npm" {
			t.Fatalf("install recipe missing: %+v", s.Install)
		}
		if s.Root == "" {
			t.Fatal("workspace root must be set")
		}
	}
	if !found {
		t.Fatal("doctorServers must include the registered language")
	}
}
