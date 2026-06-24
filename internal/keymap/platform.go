package keymap

import "runtime"

// NormalizeKey folds the logical Meta modifier into the platform's concrete
// modifier: on macOS the terminal can forward Cmd as Meta, so Meta is kept; on
// every other platform Cmd→Ctrl. Normalisation is idempotent and runs once at
// table-build time so the resolver only ever compares concrete keys.
func NormalizeKey(k Key, goos string) Key {
	if goos == "darwin" {
		return k
	}
	if k.Mods&ModMeta != 0 {
		k.Mods &^= ModMeta
		k.Mods |= ModCtrl
	}
	return k
}

// NormalizeChord normalises every step of a chord for goos.
func NormalizeChord(c Chord, goos string) Chord {
	out := Chord{Steps: make([]Key, len(c.Steps))}
	for i, k := range c.Steps {
		out.Steps[i] = NormalizeKey(k, goos)
	}
	return out
}

// GOOS is the platform the table is built for; defaults to the build target and
// is overridable in tests.
var GOOS = runtime.GOOS
