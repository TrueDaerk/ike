package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/host"
	"ike/internal/palette"
	"ike/internal/registry"
	"ike/internal/sshconf"
)

// TestSSHModeResults verifies the picker rows: the alias is the title, the
// config's target the detail, and activating a row carries the alias.
func TestSSHModeResults(t *testing.T) {
	mode := newSSHMode()
	mode.entries = []sshconf.Host{
		{Alias: "build01", HostName: "build01.example.com", User: "ci", Port: "2222"},
		{Alias: "db-primary"},
	}
	items := mode.Results("", palette.Context{})
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].Title != "build01" || items[0].Detail != "ci@build01.example.com:2222" {
		t.Fatalf("row = %+v", items[0])
	}
	if items[1].Detail != "" {
		t.Fatalf("a bare Host block has no detail: %q", items[1].Detail)
	}
	picked, ok := items[0].Msg.(SSHHostPickedMsg)
	if !ok || picked.Host != "build01" {
		t.Fatalf("activation msg = %+v", items[0].Msg)
	}

	// Fuzzy filtering matches over the alias.
	items = mode.Results("db", palette.Context{})
	if len(items) != 1 || items[0].Title != "db-primary" {
		t.Fatalf("filtered items = %+v", items)
	}
}

// TestSSHModePlaceholderHint verifies the empty-picker hint: nothing to list
// explains where hosts come from instead of showing a bare prompt.
func TestSSHModePlaceholderHint(t *testing.T) {
	mode := newSSHMode()
	if got := mode.Placeholder(); !strings.Contains(got, "ssh/config") || !strings.Contains(got, "terminal.ssh_hosts") {
		t.Fatalf("empty placeholder = %q, want the hint", got)
	}
	mode.entries = []sshconf.Host{{Alias: "web"}}
	if got := mode.Placeholder(); strings.Contains(got, "terminal.ssh_hosts") {
		t.Fatalf("filled placeholder = %q, want the plain prompt", got)
	}
}

// TestSSHArgvAndLabel verifies the command assembly: the alias reaches ssh as
// one verbatim argument, and the chrome label names the host.
func TestSSHArgvAndLabel(t *testing.T) {
	argv := sshArgv("build01")
	if len(argv) != 2 || argv[0] != "ssh" || argv[1] != "build01" {
		t.Fatalf("argv = %v, want [ssh build01]", argv)
	}
	if got := sshLabel("build01"); !strings.Contains(got, "build01") {
		t.Fatalf("label = %q, want the host name in it", got)
	}
}

// TestSSHHostsMergesConfigAndSetting verifies the picker's list: ~/.ssh/config
// first, then the terminal.ssh_hosts extras, duplicates folded into the
// ssh-config entry that carries the detail.
func TestSSHHostsMergesConfigAndSetting(t *testing.T) {
	m := NewWith(registry.New(), host.MapConfig{"terminal.ssh_hosts": "monitor, build01 , ,"})
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := "Host *\n  ForwardAgent yes\n\nHost build01\n  HostName build01.example.com\n"
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	hosts := m.sshHosts()
	if len(hosts) != 2 {
		t.Fatalf("hosts = %+v, want the config host plus one extra", hosts)
	}
	if hosts[0].Alias != "build01" || hosts[0].HostName != "build01.example.com" {
		t.Fatalf("config host = %+v", hosts[0])
	}
	if hosts[1].Alias != "monitor" || hosts[1].HostName != "" {
		t.Fatalf("extra host = %+v", hosts[1])
	}
}

// TestSSHHostsWithoutConfig verifies the degraded path: no ssh config at all
// leaves the extras (here: none), never an error.
func TestSSHHostsWithoutConfig(t *testing.T) {
	m := NewWith(registry.New(), host.MapConfig{})
	t.Setenv("HOME", t.TempDir())
	if hosts := m.sshHosts(); len(hosts) != 0 {
		t.Fatalf("hosts = %+v, want none", hosts)
	}
}

// TestOpenSSHTerminalIgnoresEmptyHost verifies that an empty pick spawns
// nothing.
func TestOpenSSHTerminalIgnoresEmptyHost(t *testing.T) {
	m := NewWith(registry.New(), host.MapConfig{})
	before := len(m.activeWS().Panes.Keys())
	tm, _ := m.Update(SSHHostPickedMsg{})
	m = tm.(Model)
	if got := len(m.activeWS().Panes.Keys()); got != before {
		t.Fatalf("panes = %d, want the unchanged %d", got, before)
	}
}
