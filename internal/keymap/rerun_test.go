package keymap

import "testing"

// TestPerContextCtrlR (#2314): ctrl+r is JetBrains' Rerun, and the default set
// spends it per pane — a re-send of the shown request in the HTTP response
// pane, a listing reload in the archive viewer. The two contexts are disjoint,
// so neither is a conflict nor a shadow, and the chord stays free everywhere
// else. The editor is the exception off macOS, where the Cmd→Ctrl fold lands
// editor.replace (cmd+r) on ctrl+r — a different pane, so the panes' rows
// neither conflict with it nor shadow it.
func TestPerContextCtrlR(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		table := BuildTable(Defaults(PresetJetBrains), nil, goos)
		chord := NormalizeChord(MustParseChord("ctrl+r"), goos)
		if b, ok := table.Lookup(chord, HTTP); !ok || b.Command != "http.resend" {
			t.Errorf("%s: http ctrl+r = %+v ok=%v, want http.resend", goos, b, ok)
		}
		if b, ok := table.Lookup(chord, Archive); !ok || b.Command != "archive.reload" {
			t.Errorf("%s: archive ctrl+r = %+v ok=%v, want archive.reload", goos, b, ok)
		}
		for _, ctx := range []Context{Global, Explorer, Terminal} {
			if b, ok := table.Lookup(chord, ctx); ok {
				t.Errorf("%s: ctrl+r must stay unbound in %q, got %q", goos, contextLabel(ctx), b.Command)
			}
		}
		for _, c := range table.Conflicts() {
			if c.Chord == chord.String() {
				t.Errorf("%s: disjoint-context ctrl+r reported as conflict: %+v", goos, c)
			}
		}
		for _, s := range table.Shadows() {
			if s.Chord == chord.String() {
				t.Errorf("%s: ctrl+r reported as shadow: %+v", goos, s)
			}
		}
	}
}

// TestCtrlRIsDelivered guards the reason the chord was chosen (#2314): a plain
// ctrl+letter reaches the program on every platform, so the two bindings need
// no palette fallback the way the Cmd-modified HTTP chords do.
func TestCtrlRIsDelivered(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		if got := Classify(NormalizeChord(MustParseChord("ctrl+r"), goos)); got != Delivered {
			t.Errorf("%s: ctrl+r classified %v, want Delivered", goos, got)
		}
	}
}
