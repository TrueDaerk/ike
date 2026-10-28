package explorer

import (
	"testing"

	"ike/internal/plugin"
)

// TestDeleteAliasKeymaps pins the JetBrains delete aliases (#3087): each
// chord is explorer-scoped and dispatches explorer.delete.
func TestDeleteAliasKeymaps(t *testing.T) {
	kms := corePlugin{}.Capabilities().Keymaps
	for _, chord := range []string{"d", "cmd+backspace", "ctrl+backspace", "delete"} {
		t.Run(chord, func(t *testing.T) {
			for _, km := range kms {
				if km.Keys == chord {
					if km.CommandID != "explorer.delete" {
						t.Fatalf("%s -> %s, want explorer.delete", chord, km.CommandID)
					}
					if km.Scope != plugin.PaneScope(ctxID) {
						t.Fatalf("%s scope = %v, want explorer pane", chord, km.Scope)
					}
					return
				}
			}
			t.Fatalf("no keymap for %s", chord)
		})
	}
}
