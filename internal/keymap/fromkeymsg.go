package keymap

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// FromKeyMsg adapts a bubbletea key event into our Key model. bubbletea's
// String() already encodes modifiers as ctrl+/alt+/shift+ tokens and names
// special keys (esc, tab, enter, f7, left, …), so we reuse ParseKey on it. A
// bare space is reported as " " and mapped to the "space" base. It reports
// ok=false for events that carry no resolvable key (e.g. empty strings).
func FromKeyMsg(msg tea.KeyMsg) (Key, bool) {
	s := msg.String()
	if s == "" {
		return Key{}, false
	}
	if s == " " {
		return Key{Base: "space"}, true
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
