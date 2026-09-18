package keymap

import "testing"

// TestNonTyping pins the class of chords that resolve through the keymap even
// while an editor captures text (#2622): function keys and command-modified
// chords, never a plain or merely shifted character key.
func TestNonTyping(t *testing.T) {
	cases := []struct {
		chord string
		want  bool
	}{
		{"f1", true},
		{"f2", true},
		{"shift+f2", true},
		{"f12", true},
		{"alt+f7", true},
		{"cmd+g", true},
		{"cmd+shift+g", true},
		{"ctrl+s", true},
		{"alt+enter", true},
		{"a", false},
		{"shift+a", false},
		{"5", false},
		{"enter", false},
		{"tab", false},
		{"shift+tab", false},
		{"backspace", false},
		{"esc", false},
		{"up", false},
		{"shift+home", false},
		{"/", false},
	}
	for _, tc := range cases {
		k, err := ParseKey(tc.chord)
		if err != nil {
			t.Fatalf("ParseKey(%q): %v", tc.chord, err)
		}
		if got := k.NonTyping(); got != tc.want {
			t.Errorf("Key(%q).NonTyping() = %v, want %v", tc.chord, got, tc.want)
		}
	}
}
