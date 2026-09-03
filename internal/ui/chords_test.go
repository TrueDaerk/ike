package ui

// chords_test.go pins EditKey's chord matching (#2459): the Command key in
// every spelling a terminal may report it in, the modifier that a
// text-reporting terminal would otherwise mask, tolerated shift, and the
// readline kills added with the sweep.

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func selTestStyle() lipgloss.Style { return lipgloss.NewStyle().Reverse(true) }

// keyText is a key press that also carries the literal text the terminal
// reported — the shape the Kitty protocol's "report associated text" flag and
// the Windows Console API produce, where msg.String() returns the bare rune
// and hides the modifier entirely (#2064).
func keyText(code rune, mod tea.KeyMod, text string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod, Text: text}
}

func TestEditKeyChords(t *testing.T) {
	cases := []struct {
		name    string
		msg     tea.KeyPressMsg
		text    string
		cur     int
		wantTxt string
		wantCur int
		changed bool
	}{
		// The Command key under bubbletea's other spelling: meta+ is the same
		// physical key as super+, so every cmd chord has a meta twin.
		{"meta line start", key(tea.KeyLeft, tea.ModMeta), "foo bar", 4, "foo bar", 0, false},
		{"meta line end", key(tea.KeyRight, tea.ModMeta), "foo bar", 1, "foo bar", 7, false},
		{"meta kill to start", key(tea.KeyBackspace, tea.ModMeta), "foo bar", 4, "bar", 0, true},
		{"meta kill to end", key(tea.KeyDelete, tea.ModMeta), "foo bar", 4, "foo ", 4, true},

		// shift is tolerated on the modified chords: a one-line field has no
		// selection a shift could extend.
		{"shift+super line start", key(tea.KeyLeft, tea.ModSuper|tea.ModShift), "foo bar", 4, "foo bar", 0, false},
		{"shift+meta line end", key(tea.KeyRight, tea.ModMeta|tea.ModShift), "foo bar", 1, "foo bar", 7, false},
		{"shift+alt word left", key(tea.KeyLeft, tea.ModAlt|tea.ModShift), "foo bar", 7, "foo bar", 4, false},
		{"shift+super kill to start", key(tea.KeyBackspace, tea.ModSuper|tea.ModShift), "foo bar", 4, "bar", 0, true},

		// ctrl word motions (the everywhere-deliverable aliases).
		{"ctrl word left", key(tea.KeyLeft, tea.ModCtrl), "foo bar", 7, "foo bar", 4, false},
		{"ctrl word right", key(tea.KeyRight, tea.ModCtrl), "foo bar", 0, "foo bar", 3, false},

		// The chords a text-reporting terminal masks: the literal rune rides
		// along, and matching on Code + Mod still sees the modifier.
		{"ctrl+w masked", keyText('w', tea.ModCtrl, "w"), "foo bar", 7, "foo ", 4, true},
		{"alt+d masked", keyText('d', tea.ModAlt, "d"), "foo bar", 0, " bar", 0, true},
		{"alt+backspace masked", keyText(tea.KeyBackspace, tea.ModAlt, "\x7f"), "foo bar", 7, "foo ", 4, true},

		// New in #2459.
		{"ctrl+backspace word kill", key(tea.KeyBackspace, tea.ModCtrl), "foo bar", 7, "foo ", 4, true},
		{"ctrl+delete word kill", key(tea.KeyDelete, tea.ModCtrl), "foo bar", 0, " bar", 0, true},
		{"ctrl+h backspace", keyText('h', tea.ModCtrl, "h"), "abc", 2, "ac", 1, true},
		{"ctrl+h at start", key('h', tea.ModCtrl), "abc", 0, "abc", 0, false},
		{"ctrl+u kill to start", key('u', tea.ModCtrl), "foo bar", 4, "bar", 0, true},
		{"ctrl+u at start", key('u', tea.ModCtrl), "foo bar", 0, "foo bar", 0, false},
		{"ctrl+k kill to end", key('k', tea.ModCtrl), "foo bar", 4, "foo ", 4, true},
		{"ctrl+k at end", key('k', tea.ModCtrl), "foo bar", 7, "foo bar", 7, false},
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

// TestEditKeyUnhandled pins what EditKey leaves to its caller: enter, tab and
// esc are every field's own business, plain shift+arrows stay selection keys
// (a host that wants them must see them), and the chords the epic ruled out
// because they are load-bearing elsewhere must not be swallowed here.
func TestEditKeyUnhandled(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyPressMsg
	}{
		{"enter", key(tea.KeyEnter, 0)},
		{"tab", key(tea.KeyTab, 0)},
		{"esc", key(tea.KeyEscape, 0)},
		{"shift+left", key(tea.KeyLeft, tea.ModShift)},
		{"shift+right", key(tea.KeyRight, tea.ModShift)},
		{"up", key(tea.KeyUp, 0)},
		{"down", key(tea.KeyDown, 0)},
		{"pgup", key(tea.KeyPgUp, 0)},
		{"ctrl+a", key('a', tea.ModCtrl)},
		{"ctrl+e", key('e', tea.ModCtrl)},
		{"ctrl+b", key('b', tea.ModCtrl)},
		{"ctrl+f", key('f', tea.ModCtrl)},
		{"ctrl+d", key('d', tea.ModCtrl)},
		{"ctrl+t", key('t', tea.ModCtrl)},
		{"ctrl+n", key('n', tea.ModCtrl)},
		{"ctrl+p", key('p', tea.ModCtrl)},
		{"alt+f", key('f', tea.ModAlt)},
		{"alt+b", key('b', tea.ModAlt)},
		{"cmd+c", key('c', tea.ModSuper)},
		{"cmd+v", key('v', tea.ModSuper)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, cur, handled, changed := EditKey(tc.msg, "foo bar", 4)
			if handled || changed || out != "foo bar" || cur != 4 {
				t.Fatalf("%s must stay the caller's: (%q, %d, handled=%v, changed=%v)",
					tc.name, out, cur, handled, changed)
			}
		})
	}
}

