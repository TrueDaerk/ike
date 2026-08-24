package keymap

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// textMaskingMods are the modifiers whose presence makes a key press a *chord*
// rather than typing. bubbletea's Key.String() prefers Key.Text — the literal
// characters the terminal produced — over the keystroke form, which is right
// for unmodified typing ("?" rather than "shift+/") but hides the modifier set
// whenever a terminal reports associated text alongside a modified key. That
// happens with the Kitty protocol's "report associated text" flag and on the
// Windows Console API: opt+b then arrives as a bare "b" and no alt+b binding
// could ever match (#2064).
const textMaskingMods = tea.ModCtrl | tea.ModAlt | tea.ModMeta | tea.ModSuper | tea.ModHyper

// FromKeyMsg adapts a bubbletea key press into our Key model. bubbletea
// already encodes modifiers as ctrl+/alt+/shift+/meta+/super+/hyper+ tokens and
// names special keys (esc, tab, enter, f7, left, space, …), so we reuse
// ParseKey on its string form — String() while the press is plain typing,
// Keystroke() as soon as a chord modifier is involved, so the modifiers can
// never be masked by the reported text. It reports ok=false for events that
// carry no resolvable key (e.g. empty strings).
func FromKeyMsg(msg tea.KeyPressMsg) (Key, bool) {
	s := msg.String()
	if msg.Mod&textMaskingMods != 0 {
		s = msg.Keystroke()
	}
	if s == "" {
		return Key{}, false
	}
	// Bracket-glyph normalization lives in ParseKey's baseAlias (#284), so
	// modified presses ("cmd+[") and bare ones canonicalise identically.
	k, err := ParseKey(strings.TrimSpace(s))
	if err != nil {
		return Key{}, false
	}
	return k, true
}
