package keymap

import "testing"

// override_test.go covers context-qualified binding keys (0460, #1312).

func TestParseOverrideKey(t *testing.T) {
	cases := []struct {
		raw       string
		chord     string
		context   Context
		qualified bool
		wantErr   bool
	}{
		{raw: "ctrl+s", chord: "ctrl+s", context: Global},
		{raw: "editor.ctrl+s", chord: "ctrl+s", context: Editor, qualified: true},
		{raw: "explorer.ctrl+s", chord: "ctrl+s", context: Explorer, qualified: true},
		{raw: "global.ctrl+s", chord: "ctrl+s", context: Global, qualified: true},
		// A chord that itself contains a dot keeps working: the prefix before
		// the first dot is not a context name, so nothing is consumed.
		{raw: "cmd+.", chord: "cmd+.", context: Global},
		{raw: "editor.cmd+.", chord: "cmd+.", context: Editor, qualified: true},
		{raw: ".", chord: ".", context: Global},
		// A qualifier is not a licence to skip the chord.
		{raw: "editor.", wantErr: true},
		{raw: "focus_left", wantErr: true},
	}
	for _, c := range cases {
		key, err := ParseOverrideKey(c.raw)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseOverrideKey(%q) = %+v, want an error", c.raw, key)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseOverrideKey(%q): %v", c.raw, err)
			continue
		}
		if key.Chord.String() != c.chord || key.Context != c.context || key.Qualified != c.qualified {
			t.Errorf("ParseOverrideKey(%q) = chord %q ctx %q qualified %v, want %q/%q/%v",
				c.raw, key.Chord.String(), key.Context, key.Qualified, c.chord, c.context, c.qualified)
		}
		if got := key.String(); got != c.raw {
			t.Errorf("round-trip of %q = %q", c.raw, got)
		}
	}
}

func TestBindingConfigKey(t *testing.T) {
	if got := BindingConfigKey(Editor, "ctrl+s", false); got != "keymap.bindings.ctrl+s" {
		t.Errorf("unqualified key = %q", got)
	}
	if got := BindingConfigKey(Editor, "ctrl+s", true); got != "keymap.bindings.editor.ctrl+s" {
		t.Errorf("qualified key = %q", got)
	}
	if got := BindingConfigKey(Global, "ctrl+s", true); got != "keymap.bindings.global.ctrl+s" {
		t.Errorf("global key = %q", got)
	}
}

// TestContextQualifiedOverrideKeepsBoth is the point of #1312: one chord runs
// a different command in the editor without disturbing the global binding.
func TestContextQualifiedOverrideKeepsBoth(t *testing.T) {
	defs := []Binding{
		{Chord: MustParseChord("ctrl+g"), Command: "global.cmd", Context: Global, Layer: LayerDefault},
	}
	table := BuildTable(defs, map[string]string{"editor.ctrl+g": "editor.cmd"}, "linux")
	c := MustParseChord("ctrl+g")
	if b, ok := table.Lookup(c, Editor); !ok || b.Command != "editor.cmd" || b.Layer != LayerUser {
		t.Errorf("editor ctrl+g = %+v ok=%v, want editor.cmd @user", b, ok)
	}
	if b, ok := table.Lookup(c, Explorer); !ok || b.Command != "global.cmd" {
		t.Errorf("explorer ctrl+g = %+v ok=%v, want the untouched global binding", b, ok)
	}
	// Two contexts are not a conflict — neither shadows the other.
	if len(table.Conflicts()) != 0 {
		t.Errorf("context-separated bindings must not conflict: %v", table.Conflicts())
	}
}

// TestContextQualifiedOverrideReplacesInPlace: when the context already binds
// the chord, the override replaces that binding instead of adding a second one.
func TestContextQualifiedOverrideReplacesInPlace(t *testing.T) {
	defs := []Binding{
		{Chord: MustParseChord("ctrl+g"), Command: "global.cmd", Context: Global, Layer: LayerDefault},
		{Chord: MustParseChord("ctrl+g"), Command: "editor.cmd", Context: Editor, Layer: LayerDefault},
	}
	table := BuildTable(defs, map[string]string{"editor.ctrl+g": "editor.other"}, "linux")
	if got := len(table.Bindings()); got != 2 {
		t.Fatalf("bindings = %d, want 2", got)
	}
	if b, _ := table.Lookup(MustParseChord("ctrl+g"), Editor); b.Command != "editor.other" {
		t.Errorf("editor ctrl+g = %q, want editor.other", b.Command)
	}
	if b, _ := table.Lookup(MustParseChord("ctrl+g"), Explorer); b.Command != "global.cmd" {
		t.Errorf("global ctrl+g = %q, want global.cmd", b.Command)
	}
}

