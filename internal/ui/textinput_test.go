package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func key(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

func TestEditKeyMotions(t *testing.T) {
	cases := []struct {
		name    string
		msg     tea.KeyPressMsg
		text    string
		cur     int
		wantTxt string
		wantCur int
		changed bool
	}{
		{"left", key(tea.KeyLeft, 0), "abc", 2, "abc", 1, false},
		{"left at start", key(tea.KeyLeft, 0), "abc", 0, "abc", 0, false},
		{"right", key(tea.KeyRight, 0), "abc", 1, "abc", 2, false},
		{"right at end", key(tea.KeyRight, 0), "abc", 3, "abc", 3, false},
		{"home", key(tea.KeyHome, 0), "abc", 2, "abc", 0, false},
		{"end", key(tea.KeyEnd, 0), "abc", 0, "abc", 3, false},
		{"word left", key(tea.KeyLeft, tea.ModAlt), "foo bar", 7, "foo bar", 4, false},
		{"word left over space", key(tea.KeyLeft, tea.ModAlt), "foo bar", 4, "foo bar", 0, false},
		{"word right", key(tea.KeyRight, tea.ModAlt), "foo bar", 0, "foo bar", 3, false},
		{"word right from space", key(tea.KeyRight, tea.ModAlt), "foo bar", 3, "foo bar", 7, false},
		{"backspace mid", key(tea.KeyBackspace, 0), "abc", 2, "ac", 1, true},
		{"backspace at start", key(tea.KeyBackspace, 0), "abc", 0, "abc", 0, false},
		{"delete mid", key(tea.KeyDelete, 0), "abc", 1, "ac", 1, true},
		{"delete at end", key(tea.KeyDelete, 0), "abc", 3, "abc", 3, false},
		{"word backspace", key(tea.KeyBackspace, tea.ModAlt), "foo bar", 7, "foo ", 4, true},
		{"word backspace mid-word", key(tea.KeyBackspace, tea.ModAlt), "foo bar", 6, "foo r", 4, true},
		{"word delete", key(tea.KeyDelete, tea.ModAlt), "foo bar", 0, " bar", 0, true},
		{"kill to start", key(tea.KeyBackspace, tea.ModSuper), "foo bar", 4, "bar", 0, true},
		{"clamp cursor", key(tea.KeyBackspace, 0), "ab", 99, "a", 1, true},
		{"umlaut backspace", key(tea.KeyBackspace, 0), "über", 2, "üer", 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, cur, handled, changed := EditKey(tc.msg, tc.text, tc.cur)
			if !handled {
				t.Fatalf("not handled")
			}
			if out != tc.wantTxt || cur != tc.wantCur || changed != tc.changed {
				t.Fatalf("got (%q, %d, changed=%v), want (%q, %d, changed=%v)",
					out, cur, changed, tc.wantTxt, tc.wantCur, tc.changed)
			}
		})
	}
}

func TestEditKeyInsert(t *testing.T) {
	out, cur, handled, changed := EditKey(tea.KeyPressMsg{Text: "x"}, "ac", 1)
	if !handled || !changed || out != "axc" || cur != 2 {
		t.Fatalf("insert mid: got (%q, %d, %v, %v)", out, cur, handled, changed)
	}
	out, cur, handled, changed = EditKey(tea.KeyPressMsg{Text: " "}, "ab", 2)
	if !handled || !changed || out != "ab " || cur != 3 {
		t.Fatalf("space at end: got (%q, %d, %v, %v)", out, cur, handled, changed)
	}
	// Pasted multi-rune text lands at the cursor.
	out, cur, _, _ = EditKey(tea.KeyPressMsg{Text: "XY"}, "ab", 1)
	if out != "aXYb" || cur != 3 {
		t.Fatalf("paste: got (%q, %d)", out, cur)
	}
	// Modified printables are not insertions (chords belong to the caller).
	if _, _, handled, _ = EditKey(tea.KeyPressMsg{Text: "c", Mod: tea.ModCtrl}, "ab", 2); handled {
		t.Fatalf("ctrl-modified text must not insert")
	}
	// Line breaks never enter a single-line field.
	if _, _, handled, _ = EditKey(tea.KeyPressMsg{Text: "a\nb"}, "", 0); handled {
		t.Fatalf("line break must not insert")
	}
}

func TestCursorView(t *testing.T) {
	// Cursor at end renders an appended reversed cell.
	if got := CursorView("ab", 2); !strings.HasPrefix(got, "ab") || got == "ab" {
		t.Fatalf("end cursor: %q", got)
	}
	// Mid-text keeps all runes visible.
	got := CursorView("abc", 1)
	for _, want := range []string{"a", "b", "c"} {
		if !strings.Contains(got, want) {
			t.Fatalf("mid cursor lost %q: %q", want, got)
		}
	}
}
