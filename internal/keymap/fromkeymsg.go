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
	// Some terminals report the bracket keys as the literal glyphs; normalise to
	// the named bases the default set uses.
	switch s {
	case "[":
		return Key{Base: "left-bracket"}, true
	case "]":
		return Key{Base: "right-bracket"}, true
	}
	k, err := ParseKey(strings.TrimSpace(s))
	if err != nil {
		return Key{}, false
	}
	return k, true
}
