package intention

import "testing"

// TestVaultProvider is the applicability table of vault.treatAsFile (#2293):
// offered exactly for a writable file-backed buffer that is not vault-backed
// yet, and only while a password source is configured.
func TestVaultProvider(t *testing.T) {
	cases := []struct {
		name string
		cx   Context
		want bool
	}{
		{"plain file with password source", Context{Path: "/p/secrets.env", VaultReady: true}, true},
		{"no password source", Context{Path: "/p/secrets.env"}, false},
		{"already vault-backed", Context{Path: "/p/secrets.env", VaultReady: true, VaultBuffer: true}, false},
		{"fileless buffer", Context{Fileless: true, VaultReady: true}, false},
		{"read-only buffer", Context{Path: "/p/secrets.env", VaultReady: true, ReadOnly: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := has(ids(tc.cx), "vault.treatAsFile"); got != tc.want {
				t.Errorf("offered = %v, want %v", got, tc.want)
			}
		})
	}
}
