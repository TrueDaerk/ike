package keymap

import (
	tea "charm.land/bubbletea/v2"
)

// frommouse.go maps the dedicated mouse navigation buttons (#816) onto the
// synthetic single-step chords the default table binds to nav.back /
// nav.forward. The mapping is shared by the app's mouse dispatch and the
// terminal probe (cmd/keyprobe), so "does this terminal report buttons 4/5"
// is answered against the very same translation the editor uses.

// MouseBackBase / MouseForwardBase are the synthetic chord bases for the
// X1/X2 mouse buttons. They are ordinary binding keys: `keymap.bindings` may
// rebind them like any chord.
const (
	MouseBackBase    = "mouse-back"
	MouseForwardBase = "mouse-forward"
)

// FromMouseButton adapts a bubbletea mouse button into our Key model. Only
// the two navigation buttons map; every other button (left/middle/right,
// wheel, buttons 10+) keeps its pane routing and reports ok=false.
//
// Terminals that do not report SGR extended buttons simply never deliver
// these events — nothing to detect up front, so the degrade is silent by
// construction (see cmd/keyprobe to confirm reporting).
func FromMouseButton(b tea.MouseButton) (Key, bool) {
	switch b {
	case tea.MouseBackward:
		return Key{Base: MouseBackBase}, true
	case tea.MouseForward:
		return Key{Base: MouseForwardBase}, true
	}
	return Key{}, false
}
