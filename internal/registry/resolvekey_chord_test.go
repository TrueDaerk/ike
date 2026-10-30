package registry

import (
	"testing"

	"ike/internal/keymap"
	"ike/internal/plugin"
)

// TestResolveKeyMatchesParsedChords guards #3107: a pane-scoped keymap
// declared "cmd+<key>" resolves for the spellings bubbletea actually reports —
// super+ (Kitty) and meta+ (legacy) on darwin, ctrl+ elsewhere — because both
// sides are compared as parsed, platform-normalised chords.
func TestResolveKeyMatchesParsedChords(t *testing.T) {
	old := keymap.GOOS
	t.Cleanup(func() { keymap.GOOS = old })

	r := New()
	r.Add(fake{id: "p", caps: plugin.Capabilities{Keymaps: []plugin.Keymap{
		{Keys: "cmd+backspace", Scope: plugin.PaneScope("explorer")},
		{Keys: "C", Scope: plugin.PaneScope("explorer")},
	}}})

	for _, tc := range []struct {
		goos, press string
		want        bool
	}{
		{"darwin", "super+backspace", true},
		{"darwin", "meta+backspace", true},
		{"darwin", "cmd+backspace", true},
		{"darwin", "ctrl+backspace", false},
		{"linux", "ctrl+backspace", true},
		{"linux", "backspace", false},
		{"darwin", "shift+c", true},
		{"darwin", "c", false},
	} {
		keymap.GOOS = tc.goos
		_, ok := r.ResolveKey(tc.press, "explorer")
		if ok != tc.want {
			t.Errorf("%s: %q resolved = %v, want %v", tc.goos, tc.press, ok, tc.want)
		}
	}
	keymap.GOOS = "darwin"
	if _, ok := r.ResolveKey("super+backspace", "editor"); ok {
		t.Error("a pane-scoped keymap must not resolve outside its pane")
	}
}
