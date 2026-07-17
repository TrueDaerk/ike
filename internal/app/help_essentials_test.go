package app

import (
	"strings"
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

// TestDocShortcutHintsArePlatformNeutral (#678): the help sheet's fallback
// Shortcut hints (plugin.Command.Shortcut) bypass keymap platform
// normalization, so a mac-flavoured "cmd+…"/"opt+…" hint would render wrong
// off macOS. Doc hints must stay platform-neutral — vim keys (":w", "u"),
// function keys, or chords that exist on every platform ("ctrl+r"). Anything
// platform-specific belongs in the keymap layer, which normalizes.
func TestDocShortcutHintsArePlatformNeutral(t *testing.T) {
	for _, c := range registry.Global().Commands() {
		if strings.Contains(c.Shortcut, "cmd+") || strings.Contains(c.Shortcut, "opt+") {
			t.Errorf("command %q doc shortcut hint %q is mac-specific; bind it in the keymap layer or use a neutral hint", c.ID, c.Shortcut)
		}
	}
}
