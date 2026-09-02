package ui

import "strings"

// FindChord reports whether key is the shared find chord (#2409) — the
// muscle-memory alias for the bare "/" every searchable surface binds.
//
// Panes reached through the pane registry get the chord from the Global
// search.open command instead; this helper is for the surfaces that own the
// keyboard before the keymap layer runs (the settings panel, the TODO index
// overlay, a terminal in copy mode), which have to answer it themselves.
//
// Both spellings of the Command key are listed: bubbletea reports it as
// "super+" under some terminal protocols and the keymap layer's "cmd+" form
// reaches here through Model.String() under others.
func FindChord(key string) bool {
	switch key {
	case "cmd+f", "super+f", "ctrl+f":
		return true
	}
	return false
}

// MatchStepChord reports whether key is the shared match-step chord (#2410)
// and which way it steps: +1 for cmd+g, -1 for cmd+shift+g. ctrl+g is the
// non-mac secondary (Cmd folds onto Ctrl there) and f3 / shift+f3 the
// JetBrains spelling.
//
// Like FindChord this is for the surfaces that own the keyboard before the
// keymap layer runs — the settings panel, the TODO index overlay, the
// explorer's speed search, a terminal. Panes reached through the pane
// registry get the chord from the Global search.nextMatch / search.prevMatch
// commands instead.
//
// The chord is matched by parts rather than by spelling: bubbletea reports
// the Command key as "meta+" or "super+" depending on the terminal protocol,
// the keymap layer's canonical form is "cmd+", and the modifier order differs
// between the two ("shift+meta+g" vs "cmd+shift+g"). Enumerating that product
// is how a chord ends up working in one terminal and not the next.
func MatchStepChord(key string) (delta int, ok bool) {
	parts := strings.Split(key, "+")
	base := parts[len(parts)-1]
	shift, cmd := false, false
	for _, mod := range parts[:len(parts)-1] {
		switch mod {
		case "shift":
			shift = true
		case "cmd", "super", "meta", "ctrl":
			cmd = true
		default:
			return 0, false // alt+g and friends are somebody else's chord
		}
	}
	switch base {
	case "g":
		if !cmd {
			return 0, false // bare g is a motion key in half the panes
		}
	case "f3":
		if cmd {
			return 0, false
		}
	default:
		return 0, false
	}
	if shift {
		return -1, true
	}
	return 1, true
}

// NextMatchChord reports whether key steps forward through the matches.
func NextMatchChord(key string) bool {
	delta, ok := MatchStepChord(key)
	return ok && delta > 0
}

// PrevMatchChord reports whether key steps backward through the matches.
func PrevMatchChord(key string) bool {
	delta, ok := MatchStepChord(key)
	return ok && delta < 0
}
