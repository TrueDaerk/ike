package keymap

import "testing"

// TestDroppedDefaultTracked (#2539): an unbind override removes a default from
// resolution but the table still knows it was there, so the usage log can say
// "bound by default, removed by config" instead of a bare "unbound".
func TestDroppedDefaultTracked(t *testing.T) {
	tbl := BuildTable(Defaults(PresetJetBrains), map[string]string{"alt+shift+up": ""}, "darwin")
	c := MustParseChord("alt+shift+up")
	if _, ok := tbl.Lookup(c, WithLang(Editor, "json")); ok {
		t.Fatal("unbound chord must not resolve")
	}
	b, ok := tbl.Dropped(c, WithLang(Editor, "json"))
	if !ok || b.Command != "editor.caret.addAbove" {
		t.Fatalf("Dropped = %+v, %v; want the editor.caret.addAbove default", b, ok)
	}
	if _, ok := tbl.Dropped(c, Explorer); ok {
		t.Fatal("the dropped Editor default must not report in the explorer context")
	}
	if _, ok := tbl.Dropped(MustParseChord("ctrl+alt+0"), Editor); ok {
		t.Fatal("a chord no default ever bound reports nothing")
	}
}

// TestDroppedOnlyDefaults: a user binding removed again is not a dropped
// default, and a plain default table drops nothing.
func TestDroppedOnlyDefaults(t *testing.T) {
	tbl := BuildTable(Defaults(PresetJetBrains), nil, "darwin")
	if _, ok := tbl.Dropped(MustParseChord("alt+shift+up"), Editor); ok {
		t.Fatal("no override, nothing dropped")
	}
	// A qualified unbind touches one context only: the editor default for
	// cmd+c stays while the debug one goes.
	tbl = BuildTable(Defaults(PresetJetBrains), map[string]string{"debug.cmd+c": ""}, "darwin")
	c := MustParseChord("cmd+c")
	if _, ok := tbl.Dropped(c, Debug); !ok {
		t.Fatal("the debug cmd+c default was unbound and must be reported dropped")
	}
	if _, ok := tbl.Dropped(c, Editor); ok {
		t.Fatal("the editor cmd+c default was untouched")
	}
}
