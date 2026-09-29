package settings

import (
	"testing"

	"ike/internal/config"
	"ike/internal/keymap"
)

// TestPlainRebindOfASharedDefaultSparesTheOtherContext guards #2820: a plain
// rebind of the explorer's alt+enter (#2805) unbinds the old chord in the
// explorer alone — the flat keymap.bindings.alt+enter = "" it used to write
// dropped the editor's lsp.codeAction too.
func TestPlainRebindOfASharedDefaultSparesTheOtherContext(t *testing.T) {
	k, opts := keymapPage(t)
	var row keymapRow
	for i, r := range k.rows() {
		if r.Command == "explorer.contextMenu" && r.Chord.String() == "alt+enter" {
			k.sel, row = i, r
		}
	}
	if row.Command == "" {
		t.Fatal("precondition: explorer.contextMenu is listed on alt+enter")
	}
	free := k.suggestChords(1)[0]
	c := newKeymapCapture(k, k.host.(*stubHost), row)
	c.steps = keymap.MustParseChord(free).Steps
	apply(t, c.confirm())

	if _, flat := config.Get().Keymap.Bindings["alt+enter"]; flat {
		t.Fatal("the rebind must not write the flat alt+enter key")
	}
	if got := config.Origin(opts, "keymap.bindings.explorer.alt+enter"); got != "user" {
		t.Fatalf("the qualified unbind must persist, origin = %q", got)
	}
	table := k.table()
	chord := keymap.MustParseChord("alt+enter")
	if nb, ok := table.Lookup(chord, keymap.Editor); !ok || nb.Command != "lsp.codeAction" {
		t.Fatalf("editor alt+enter = %+v ok=%v, want lsp.codeAction untouched", nb, ok)
	}
	if nb, ok := table.Lookup(chord, keymap.Explorer); ok && nb.Command == "explorer.contextMenu" {
		t.Fatal("the explorer's old chord must be released")
	}
	if nb, ok := table.Lookup(keymap.MustParseChord(free), keymap.Explorer); !ok || nb.Command != "explorer.contextMenu" {
		t.Fatalf("explorer %s = %+v ok=%v, want explorer.contextMenu", free, nb, ok)
	}
}
