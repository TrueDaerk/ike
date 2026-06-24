// Package keymap owns IKE's keybinding layer: the model that resolves a key
// chord (in a focus context) to a registered Command id. Roadmap 0020 defines
// the Keymap capability and the registry; Roadmap 0040 owns the [keymap] config
// section and its precedence. This package sits in between: the binding model
// (Key/Chord), the JetBrains-like default set, scope/context resolution, build-
// time conflict detection, platform normalisation, a tea.KeyMsg adapter, and a
// cheatsheet view. It defines no Commands — it only binds keys to command ids
// owned by other roadmaps (editor/explorer/palette/project/VCS).
//
// Leaf discipline: this package depends only on bubbletea (for the KeyMsg
// adapter) and standard library; it never imports concrete panes.
package keymap

import "strings"

// Mod is a bitset of logical modifier keys. Authors write bindings with logical
// modifiers (Cmd/Meta); platform.go folds Meta into Ctrl off macOS once at
// table-build time so the resolver compares like-for-like.
type Mod uint8

const (
	ModMeta  Mod = 1 << iota // Cmd on macOS; normalised to Ctrl elsewhere.
	ModCtrl                  // Control.
	ModAlt                   // Alt / Option.
	ModShift                 // Shift.
)

// Key is a single key press: a canonical lowercase base ("a", "f7", "esc",
// "left-bracket", "/") plus its modifier set.
type Key struct {
	Base string
	Mods Mod
}

// Has reports whether the key carries modifier m.
func (k Key) Has(m Mod) bool { return k.Mods&m != 0 }

// modToken maps a logical modifier to its canonical string form. Meta renders
// as "cmd" to match the JetBrains-flavoured default set.
var modOrder = []struct {
	m   Mod
	tok string
}{
	{ModMeta, "cmd"},
	{ModCtrl, "ctrl"},
	{ModAlt, "alt"},
	{ModShift, "shift"},
}

// String returns the canonical "cmd+shift+a" form: modifiers in a fixed order
// (meta, ctrl, alt, shift) followed by the base key. Canonical formatting makes
// parse→format→parse idempotent regardless of the input ordering.
func (k Key) String() string {
	var b strings.Builder
	for _, mo := range modOrder {
		if k.Mods&mo.m != 0 {
			b.WriteString(mo.tok)
			b.WriteByte('+')
		}
	}
	b.WriteString(k.Base)
	return b.String()
}
