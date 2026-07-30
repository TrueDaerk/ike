package langansible

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/format"
)

// TestAnsibleDefaults (#1405): prettier's YAML parser first, yamlfmt as the
// fallback.
func TestAnsibleDefaults(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "yamlfmt"), []byte("#!/bin/sh\ncat\n"), 0o755)
	t.Setenv("PATH", dir)
	prov, ok := format.Resolve("ansible", "playbook.yml")
	if !ok || prov.DisplayName("playbook.yml") != "yamlfmt" {
		t.Fatalf("yamlfmt fallback expected, got ok=%v name=%q", ok, prov.DisplayName("playbook.yml"))
	}
	os.WriteFile(filepath.Join(dir, "prettier"), []byte("#!/bin/sh\ncat\n"), 0o755)
	prov, _ = format.Resolve("ansible", "playbook.yml")
	if prov.DisplayName("playbook.yml") != "prettier" {
		t.Fatalf("prettier must win when installed, got %q", prov.DisplayName("playbook.yml"))
	}
	if def, ok := format.ExternalDefault("ansible"); !ok || def.Command != "prettier" {
		t.Fatalf("settings-page default: %+v ok=%v", def, ok)
	}
}
