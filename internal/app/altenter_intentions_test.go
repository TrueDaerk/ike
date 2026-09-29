package app

import (
	"testing"

	"ike/internal/config"
	"ike/internal/keydoctor"
	"ike/internal/keymap"
	ilsp "ike/internal/lsp"
	"ike/internal/palette"
)

// altenter_intentions_test.go guards #2820: alt+enter with the editor focused
// on a JSON buffer opens the intention popup with the jq-path built-in, even
// though the explorer binds the same chord (#2805).

// pressAltEnterForIntentions feeds the real alt+enter keypress and plays the
// LSP bridge's reply: lsp.codeAction must be what the chord dispatched, and
// the bridge answers it with an Intentions offer (empty here — no server)
// that the app merges with the built-ins.
func pressAltEnterForIntentions(t *testing.T, m Model) Model {
	t.Helper()
	out, cmd := m.Update(altEnter())
	m = out.(Model)
	if m.ctxMenu.IsOpen() {
		t.Fatal("alt+enter in the editor must not open the explorer menu")
	}
	dispatched := false
	for _, msg := range cmdMsgs(cmd) {
		if e, ok := msg.(CommandExecutedMsg); ok && e.ID == "lsp.codeAction" {
			dispatched = true
		}
	}
	if !dispatched {
		t.Fatal("alt+enter in a JSON buffer must dispatch lsp.codeAction")
	}
	out, _ = m.Update(ilsp.CodeActionsMsg{Path: m.activeEditor().Path(), Intentions: true})
	return out.(Model)
}

func assertJQPathOffered(t *testing.T, m Model) {
	t.Helper()
	if !m.palette.IsOpen() || !m.palette.Anchored() {
		t.Fatal("alt+enter must open the anchored intention popup")
	}
	for _, it := range m.actions.Results("", palette.Context{}) {
		if it.Title == "Copy Path as jq Expression" {
			return
		}
	}
	t.Fatal("the intention popup must offer Copy Path as jq Expression")
}

// TestAltEnterOpensIntentionsInJSONBuffer: the default keymap, the real
// keypress, the popup with the jq-path intention.
func TestAltEnterOpensIntentionsInJSONBuffer(t *testing.T) {
	m := intentionModel(t, "x.json", `{"name": "value"}`, 0, 11)
	assertJQPathOffered(t, pressAltEnterForIntentions(t, m))
}

// TestAltEnterSurvivesExplorerRebind is the #2820 regression: moving the
// explorer's alt+enter elsewhere (the keymap doctor offers it, alt+enter being
// at risk in many terminals) used to write the flat keymap.bindings.alt+enter
// = "" — which unbound the editor's lsp.codeAction too, so alt+enter did
// nothing in any buffer.
func TestAltEnterSurvivesExplorerRebind(t *testing.T) {
	m := intentionModel(t, "x.json", `{"name": "value"}`, 0, 11)
	orig := config.Get()
	t.Cleanup(func() { config.Set(orig) })
	fresh := keymap.MustParseChord("ctrl+alt+shift+f9")
	out, cmd := m.Update(keydoctor.RebindMsg{
		Command: "explorer.contextMenu",
		Context: keymap.Explorer,
		Old:     keymap.MustParseChord("alt+enter"),
		New:     fresh,
	})
	m = out.(Model)
	reload := runForReload(t, cmd)
	if _, flat := reload.Config.Keymap.Bindings["alt+enter"]; flat {
		t.Fatal("the rebind must not write the flat alt+enter key")
	}
	out, _ = m.Update(reload)
	m = out.(Model)
	if b, ok := m.bindings.Table().Lookup(fresh, keymap.Explorer); !ok || b.Command != "explorer.contextMenu" {
		t.Fatalf("explorer menu on %s = %+v (ok=%v)", fresh, b, ok)
	}
	assertJQPathOffered(t, pressAltEnterForIntentions(t, m))
}
