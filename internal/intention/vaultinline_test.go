package intention

import "testing"

// TestVaultInlineProvider is the applicability table of the inline vault
// actions (#2712): edit and decrypt over a `!vault |` block, encrypt over a
// plain mapping scalar — each only with a password source and a writable
// buffer, and never both groups at once.
func TestVaultInlineProvider(t *testing.T) {
	block := Context{Path: "/p/vars.yml", LangID: "yaml", VaultReady: true, VaultBlockAtCaret: true}
	scalar := Context{Path: "/p/vars.yml", LangID: "yaml", VaultReady: true, VaultScalarAtCaret: true}
	cases := []struct {
		name string
		cx   Context
		want []string
		not  []string
	}{
		{"block with source", block, []string{"vault.editValue", "vault.decryptValue"}, []string{"vault.encryptValue"}},
		{"scalar with source", scalar, []string{"vault.encryptValue"}, []string{"vault.editValue", "vault.decryptValue"}},
		{"block without source", Context{Path: "/p/vars.yml", VaultBlockAtCaret: true}, nil, []string{"vault.editValue", "vault.decryptValue"}},
		{"scalar without source", Context{Path: "/p/vars.yml", VaultScalarAtCaret: true}, nil, []string{"vault.encryptValue"}},
		{"read-only block", Context{Path: "/p/vars.yml", VaultReady: true, VaultBlockAtCaret: true, ReadOnly: true}, nil, []string{"vault.editValue", "vault.decryptValue"}},
		{"nothing at caret", Context{Path: "/p/vars.yml", VaultReady: true}, nil, []string{"vault.editValue", "vault.decryptValue", "vault.encryptValue"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ids(tc.cx)
			for _, id := range tc.want {
				if !has(got, id) {
					t.Errorf("%s not offered, got %v", id, got)
				}
			}
			for _, id := range tc.not {
				if has(got, id) {
					t.Errorf("%s offered, got %v", id, got)
				}
			}
		})
	}
}

// TestVaultToggleTravelsWithExplain: the conceal entry over a vault block
// carries the family's per-view toggle like every other family's.
func TestVaultToggleTravelsWithExplain(t *testing.T) {
	got := ids(Context{ConcealValue: true, ConcealFamily: "vault"})
	if !has(got, "editor.explainConceal") || !has(got, "view.toggleVaultStandIn") {
		t.Errorf("got %v, want the explain entry and the vault toggle", got)
	}
}
