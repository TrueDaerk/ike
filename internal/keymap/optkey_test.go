package keymap

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// optkey_test.go is the macOS Option-key ground truth (#2064): the exact byte
// sequences the supported terminals put on the wire for opt+<key>, driven
// through the full input path (raw bytes → ultraviolet's EventDecoder → the
// bubbletea key message → FromKeyMsg → a table lookup). Every mode that
// carries the modifier survives as an alt chord; the one mode that does not —
// a terminal composing the Option layer into a character — is pinned as the
// documented limitation it is.

// decodeKey runs raw terminal bytes through the real decoder and adapts the
// resulting key press, mirroring what the running program sees.
func decodeKey(t *testing.T, raw string) (Key, tea.KeyPressMsg) {
	t.Helper()
	var d uv.EventDecoder
	n, ev := d.Decode([]byte(raw))
	if n != len(raw) {
		t.Fatalf("%q: decoder consumed %d of %d bytes", raw, n, len(raw))
	}
	kp, ok := ev.(uv.KeyPressEvent)
	if !ok {
		t.Fatalf("%q: decoded %T, want uv.KeyPressEvent", raw, ev)
	}
	msg := tea.KeyPressMsg(kp)
	k, ok := FromKeyMsg(msg)
	if !ok {
		t.Fatalf("%q: FromKeyMsg rejected %q", raw, msg.String())
	}
	return k, msg
}

// TestOptionKeyEncodingsDecodeToAltChords walks the encodings a macOS terminal
// can produce for an Option-modified key once the Option key is configured to
// act as Alt/Meta (Terminal.app "Use Option as Meta Key", iTerm2 "Esc+",
// kitty `macos_option_as_alt`, Ghostty `macos-option-as-alt`, WezTerm
// `send_composed_key_when_left_alt_is_pressed = false`, Alacritty
// `window.option_as_alt`).
func TestOptionKeyEncodingsDecodeToAltChords(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		// ESC-prefix (meta) encoding — Terminal.app, iTerm2 "Esc+", and what
		// tmux forwards. The whole key sequence is prefixed, so F-keys and
		// named keys work the same way as letters and digits.
		{"esc-prefix opt+b", "\x1bb", "alt+b"},
		{"esc-prefix opt+9", "\x1b9", "alt+9"},
		{"esc-prefix opt+enter", "\x1b\r", "alt+enter"},
		{"esc-prefix ctrl+opt+h", "\x1b\x08", "ctrl+alt+h"},
		{"esc-prefix opt+shift+t", "\x1bT", "alt+shift+t"},
		{"esc-prefix opt+f7 (CSI-~ F-key)", "\x1b\x1b[18~", "alt+f7"},
		{"esc-prefix opt+f12", "\x1b\x1b[24~", "alt+f12"},
		{"esc-prefix opt+f1 (SS3 F-key)", "\x1b\x1bOP", "alt+f1"},

		// Legacy CSI-parameter encoding — the modifier bitset rides in the
		// sequence (shift=1, alt=2, ctrl=4, meta=8, offset by 1), which is how
		// every mainstream terminal sends modified arrows and F-keys.
		{"csi-param alt+f7", "\x1b[18;3~", "alt+f7"},
		{"csi-param alt+f1", "\x1b[1;3P", "alt+f1"},
		{"csi-param alt+up", "\x1b[1;3A", "alt+up"},
		{"csi-param alt+shift+up", "\x1b[1;4A", "alt+shift+up"},
		{"csi-param alt+shift+down", "\x1b[1;4B", "alt+shift+down"},
		{"csi-param cmd+alt+shift+right", "\x1b[1;12C", "cmd+alt+shift+right"},
		{"csi-param cmd+alt+f7", "\x1b[18;11~", "cmd+alt+f7"},

		// Kitty keyboard protocol (CSI u) — kitty, Ghostty, WezTerm and tmux
		// 3.4+ in passthrough. bubbletea always requests basic key
		// disambiguation, so these arrive whenever the terminal speaks it.
		{"kitty alt+b", "\x1b[98;3u", "alt+b"},
		{"kitty alt+1", "\x1b[49;3u", "alt+1"},
		{"kitty alt+shift+t", "\x1b[116;4u", "alt+shift+t"},
		{"kitty ctrl+alt+h", "\x1b[104;7u", "ctrl+alt+h"},
		{"kitty alt+enter", "\x1b[13;3u", "alt+enter"},
		{"kitty alt+slash", "\x1b[47;3u", "alt+/"},
		{"kitty alt+left-bracket", "\x1b[91;3u", "alt+left-bracket"},
		// macOS terminals report Cmd as the Kitty protocol's *super* bit;
		// the legacy encoding calls the same modifier meta. Both fold to
		// IKE's single Cmd-class modifier.
		{"kitty cmd+alt+b (super)", "\x1b[98;11u", "cmd+alt+b"},
		{"kitty cmd+alt+shift+c", "\x1b[99;12u", "cmd+alt+shift+c"},
		{"kitty alt+b with alternate key codes", "\x1b[98::98;3u", "alt+b"},
		// Lock states must not leak into the chord: caps lock (bit 64) and
		// num lock (bit 128) ride along in the same bitset.
		{"kitty alt+b with caps lock on", "\x1b[98;67u", "alt+b"},
		{"kitty alt+b with num lock on", "\x1b[98;131u", "alt+b"},
		// Report-associated-text (Kitty flag 16) repeats the produced text as
		// a third parameter. bubbletea's String() then returns that bare text
		// and swallows the modifiers — the regression this issue fixes.
		{"kitty alt+b with associated text", "\x1b[98;3;98u", "alt+b"},

		// xterm modifyOtherKeys (CSI 27 ; mod ; code ~).
		{"modifyOtherKeys alt+b", "\x1b[27;3;98~", "alt+b"},
		{"modifyOtherKeys alt+shift+b", "\x1b[27;4;66~", "alt+shift+b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k, msg := decodeKey(t, c.raw)
			if got := k.String(); got != c.want {
				t.Errorf("%q: got %q (bubbletea String %q, Keystroke %q), want %q",
					c.raw, got, msg.String(), msg.Keystroke(), c.want)
			}
		})
	}
}

