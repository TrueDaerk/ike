package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVaultPasswordFileValidation covers the ansible.vault_password_file
// diagnostics (#2293): a missing file is reported (the silent alternative is
// vault files opening as ciphertext with no explanation) but the value is
// kept — the file may appear later.
func TestVaultPasswordFileValidation(t *testing.T) {
	c := defaults()
	c.Ansible.VaultPasswordFile = filepath.Join(t.TempDir(), "does-not-exist")
	diags := validate(c)
	found := false
	for _, d := range diags {
		if d.Field == "ansible.vault_password_file" {
			found = true
			if !strings.Contains(d.Message, "not found") {
				t.Errorf("message = %q, want it to say the file is missing", d.Message)
			}
		}
	}
	if !found {
		t.Error("missing vault password file produced no diagnostic")
	}
	if c.Ansible.VaultPasswordFile == "" {
		t.Error("the configured value must survive validation")
	}
}

func TestVaultPasswordFileValidationAccepts(t *testing.T) {
	pass := filepath.Join(t.TempDir(), "vault-pass")
	if err := os.WriteFile(pass, []byte("pw\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"empty": "", "existing file": pass} {
		c := defaults()
		c.Ansible.VaultPasswordFile = value
		for _, d := range validate(c) {
			if d.Field == "ansible.vault_password_file" {
				t.Errorf("%s: unexpected diagnostic %q", name, d.Message)
			}
		}
	}
}

func TestVaultPasswordFileValidationRejectsDir(t *testing.T) {
	c := defaults()
	c.Ansible.VaultPasswordFile = t.TempDir()
	found := false
	for _, d := range validate(c) {
		if d.Field == "ansible.vault_password_file" && strings.Contains(d.Message, "directory") {
			found = true
		}
	}
	if !found {
		t.Error("a directory as vault password file produced no diagnostic")
	}
}
