// Package ui: shared single-line text-input editing (#763). Palette and
// finder inputs were append-only; EditKey gives them a movable cursor with
// word motions and word deletion, CursorView renders it.
package ui

import (
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// EditKey applies a single-line editing key to text with a rune cursor at
// cur (clamped into range first). It returns the new text and cursor,
// whether the key was consumed, and whether the text changed. Callers run
// their own chords first — EditKey only sees what they left over — so a
// caller-level ctrl+w toggle keeps priority over the word-delete here.
func EditKey(msg tea.KeyPressMsg, text string, cur int) (out string, ncur int, handled, changed bool) {
	r := []rune(text)
	if cur < 0 {
		cur = 0
	}
	if cur > len(r) {
		cur = len(r)
	}
	switch msg.String() {
	case "left":
		if cur > 0 {
			cur--
		}
		return text, cur, true, false
	case "right":
		if cur < len(r) {
			cur++
		}
		return text, cur, true, false
	case "home", "super+left":
		return text, 0, true, false
	case "end", "super+right":
		return text, len(r), true, false
	case "alt+left", "ctrl+left":
		return text, wordLeft(r, cur), true, false
	case "alt+right", "ctrl+right":
		return text, wordRight(r, cur), true, false
	case "backspace":
		if cur > 0 {
			return string(r[:cur-1]) + string(r[cur:]), cur - 1, true, true
		}
		return text, cur, true, false
	case "alt+backspace", "ctrl+w":
		if cur > 0 {
			s := wordLeft(r, cur)
			return string(r[:s]) + string(r[cur:]), s, true, true
		}
		return text, cur, true, false
	case "delete":
		if cur < len(r) {
			return string(r[:cur]) + string(r[cur+1:]), cur, true, true
		}
		return text, cur, true, false
	case "alt+delete", "alt+d":
		if cur < len(r) {
			e := wordRight(r, cur)
			return string(r[:cur]) + string(r[e:]), cur, true, true
		}
		return text, cur, true, false
	case "super+backspace":
		if cur > 0 {
			return string(r[cur:]), 0, true, true
		}
		return text, cur, true, false
	}
	if msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper|tea.ModMeta) == 0 {
		ins := []rune(msg.Text)
		if hasLineBreak(ins) {
			return text, cur, false, false
		}
		out := string(r[:cur]) + msg.Text + string(r[cur:])
		return out, cur + len(ins), true, true
	}
	return text, cur, false, false
}

// CursorView renders text with a reverse-video cursor at rune index cur
// (end-of-text shows a reversed space).
func CursorView(text string, cur int) string {
	rev := lipgloss.NewStyle().Reverse(true)
	r := []rune(text)
	if cur < 0 {
		cur = 0
	}
	if cur >= len(r) {
		return text + rev.Render(" ")
	}
	return string(r[:cur]) + rev.Render(string(r[cur])) + string(r[cur+1:])
}

// wordLeft finds the start of the word before cur: skip non-word runes,
// then the word-rune run. Word runes are letters, digits and underscore.
func wordLeft(r []rune, cur int) int {
	for cur > 0 && !isWordRune(r[cur-1]) {
		cur--
	}
	for cur > 0 && isWordRune(r[cur-1]) {
		cur--
	}
	return cur
}

// wordRight finds the position after the word at/after cur.
func wordRight(r []rune, cur int) int {
	for cur < len(r) && !isWordRune(r[cur]) {
		cur++
	}
	for cur < len(r) && isWordRune(r[cur]) {
		cur++
	}
	return cur
}

func isWordRune(c rune) bool {
	return c == '_' || unicode.IsLetter(c) || unicode.IsDigit(c)
}

func hasLineBreak(r []rune) bool {
	for _, c := range r {
		if c == '\n' || c == '\r' {
			return true
		}
	}
	return false
}