// TestOptionChordsResolveToDefaultCommands closes the loop: the decoded chord
// is looked up in the darwin binding table and must reach the command the
// default set promises. This is the acceptance test for "alt+f7 fires Find
// usages on a Mac".
func TestOptionChordsResolveToDefaultCommands(t *testing.T) {
	table := BuildTable(DefaultsFor(PresetJetBrains, "darwin"), nil, "darwin")
	cases := []struct {
		raw  string
		ctx  Context
		want string
	}{
		{"\x1b\x1b[18~", Editor, "lsp.references"},            // Terminal.app opt+F7
		{"\x1b[18;3~", Editor, "lsp.references"},              // kitty/legacy alt+F7
		{"\x1b\x1bOP", Global, "explorer.reveal"},             // Terminal.app opt+F1
		{"\x1b[116;4;84u", Global, "editor.tab.reopenClosed"}, // kitty alt+shift+t reporting text
		{"\x1b1", Global, "editor.tab.select1"},               // esc-prefix opt+1
		{"\x1b[49;3u", Global, "editor.tab.select1"},          // kitty alt+1
		{"\x1b\r", Editor, "lsp.codeAction"},                  // opt+enter
		{"\x1b\x08", Editor, "lsp.callHierarchy"},             // ctrl+opt+h
		{"\x1bT", Global, "editor.tab.reopenClosed"},          // opt+shift+T
		{"\x1b[98;11u", Editor, "lsp.implementations"},        // kitty cmd+opt+b
		{"\x1b\x1b[24~", Global, "terminal.toggle"},           // opt+F12
		{"\x1b[1;3A", Editor, "editor.selection.extend"},      // alt+up
		{"\x1b[1;4A", Editor, "editor.caret.addAbove"},        // alt+shift+up
	}
	for _, c := range cases {
		k, _ := decodeKey(t, c.raw)
		b, found := table.Lookup(Chord{Steps: []Key{k}}, c.ctx)
		if !found {
			t.Errorf("%q (%s): no binding in context %v", c.raw, k.String(), c.ctx)
			continue
		}
		if b.Command != c.want {
			t.Errorf("%q (%s) = %q, want %q", c.raw, k.String(), b.Command, c.want)
		}
	}
}

