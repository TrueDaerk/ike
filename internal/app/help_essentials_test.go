package app

import (
	"testing"

	"ike/internal/help"
	"ike/internal/registry"
)

// TestEssentialsCurationResolves is the curation-drift guard (#656): every
// command ID in the hand-curated Essentials spec must resolve against the
// real global registry, so a command rename breaks CI instead of silently
// dropping a row from the default help view.
func TestEssentialsCurationResolves(t *testing.T) {
	known := map[string]bool{}
	for _, c := range registry.Global().Commands() {
		known[c.ID] = true
	}
	for _, id := range help.EssentialIDs() {
		if !known[id] {
			t.Errorf("essentials curation references unregistered command %q", id)
		}
	}
}
