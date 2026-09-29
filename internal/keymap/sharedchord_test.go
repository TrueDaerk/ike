package keymap

import (
	"strings"
	"testing"
)

// sharedchord_test.go guards #2820: a second default binding for a chord in
// another context — the explorer's alt+enter context menu (#2805) next to the
// editor's lsp.codeAction — must never silence the first one.

// TestSharedChordDefaultsResolveInOwnContext: every default resolves in its
// own context, whatever other contexts bind the same chord — none is dropped,
// lost to a conflict or hidden by a sibling pane's binding.
func TestSharedChordDefaultsResolveInOwnContext(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		table := BuildTable(DefaultsFor(PresetJetBrains, goos), nil, goos)
		for _, b := range table.Bindings() {
			if !ChordInOtherContext(b.Context, b.Chord.String(), table.Bindings()) {
				continue
			}
			got, ok := table.Lookup(b.Chord, b.Context)
			if !ok || got.Command != b.Command {
				t.Errorf("%s: %s in %s resolves to %q (ok=%v), want %s", goos, b.Chord, b.Context, got.Command, ok, b.Command)
			}
			if d, dropped := table.Dropped(b.Chord, b.Context); dropped {
				t.Errorf("%s: %s in %s reported dropped (%s)", goos, b.Chord, b.Context, d.Command)
			}
		}
	}
}

// TestAltEnterBothContexts pins the concrete pair: the editor's intentions
// and the explorer's context menu, with no conflict or shadow between them.
func TestAltEnterBothContexts(t *testing.T) {
	table := BuildTable(Defaults(PresetJetBrains), nil, GOOS)
	chord := MustParseChord("alt+enter")
	for ctx, want := range map[Context]string{
		Editor:                   "lsp.codeAction",
		WithLang(Editor, "json"): "lsp.codeAction",
		Explorer:                 "explorer.contextMenu",
	} {
		if b, ok := table.Lookup(chord, ctx); !ok || b.Command != want {
			t.Fatalf("alt+enter in %s = %+v (ok=%v), want %s", ctx, b, ok, want)
		}
	}
	for _, c := range table.Conflicts() {
		if c.Chord == "alt+enter" {
			t.Fatalf("alt+enter conflict: %s", c)
		}
	}
	for _, s := range table.Shadows() {
		if s.Chord == "alt+enter" {
			t.Fatalf("alt+enter shadow: %s", s)
		}
	}
}

// TestScopedOverrideKeyUnbindsOneContext: moving one half of a shared chord
// writes a qualified unbind, so the other context keeps its binding — the
// flat key the settings page and the keymap doctor used to write dropped
// both (#2820). An unshared chord keeps the historical flat spelling.
func TestScopedOverrideKeyUnbindsOneContext(t *testing.T) {
	defaults := Defaults(PresetJetBrains)
	all := BuildTable(defaults, nil, GOOS).Bindings()
	key := ScopedOverrideKey(Explorer, "alt+enter", all)
	if key != "keymap.bindings.explorer.alt+enter" {
		t.Fatalf("key = %q, want the explorer-qualified spelling", key)
	}
	overrides := map[string]string{
		strings.TrimPrefix(key, "keymap.bindings."): "",
		"ctrl+alt+shift+f9":                         "explorer.contextMenu",
	}
	table := BuildTable(defaults, overrides, GOOS)
	chord := MustParseChord("alt+enter")
	if b, ok := table.Lookup(chord, Editor); !ok || b.Command != "lsp.codeAction" {
		t.Fatalf("editor alt+enter = %+v (ok=%v), want lsp.codeAction to survive", b, ok)
	}
	if b, ok := table.Lookup(chord, Explorer); ok && b.Command == "explorer.contextMenu" {
		t.Fatal("the explorer's alt+enter must be unbound")
	}

	// The flat key is what broke the editor: it drops the chord everywhere.
	flat := BuildTable(defaults, map[string]string{"alt+enter": ""}, GOOS)
	if _, ok := flat.Lookup(chord, Editor); ok {
		t.Fatal("the flat unbind is expected to drop every context (the #2820 trap)")
	}

	if got := ScopedOverrideKey(Editor, "ctrl+alt+shift+f9", all); got != "keymap.bindings.ctrl+alt+shift+f9" {
		t.Fatalf("unshared chord key = %q, want the flat spelling", got)
	}
}