// TestModifiersSurviveReportedText is the unit-level guard for the same bug,
// independent of the wire format: whenever a chord modifier is set, the
// reported text must not be able to mask it. Terminals reach this shape via
// the Kitty protocol's report-associated-text flag and via the Windows
// Console API, both of which populate Key.Text alongside the modifiers.
func TestModifiersSurviveReportedText(t *testing.T) {
	cases := []struct {
		msg  tea.KeyPressMsg
		base string
		mods Mod
	}{
		{tea.KeyPressMsg{Text: "b", Code: 'b', Mod: tea.ModAlt}, "b", ModAlt},
		{tea.KeyPressMsg{Text: "1", Code: '1', Mod: tea.ModAlt}, "1", ModAlt},
		{tea.KeyPressMsg{Text: "b", Code: 'b', Mod: tea.ModCtrl}, "b", ModCtrl},
		{tea.KeyPressMsg{Text: "b", Code: 'b', Mod: tea.ModSuper | tea.ModAlt}, "b", ModMeta | ModAlt},
		{tea.KeyPressMsg{Text: "T", Code: 't', Mod: tea.ModAlt | tea.ModShift}, "t", ModAlt | ModShift},
		{tea.KeyPressMsg{Text: "[", Code: '[', Mod: tea.ModAlt}, "left-bracket", ModAlt},
		// Shift alone leaves the text authoritative: "?" stays the glyph
		// instead of becoming "shift+/" (#284's rule), so the shift bit is
		// already spent producing the character and is not re-applied.
		{tea.KeyPressMsg{Text: "?", Code: '/', ShiftedCode: '?', Mod: tea.ModShift}, "?", 0},
	}
	for _, c := range cases {
		k, ok := FromKeyMsg(c.msg)
		if !ok {
			t.Errorf("FromKeyMsg(%+v): ok=false", c.msg)
			continue
		}
		if k.Base != c.base || k.Mods != c.mods {
			t.Errorf("FromKeyMsg(%+v) = %+v, want base=%q mods=%d", c.msg, k, c.base, c.mods)
		}
	}
}

// TestOSClassModifiersFoldToMeta pins the Cmd-class folding. The Kitty
// protocol can report meta, super and hyper; IKE models one Cmd-class
// modifier, and every one of them has to stay parseable — an unknown token
// would make FromKeyMsg drop the event instead of resolving a chord.
func TestOSClassModifiersFoldToMeta(t *testing.T) {
	for _, mod := range []tea.KeyMod{tea.ModMeta, tea.ModSuper, tea.ModHyper} {
		k, ok := FromKeyMsg(tea.KeyPressMsg{Code: 'b', Mod: mod | tea.ModAlt})
		if !ok {
			t.Fatalf("mod %v: FromKeyMsg dropped the event", mod)
		}
		if got := k.String(); got != "cmd+alt+b" {
			t.Errorf("mod %v = %q, want cmd+alt+b", mod, got)
		}
	}
}

// TestComposedOptionCharacterCarriesNoModifier documents the limitation IKE
// cannot fix. With the Option key left in its default macOS role the terminal
// composes the character *before* any escape sequence exists: opt+b is the
// literal rune "∫" with an empty modifier set, indistinguishable from the user
// typing "∫". No alt binding can match it — the fix is a terminal setting, and
// the wiki's troubleshooting section names it per terminal.
func TestComposedOptionCharacterCarriesNoModifier(t *testing.T) {
	for _, raw := range []string{"∫", "ƒ", "¡", "å"} {
		k, msg := decodeKey(t, raw)
		if k.Has(ModAlt) {
			t.Errorf("%q: reported ModAlt — the composed rune carries no modifier info", raw)
		}
		if msg.Mod != 0 {
			t.Errorf("%q: bubbletea reported Mod=%v, want none", raw, msg.Mod)
		}
		if k.Base != raw {
			t.Errorf("%q: base = %q, want the literal rune", raw, k.Base)
		}
	}
}
