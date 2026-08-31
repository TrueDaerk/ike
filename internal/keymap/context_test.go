package keymap

import "testing"

// paneContexts is every pane-scoped context of the #1794 set, for the
// table-driven checks below.
var paneContexts = []Context{
	Editor, Explorer, Palette, Diff, Terminal, Preview,
	VCS, Debug, Problems, Structure, Usages, HTTP, Breakpoints, Archive, Data,
}

// TestContextMatchesAndSpecificity (#1794): the two-level specificity order
// holds over the full context set — Global matches everywhere and loses to any
// pane context; pane contexts are mutually disjoint and never outrank each
// other.
func TestContextMatchesAndSpecificity(t *testing.T) {
	for _, c := range paneContexts {
		if !Global.Matches(c) {
			t.Errorf("Global must match in %q", c)
		}
		if !c.Matches(c) {
			t.Errorf("%q must match itself", c)
		}
		if !c.MoreSpecific(Global) {
			t.Errorf("%q must be more specific than Global", c)
		}
		if Global.MoreSpecific(c) {
			t.Errorf("Global must not outrank %q", c)
		}
		for _, o := range paneContexts {
			if o == c {
				continue
			}
			if c.Matches(o) {
				t.Errorf("%q must not match in %q", c, o)
			}
			if c.MoreSpecific(o) {
				t.Errorf("pane contexts are disjoint: %q must not outrank %q", c, o)
			}
		}
	}
}

// TestOverrideKeyPaneContexts (#1794): every config context spelling parses as
// a qualified override key and round-trips through ContextName.
func TestOverrideKeyPaneContexts(t *testing.T) {
	for _, name := range ContextNames() {
		key, err := ParseOverrideKey(name + ".ctrl+g")
		if err != nil {
			t.Fatalf("%s.ctrl+g: %v", name, err)
		}
		if !key.Qualified {
			t.Errorf("%s.ctrl+g should parse as a qualified key", name)
		}
		if got := ContextName(key.Context); got != name {
			t.Errorf("%s.ctrl+g parsed context %q", name, got)
		}
	}
}

// TestPerContextCtrlT (#1794): the acceptance chord — ctrl+t resolves to a new
// terminal tab in the terminal context and a new empty editor tab in the
// editor context, stays unbound everywhere else, and the disjoint pair is
// neither a conflict nor a shadow.
func TestPerContextCtrlT(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		table := BuildTable(DefaultsFor(PresetJetBrains, goos), nil, goos)
		chord := NormalizeChord(MustParseChord("ctrl+t"), goos)
		if b, ok := table.Lookup(chord, Terminal); !ok || b.Command != "terminal.newTab" {
			t.Errorf("%s: terminal ctrl+t = %+v ok=%v, want terminal.newTab", goos, b, ok)
		}
		if b, ok := table.Lookup(chord, Editor); !ok || b.Command != "editor.tab.new" {
			t.Errorf("%s: editor ctrl+t = %+v ok=%v, want editor.tab.new", goos, b, ok)
		}
		for _, ctx := range []Context{Global, Explorer, Data, Preview} {
			if b, ok := table.Lookup(chord, ctx); ok {
				t.Errorf("%s: ctrl+t must stay unbound in %q, got %q", goos, contextLabel(ctx), b.Command)
			}
		}
		for _, c := range table.Conflicts() {
			if c.Chord == chord.String() {
				t.Errorf("%s: disjoint-context ctrl+t reported as conflict: %+v", goos, c)
			}
		}
		for _, s := range table.Shadows() {
			if s.Chord == chord.String() {
				t.Errorf("%s: ctrl+t reported as shadow: %+v", goos, s)
			}
		}
	}
}

// TestPaneContextOverrideResolution (#1794): a user override qualified with one
// of the new pane contexts binds there and only there, shadowing Global on
// that chord while the pane has focus — and two more qualified overrides on
// the same chord in other panes coexist conflict-free.
func TestPaneContextOverrideResolution(t *testing.T) {
	defs := []Binding{
		{Chord: MustParseChord("ctrl+g"), Command: "global.cmd", Context: Global, Layer: LayerDefault},
	}
	overrides := map[string]string{
		"data.ctrl+g":     "data.cmd",
		"terminal.ctrl+g": "terminal.cmd",
		"archive.ctrl+g":  "archive.cmd",
	}
	table := BuildTable(defs, overrides, "linux")
	if len(table.Conflicts()) != 0 {
		t.Fatalf("disjoint pane overrides must not conflict: %+v", table.Conflicts())
	}
	chord := MustParseChord("ctrl+g")
	want := map[Context]string{
		Data:     "data.cmd",
		Terminal: "terminal.cmd",
		Archive:  "archive.cmd",
		Editor:   "global.cmd", // untouched context falls back to Global
		Global:   "global.cmd",
	}
	for ctx, cmd := range want {
		if b, ok := table.Lookup(chord, ctx); !ok || b.Command != cmd {
			t.Errorf("%s ctrl+g = %+v ok=%v, want %s", contextLabel(ctx), b, ok, cmd)
		}
	}
	// The pane-over-Global layerings surface as shadows (visibility, #1875) —
	// three of them, and none is a conflict.
	if got := len(table.Shadows()); got != 3 {
		t.Errorf("shadows = %d, want 3 (one per pane override): %+v", got, table.Shadows())
	}
}
