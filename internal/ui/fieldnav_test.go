package ui

// fieldnav_test.go covers the caret-navigation chords every one-line input
// owes the user (#2634): cmd+left / cmd+right to the ends of the text, the
// editor's editor.lineStart / editor.lineEnd applied to a field, and
// ctrl|alt+left / ctrl|alt+right by words. Telemetry had them resolving as
// `unbound` while a tool pane's input held the keyboard, so the chords are
// asserted here at the field — where they are answered — and at the hosts,
// where they used to leak.

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// navKey builds an arrow press with mod.
func navKey(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

func TestFieldNavigationChordsMoveTheCaret(t *testing.T) {
	cases := []struct {
		name string
		key  tea.KeyPressMsg
		cur  int
		want int
	}{
		{"cmd+left to the start", navKey(tea.KeyLeft, tea.ModSuper), 5, 0},
		{"meta+left to the start", navKey(tea.KeyLeft, tea.ModMeta), 5, 0},
		{"cmd+left at the start", navKey(tea.KeyLeft, tea.ModSuper), 0, 0},
		{"cmd+right to the end", navKey(tea.KeyRight, tea.ModSuper), 2, 11},
		{"meta+right to the end", navKey(tea.KeyRight, tea.ModMeta), 2, 11},
		{"cmd+right at the end", navKey(tea.KeyRight, tea.ModSuper), 11, 11},
		// shift is tolerated: a one-line field has no selection range to
		// extend, so the chord still means "to the edge" rather than falling
		// through to the keymap.
		{"shift+cmd+left to the start", navKey(tea.KeyLeft, tea.ModSuper|tea.ModShift), 5, 0},
		{"shift+cmd+right to the end", navKey(tea.KeyRight, tea.ModSuper|tea.ModShift), 5, 11},
		{"ctrl+left by a word", navKey(tea.KeyLeft, tea.ModCtrl), 11, 8},
		{"alt+left by a word", navKey(tea.KeyLeft, tea.ModAlt), 11, 8},
		{"ctrl+right by a word", navKey(tea.KeyRight, tea.ModCtrl), 0, 3},
		{"alt+right by a word", navKey(tea.KeyRight, tea.ModAlt), 0, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := Field{Text: "sev err bar", Cur: tc.cur} // 11 runes
			handled, changed := f.Key(tc.key)
			if !handled {
				t.Fatal("the field must consume the chord, not hand it back")
			}
			if changed {
				t.Fatal("a caret move must not report a text change")
			}
			if f.Cur != tc.want {
				t.Fatalf("caret = %d, want %d", f.Cur, tc.want)
			}
			if f.Text != "sev err bar" {
				t.Fatalf("the text must be untouched, got %q", f.Text)
			}
		})
	}
}

// A caret move drops an armed select-all rather than replacing the text: the
// user asked to go somewhere, not to overwrite everything.
func TestFieldNavigationChordDropsTheSelection(t *testing.T) {
	f := NewField("sev err")
	f.SelectAll()
	f.Key(navKey(tea.KeyLeft, tea.ModSuper))
	if f.Selected() {
		t.Fatal("a caret move must drop the armed selection")
	}
	if f.Cur != 0 || f.Text != "sev err" {
		t.Fatalf("cmd+left must move to the start of the intact text, got %q/%d", f.Text, f.Cur)
	}
}

// The chords reach a LineSearch's query the same way — the prompt only claims
// enter and esc for itself.
func TestLineSearchTakesTheNavigationChords(t *testing.T) {
	var s LineSearch
	s.Set("sev err bar")
	s.Open = true
	if handled, changed, act := s.Key(navKey(tea.KeyLeft, tea.ModSuper)); !handled || changed || act != SearchNone {
		t.Fatalf("cmd+left = (%v,%v,%v), want (true,false,SearchNone)", handled, changed, act)
	}
	if s.Field.Cur != 0 {
		t.Fatalf("cmd+left must put the caret at the start, got %d", s.Field.Cur)
	}
	if _, _, _ = s.Key(navKey(tea.KeyRight, tea.ModCtrl)); s.Field.Cur != 3 {
		t.Fatalf("ctrl+right must jump a word, got %d", s.Field.Cur)
	}
}

func TestIsNavKey(t *testing.T) {
	cases := []struct {
		name string
		key  tea.KeyPressMsg
		want bool
	}{
		{"cmd+left", navKey(tea.KeyLeft, tea.ModSuper), true},
		{"meta+right", navKey(tea.KeyRight, tea.ModMeta), true},
		{"ctrl+left", navKey(tea.KeyLeft, tea.ModCtrl), true},
		{"alt+right", navKey(tea.KeyRight, tea.ModAlt), true},
		{"shift+cmd+left", navKey(tea.KeyLeft, tea.ModSuper|tea.ModShift), true},
		{"plain left", navKey(tea.KeyLeft, 0), false},
		{"shift+left", navKey(tea.KeyLeft, tea.ModShift), false},
		{"cmd+up", navKey(tea.KeyUp, tea.ModSuper), false},
		// The two-modifier arrows stay commands: editor.tab.next /
		// editor.tab.prev and nav.back / nav.forward live there, and an open
		// input must not swallow them.
		{"ctrl+alt+left", navKey(tea.KeyLeft, tea.ModCtrl|tea.ModAlt), false},
		{"ctrl+cmd+right", navKey(tea.KeyRight, tea.ModCtrl|tea.ModSuper), false},
		{"cmd+alt+left", navKey(tea.KeyLeft, tea.ModSuper|tea.ModAlt), false},
		{"cmd+backspace", tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModSuper}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsNavKey(tc.key); got != tc.want {
				t.Fatalf("IsNavKey = %v, want %v", got, tc.want)
			}
		})
	}
}
