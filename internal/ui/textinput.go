// Package ui: shared single-line text-input editing (#763). Palette and
// finder inputs were append-only; EditKey gives them a movable cursor with
// word motions and word deletion, CursorView renders it.
package ui

import (
	"strings"
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

// PasteText inserts a pasted block into a single-line input at rune cursor
// cur (clamped into range first), returning the new text, the new cursor and
// whether anything changed (#1273).
//
// Every caller here is a one-line field, so a multi-line paste is flattened
// rather than rejected: lines are trimmed and joined with single spaces, and
// empty lines drop out. That makes the common cases behave — a path copied
// with its trailing newline pastes clean, a wrapped multi-line snippet
// becomes one searchable line — without a single-line input ever holding a
// line break, which EditKey also forbids.
func PasteText(text string, cur int, paste string) (out string, ncur int, changed bool) {
	r := []rune(text)
	if cur < 0 {
		cur = 0
	}
	if cur > len(r) {
		cur = len(r)
	}
	ins := []rune(flattenPaste(paste))
	if len(ins) == 0 {
		return text, cur, false
	}
	return string(r[:cur]) + string(ins) + string(r[cur:]), cur + len(ins), true
}

// flattenPaste collapses a possibly multi-line block into one line. Other
// control characters are dropped so a stray escape sequence cannot corrupt
// the rendered row.
//
// A block that holds a single line of content is inserted verbatim — a
// deliberate leading or trailing space survives, and a path copied with its
// trailing newline arrives clean, because the empty segment after the break
// carries no content. Only a genuinely multi-line block is normalised: each
// line trimmed, empties dropped, the rest joined with single spaces, which is
// what makes a wrapped snippet usable as one query.
func flattenPaste(paste string) string {
	lines := strings.FieldsFunc(paste, func(c rune) bool { return c == '\n' || c == '\r' })
	if len(lines) == 1 {
		return stripControl(lines[0])
	}
	var out []string
	for _, line := range lines {
		if line = strings.TrimSpace(stripControl(line)); line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, " ")
}

// stripControl removes non-printable runes (tabs become spaces, so a pasted
// indented line keeps its word boundaries).
func stripControl(s string) string {
	return strings.Map(func(c rune) rune {
		switch {
		case c == '\t':
			return ' '
		case unicode.IsControl(c):
			return -1
		}
		return c
	}, s)
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