// TestFieldWrapsEditKey checks the value type mirrors the helpers it wraps —
// the state moves, the reports match, and an unhandled key leaves it alone.
func TestFieldWrapsEditKey(t *testing.T) {
	var f Field
	f.Set("foo bar")
	if f.Text != "foo bar" || f.Cur != 7 || f.Len() != 7 || len(f.Runes()) != 7 {
		t.Fatalf("Set: %+v", f)
	}
	if handled, changed := f.Key(key(tea.KeyLeft, tea.ModSuper)); !handled || changed {
		t.Fatalf("line start: handled=%v changed=%v", handled, changed)
	}
	if f.Cur != 0 {
		t.Fatalf("cursor after line start = %d", f.Cur)
	}
	if handled, changed := f.Key(key('k', tea.ModCtrl)); !handled || !changed || f.Text != "" {
		t.Fatalf("ctrl+k: handled=%v changed=%v text=%q", handled, changed, f.Text)
	}
	if handled, _ := f.Key(key(tea.KeyEnter, 0)); handled {
		t.Fatalf("enter must stay unhandled")
	}
	if !f.Paste("one\ntwo") || f.Text != "one two" || f.Cur != 7 {
		t.Fatalf("paste: %+v", f)
	}
	if f.Paste("  \n \n ") {
		t.Fatalf("a blank block must not change the field")
	}
	if f.View() == "" || f.ViewSel(0, 3, selTestStyle()) == "" {
		t.Fatalf("views must render")
	}
	f.Clear()
	if !f.Empty() || f.Cur != 0 {
		t.Fatalf("Clear: %+v", f)
	}
	if got := NewField("grün"); got.Text != "grün" || got.Cur != 4 {
		t.Fatalf("NewField: %+v", got)
	}
}
