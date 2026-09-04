package keymap

import (
	"strings"
	"testing"
)

// TestUserBindingShadowsGlobalDefault is the #1875 repro: a user binding
// "editor.cmd+e" → http.selectEnvironment hides the global default cmd+e →
// palette.recentFiles whenever an editor is focused. That must surface as a
// shadow diagnostic naming both commands and a resolution hint.
func TestUserBindingShadowsGlobalDefault(t *testing.T) {
	overrides := map[string]string{"editor.cmd+e": "http.selectEnvironment"}
	table := BuildTable(DefaultsFor(PresetJetBrains, "darwin"), overrides, "darwin")

	var hit *Shadow
	for i, s := range table.Shadows() {
		if s.Winner.Command == "http.selectEnvironment" {
			hit = &table.Shadows()[i]
		}
	}
	if hit == nil {
		t.Fatalf("no shadow reported for editor.cmd+e over the global cmd+e; shadows: %v", table.Shadows())
	}
	if hit.Hidden.Command != "palette.recentFiles" {
		t.Errorf("hidden command = %q, want palette.recentFiles", hit.Hidden.Command)
	}
	if hit.Winner.Context != Editor || hit.Hidden.Context != Global {
		t.Errorf("contexts = winner %q / hidden %q, want editor / global", hit.Winner.Context, hit.Hidden.Context)
	}
	// The diagnostic must be part of the table's diagnostics and carry a
	// resolution hint (the qualified key to unbind).
	found := false
	for _, d := range table.Diagnostics() {
		if strings.Contains(d, "keymap shadow") && strings.Contains(d, "http.selectEnvironment") {
			found = true
			if !strings.Contains(d, "palette.recentFiles") || !strings.Contains(d, "editor.cmd+e") {
				t.Errorf("diagnostic lacks hidden command or resolution key: %q", d)
			}
		}
	}
	if !found {
		t.Errorf("shadow missing from Diagnostics(): %v", table.Diagnostics())
	}
	// The runtime behavior itself is unchanged: editor focus resolves to the
	// user command, everywhere else the default still wins.
	if b, ok := table.Lookup(MustParseChord("cmd+e"), Editor); !ok || b.Command != "http.selectEnvironment" {
		t.Errorf("editor lookup = %+v, %v", b, ok)
	}
	if b, ok := table.Lookup(MustParseChord("cmd+e"), Explorer); !ok || b.Command != "palette.recentFiles" {
		t.Errorf("explorer lookup = %+v, %v", b, ok)
	}
}

// TestDefaultVsDefaultShadowDetected: an unlisted default-vs-default layering
// is reported — the allowlist is the only silencer.
func TestDefaultVsDefaultShadowDetected(t *testing.T) {
	bindings := []Binding{
		{Chord: MustParseChord("ctrl+x"), Command: "a.one", Context: Global, Layer: LayerDefault},
		{Chord: MustParseChord("ctrl+x"), Command: "b.two", Context: Editor, Layer: LayerDefault},
	}
	shadows := detectShadows(bindings)
	if len(shadows) != 1 {
		t.Fatalf("want 1 shadow, got %v", shadows)
	}
	if shadows[0].Winner.Command != "b.two" || shadows[0].Hidden.Command != "a.one" {
		t.Errorf("shadow = %+v", shadows[0])
	}
}

// TestSameCommandIsNoShadow: the dual-context pattern (same command bound
// Global and pane-scoped, or in two panes) is not a conflict.
func TestSameCommandIsNoShadow(t *testing.T) {
	bindings := []Binding{
		{Chord: MustParseChord("ctrl+x"), Command: "a.one", Context: Global, Layer: LayerDefault},
		{Chord: MustParseChord("ctrl+x"), Command: "a.one", Context: Editor, Layer: LayerUser},
	}
	if shadows := detectShadows(bindings); len(shadows) != 0 {
		t.Errorf("same command reported as shadow: %v", shadows)
	}
}

// TestSeparatePaneContextsAreNoShadow: two pane-scoped bindings in different
// panes never overlap — "keep both, resolve by context" (#1312) stays quiet.
func TestSeparatePaneContextsAreNoShadow(t *testing.T) {
	bindings := []Binding{
		{Chord: MustParseChord("ctrl+x"), Command: "a.one", Context: Explorer, Layer: LayerUser},
		{Chord: MustParseChord("ctrl+x"), Command: "b.two", Context: Editor, Layer: LayerUser},
	}
	if shadows := detectShadows(bindings); len(shadows) != 0 {
		t.Errorf("separable contexts reported as shadow: %v", shadows)
	}
}

// TestAllowlistOnlySilencesDefaults: an allowlisted pair is still reported
// when one side comes from a user override — the allowlist speaks only about
// the shipped default set.
func TestAllowlistOnlySilencesDefaults(t *testing.T) {
	bindings := []Binding{
		{Chord: MustParseChord("shift+f6"), Command: "file.rename", Context: Global, Layer: LayerDefault},
		{Chord: MustParseChord("shift+f6"), Command: "lsp.rename", Context: Editor, Layer: LayerUser},
	}
	if shadows := detectShadows(bindings); len(shadows) != 1 {
		t.Errorf("user-layer allowlisted pair not reported: %v", shadows)
	}
	bindings[1].Layer = LayerDefault
	if shadows := detectShadows(bindings); len(shadows) != 0 {
		t.Errorf("default-layer allowlisted pair reported: %v", shadows)
	}
}

// TestUnbindResolvesShadow: unbinding the qualified user chord (the hint the
// diagnostic gives) removes the shadow.
func TestUnbindResolvesShadow(t *testing.T) {
	overrides := map[string]string{
		"editor.cmd+e": "http.selectEnvironment",
	}
	table := BuildTable(DefaultsFor(PresetJetBrains, "darwin"), overrides, "darwin")
	if len(table.Shadows()) == 0 {
		t.Fatal("precondition: shadow expected")
	}
	overrides["editor.cmd+e"] = ""
	table = BuildTable(DefaultsFor(PresetJetBrains, "darwin"), overrides, "darwin")
	for _, s := range table.Shadows() {
		if s.Winner.Command == "http.selectEnvironment" {
			t.Errorf("shadow survived the unbind: %+v", s)
		}
	}
}
