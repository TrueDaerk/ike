package ui

import (
	tea "charm.land/bubbletea/v2"
)

// CopyChord reports whether key is the shared modified copy chord of the
// selectable list surfaces (#2062, #2071). It is the muscle-memory alias for
// the bare copy key ("y" in the list panels, "c"/"Y" in the DOM tree).
//
// The Command key has three spellings in play — bubbletea reports it as
// super+ under one terminal protocol and meta+ under another, and the keymap
// layer's canonical name is cmd+ — so all three match the same physical chord
// (#2459).
//
// ctrl+c is deliberately absent: the chord is the global quit and a list
// panel has no text selection that could claim it — only surfaces with a live
// selection (the HTTP response viewer) take it over.
func CopyChord(key string) bool {
	switch key {
	case "cmd+c", "super+c", "meta+c":
		return true
	}
	return false
}

// CopyKey is CopyChord over the key message itself, matching Code + Mod so a
// terminal that reports associated text alongside the chord ("c" for cmd+c)
// cannot mask it (#2064, #2459). Prefer it over CopyChord in new call sites.
func CopyKey(msg tea.KeyPressMsg) bool {
	mod := msg.Mod &^ tea.ModShift
	return msg.Code == 'c' && (mod == tea.ModSuper || mod == tea.ModMeta)
}
