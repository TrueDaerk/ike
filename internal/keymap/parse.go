package keymap

import (
	"fmt"
	"strings"
)

// modAlias maps the modifier tokens an author may write to a logical Mod. Cmd,
// command, meta and super all fold to ModMeta (platform.go decides its concrete
// mapping); option/opt alias Alt; control aliases Ctrl.
var modAlias = map[string]Mod{
	"cmd":     ModMeta,
	"command": ModMeta,
	"meta":    ModMeta,
	"super":   ModMeta,
	"win":     ModMeta,
	"ctrl":    ModCtrl,
	"control": ModCtrl,
	"alt":     ModAlt,
	"opt":     ModAlt,
	"option":  ModAlt,
	"shift":   ModShift,
}

// baseAlias canonicalises common spellings of named base keys. The bracket
// glyphs map to their named bases here (not in FromKeyMsg) so a modified
// press ("cmd+[") normalizes the same way as a bare one (#284).
var baseAlias = map[string]string{
	"escape":   "esc",
	"return":   "enter",
	"spacebar": "space",
	"spc":      "space",
	"del":      "delete",
	"pageup":   "pgup",
	"pagedown": "pgdown",
	"pgdn":     "pgdown",
	"[":        "left-bracket",
	"]":        "right-bracket",
}

// ParseChord parses a chord string ("cmd+k cmd+c", "shift shift", "esc") into a
// Chord. Steps are whitespace-separated; within a step, "+"-separated modifier
// tokens precede the base key. It is the inverse of Chord.String up to canonical
// ordering: ParseChord(c.String()) == c.
func ParseChord(s string) (Chord, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return Chord{}, fmt.Errorf("keymap: empty chord %q", s)
	}
	steps := make([]Key, 0, len(fields))
	for _, f := range fields {
		k, err := ParseKey(f)
		if err != nil {
			return Chord{}, err
		}
		steps = append(steps, k)
	}
	return Chord{Steps: steps}, nil
}

// MustParseChord is ParseChord that panics on error; for the static default set.
func MustParseChord(s string) Chord {
	c, err := ParseChord(s)
	if err != nil {
		panic(err)
	}
	return c
}

// ParseKey parses a single step ("cmd+shift+a", "f7", "A") into a Key. A bare
// uppercase single letter (as bubbletea reports a shifted letter) is folded to
// its lowercase base with an implied Shift modifier.
func ParseKey(s string) (Key, error) {
	parts := strings.Split(s, "+")
	base := parts[len(parts)-1]
	var mods Mod
	for _, p := range parts[:len(parts)-1] {
		m, ok := modAlias[strings.ToLower(p)]
		if !ok {
			return Key{}, fmt.Errorf("keymap: unknown modifier %q in %q", p, s)
		}
		mods |= m
	}
	if base == "" {
		return Key{}, fmt.Errorf("keymap: missing base key in %q", s)
	}
	// No real key base contains an underscore; reject so the focus_* config
	// stopgap (which shares the [keymap.bindings] map) is treated as a non-chord.
	if strings.Contains(base, "_") {
		return Key{}, fmt.Errorf("keymap: invalid base key %q in %q", base, s)
	}
	// A shifted letter arrives as a bare uppercase rune; record it as base+Shift.
	if len(base) == 1 && base[0] >= 'A' && base[0] <= 'Z' {
		mods |= ModShift
		base = strings.ToLower(base)
	} else {
		base = strings.ToLower(base)
	}
	if canon, ok := baseAlias[base]; ok {
		base = canon
	}
	return Key{Base: base, Mods: mods}, nil
}
