package keymap

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestNULFoldsOntoCtrlSpace guards #2695: a terminal on the legacy encoding
// reports ctrl+space as the C0 NUL, which bubbletea spells "ctrl+@". Both
// spellings — typed by the terminal or written into settings.toml — parse to
// the same chord, so one binding covers every terminal. A bare "@" and a
// differently modified one keep their own identity.
func TestNULFoldsOntoCtrlSpace(t *testing.T) {
	want := key(t, "ctrl+space")
	for _, s := range []string{"ctrl+@", "ctrl+space", "control+@"} {
		if got := key(t, s); got != want {
			t.Errorf("ParseKey(%q) = %v, want %v", s, got, want)
		}
	}
	for _, s := range []string{"@", "cmd+@", "ctrl+shift+@"} {
		if got := key(t, s); got.Base != "@" {
			t.Errorf("ParseKey(%q) = %v, want the @ base untouched", s, got)
		}
	}
	// The same fold applies to what the terminal actually delivers.
	for _, msg := range []tea.KeyPressMsg{
		{Code: tea.KeySpace, Mod: tea.ModCtrl},
		{Code: '@', Mod: tea.ModCtrl},
	} {
		got, ok := FromKeyMsg(msg)
		if !ok || got != want {
			t.Errorf("FromKeyMsg(%q) = %v (ok=%v), want %v", msg.String(), got, ok, want)
		}
		if !got.NonTyping() {
			t.Errorf("%v must be non-typing so it reaches the keymap layer in insert mode", got)
		}
	}
}

// TestCompletionTriggerIsBoundInTheEditor: ctrl+space resolves to
// completion.trigger while an editor has the focus, and the chord is delivered
// (no fragile flag, no alternative needed) on both platforms.
func TestCompletionTriggerIsBoundInTheEditor(t *testing.T) {
	chord := Chord{Steps: []Key{key(t, "ctrl+space")}}
	for _, goos := range []string{"darwin", "linux"} {
		table := BuildTable(DefaultsFor(PresetJetBrains, goos), nil, goos)
		b, ok := table.Lookup(chord, Editor)
		if !ok {
			t.Fatalf("%s: ctrl+space is unbound in the editor", goos)
		}
		if b.Command != "completion.trigger" {
			t.Errorf("%s: ctrl+space runs %q, want completion.trigger", goos, b.Command)
		}
		if b.Fragile {
			t.Errorf("%s: ctrl+space must not be fragile", goos)
		}
	}
	if got := Classify(chord); got != Delivered {
		t.Errorf("ctrl+space classified %v, want Delivered", got)
	}
}
