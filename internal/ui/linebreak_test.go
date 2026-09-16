package ui_test

// linebreak_test.go covers the one-line field's line-break support (#2600):
// the alt+enter chord, the marker rendering that keeps the field one row, and
// the fact that ordinary editing treats the break as exactly one rune.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/ui"
)

func TestIsBreakKey(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyPressMsg
		want bool
	}{
		{"alt+enter", tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt}, true},
		{"shift+alt+enter", tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt | tea.ModShift}, true},
		{"plain enter", tea.KeyPressMsg{Code: tea.KeyEnter}, false},
		{"ctrl+enter", tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}, false},
		{"alt+a", tea.KeyPressMsg{Code: 'a', Mod: tea.ModAlt}, false},
	}
	for _, c := range cases {
		if got := ui.IsBreakKey(c.msg); got != c.want {
			t.Errorf("%s: IsBreakKey = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFieldInsertBreakMidText(t *testing.T) {
	f := ui.NewField("ab")
	f.Cur = 1
	f.InsertBreak()
	if f.Text != "a\nb" || f.Cur != 2 {
		t.Fatalf("insert: text=%q cur=%d, want %q 2", f.Text, f.Cur, "a\nb")
	}
	if !f.HasBreak() {
		t.Fatal("HasBreak should report the break")
	}
	// The break is one rune for every other editing key: left steps onto it,
	// backspace removes it whole.
	if _, _ = f.Key(tea.KeyPressMsg{Code: tea.KeyLeft}); f.Cur != 1 {
		t.Fatalf("left: cur=%d, want 1", f.Cur)
	}
	f.Cur = 2
	if _, changed := f.Key(tea.KeyPressMsg{Code: tea.KeyBackspace}); !changed {
		t.Fatal("backspace should change the text")
	}
	if f.Text != "ab" || f.Cur != 1 {
		t.Fatalf("backspace: text=%q cur=%d, want %q 1", f.Text, f.Cur, "ab")
	}
}

func TestShowBreaksRendersOneRow(t *testing.T) {
	out := ui.ShowBreaks("a\nb")
	if strings.Contains(out, "\n") {
		t.Fatalf("rendered text must stay one row: %q", out)
	}
	if !strings.Contains(out, ui.BreakMarker) {
		t.Fatalf("rendered text should carry the marker: %q", out)
	}
	if got := ui.ShowBreaks("plain"); got != "plain" {
		t.Fatalf("break-free text must pass through unchanged: %q", got)
	}
}

func TestCursorViewMarksBreak(t *testing.T) {
	// Cursor before the break, on it, and past it: the row never wraps and the
	// marker is always there.
	for _, cur := range []int{0, 1, 3} {
		out := ui.CursorView("a\nb", cur)
		if strings.Contains(out, "\n") {
			t.Fatalf("cur=%d: view must stay one row: %q", cur, out)
		}
		if !strings.Contains(out, ui.BreakMarker) {
			t.Fatalf("cur=%d: view should show the marker: %q", cur, out)
		}
	}
}

func TestPasteStillFlattensBreaks(t *testing.T) {
	// The chord is the only way a break gets in: a multi-line paste keeps
	// flattening, so nothing else in the tree starts holding breaks by accident.
	f := ui.NewField("")
	f.Paste("one\ntwo")
	if strings.Contains(f.Text, "\n") {
		t.Fatalf("paste should stay flattened: %q", f.Text)
	}
}
