package keymap

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// FromKeyMsg adapts a bubbletea key press into our Key model. bubbletea's
// String() already encodes modifiers as ctrl+/alt+/shift+ tokens and names
// special keys (esc, tab, enter, f7, left, space, …), so we reuse ParseKey on
// it. It reports ok=false for events that carry no resolvable key (e.g. empty
// strings).
func FromKeyMsg(msg tea.KeyPressMsg) (Key, bool) {
	s := msg.String()
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