// TestContextQualifiedUnbindDropsOneContext: the flat form unbinds a chord
// everywhere, the qualified form only in its own context.
func TestContextQualifiedUnbindDropsOneContext(t *testing.T) {
	defs := []Binding{
		{Chord: MustParseChord("ctrl+g"), Command: "global.cmd", Context: Global, Layer: LayerDefault},
		{Chord: MustParseChord("ctrl+g"), Command: "editor.cmd", Context: Editor, Layer: LayerDefault},
	}
	table := BuildTable(defs, map[string]string{"editor.ctrl+g": ""}, "linux")
	if _, ok := table.Lookup(MustParseChord("ctrl+g"), Editor); !ok {
		t.Error("the global binding must still resolve in the editor")
	}
	if b, _ := table.Lookup(MustParseChord("ctrl+g"), Editor); b.Command != "global.cmd" {
		t.Errorf("editor ctrl+g = %q, want the global fallback", b.Command)
	}
	flat := BuildTable(defs, map[string]string{"ctrl+g": ""}, "linux")
	if _, ok := flat.Lookup(MustParseChord("ctrl+g"), Editor); ok {
		t.Error("the flat unbind must drop every context")
	}
}

// TestQualifiedOverrideWinsOverBare guards the application order: the narrower
// statement is applied last whatever the map iteration order was.
func TestQualifiedOverrideWinsOverBare(t *testing.T) {
	defs := []Binding{
		{Chord: MustParseChord("ctrl+g"), Command: "global.cmd", Context: Global, Layer: LayerDefault},
		{Chord: MustParseChord("ctrl+g"), Command: "editor.cmd", Context: Editor, Layer: LayerDefault},
	}
	overrides := map[string]string{
		"ctrl+g":        "everywhere.cmd",
		"editor.ctrl+g": "editor.only",
	}
	// Repeated because the override map's iteration order is random.
	for i := 0; i < 20; i++ {
		table := BuildTable(defs, overrides, "linux")
		if b, _ := table.Lookup(MustParseChord("ctrl+g"), Editor); b.Command != "editor.only" {
			t.Fatalf("editor ctrl+g = %q, want editor.only", b.Command)
		}
		if b, _ := table.Lookup(MustParseChord("ctrl+g"), Explorer); b.Command != "everywhere.cmd" {
			t.Fatalf("explorer ctrl+g = %q, want everywhere.cmd", b.Command)
		}
	}
}

// TestGlobalQualifierTouchesOnlyTheGlobalContext: "global.<chord>" is narrower
// than the bare form — it leaves pane-scoped bindings of the same chord alone.
func TestGlobalQualifierTouchesOnlyTheGlobalContext(t *testing.T) {
	defs := []Binding{
		{Chord: MustParseChord("ctrl+g"), Command: "global.cmd", Context: Global, Layer: LayerDefault},
		{Chord: MustParseChord("ctrl+g"), Command: "editor.cmd", Context: Editor, Layer: LayerDefault},
	}
	table := BuildTable(defs, map[string]string{"global.ctrl+g": "other.cmd"}, "linux")
	if b, _ := table.Lookup(MustParseChord("ctrl+g"), Explorer); b.Command != "other.cmd" {
		t.Errorf("explorer ctrl+g = %q, want other.cmd", b.Command)
	}
	if b, _ := table.Lookup(MustParseChord("ctrl+g"), Editor); b.Command != "editor.cmd" {
		t.Errorf("editor ctrl+g = %q, want the untouched editor binding", b.Command)
	}
}

// TestQualifiedOverrideOnUnknownContextIsADiagnostic: a context that is not a
// pane scope parses as part of the chord and is reported, never applied.
func TestQualifiedOverrideOnUnknownContextIsADiagnostic(t *testing.T) {
	table := BuildTable(nil, map[string]string{"terminal.ctrl+g": "some.cmd"}, "linux")
	if len(table.Bindings()) != 0 {
		t.Fatalf("bindings = %+v, want none", table.Bindings())
	}
	found := false
	for _, d := range table.Diagnostics() {
		if contains(d, "terminal.ctrl+g") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a diagnostic naming the key, got %v", table.Diagnostics())
	}
}

// TestQualifiedOverrideResolvesPerFocusContext drives the resolver, not just
// the table: the same key press runs a different command depending on which
// pane has focus — the whole point of resolving a shared chord by context.
func TestQualifiedOverrideResolvesPerFocusContext(t *testing.T) {
	defs := []Binding{
		{Chord: MustParseChord("ctrl+g"), Command: "global.cmd", Context: Global, Layer: LayerDefault},
	}
	table := BuildTable(defs, map[string]string{"editor.ctrl+g": "editor.cmd"}, "linux")
	press := MustParseChord("ctrl+g").Steps[0]

	r := NewResolver(table)
	if res := r.Feed(press, Editor); res.Status != Resolved || res.Command != "editor.cmd" {
		t.Fatalf("in the editor: %+v, want editor.cmd", res)
	}
	if res := NewResolver(table).Feed(press, Explorer); res.Status != Resolved || res.Command != "global.cmd" {
		t.Fatalf("in the explorer: %+v, want global.cmd", res)
	}
}
