package app

import (
	"testing"

	"ike/internal/keymap"
	"ike/internal/registry"

	// The compiled-in plugins register their commands via init(), mirroring
	// cmd/ike; without these imports the registry would under-report and the
	// audit below would flag live bindings as dead.
	_ "ike/plugins/lsp"
)

// TestNoSilentlyDeadDefaultBindings is the 0081/20 acceptance test: every
// default binding's command id must either be registered (the binding is
// live) or appear in the keymap blocked ledger with its dependency recorded.
// An id that is neither is a silently-dead binding; an id that is both means
// the ledger went stale when the command landed.
func TestNoSilentlyDeadDefaultBindings(t *testing.T) {
	reg := registry.Global()
	for _, b := range keymap.Defaults(keymap.PresetJetBrains) {
		_, registered := reg.Command(b.Command)
		reason, blocked := keymap.BlockedReason(b.Command)
		switch {
		case registered && blocked:
			t.Errorf("%s (%s): registered but still in the blocked ledger (%s) — remove the ledger entry", b.Command, b.Chord.String(), reason)
		case !registered && !blocked:
			t.Errorf("%s (%s): not registered and not in the blocked ledger — register, alias, or document the dependency", b.Command, b.Chord.String())
		}
	}
}
