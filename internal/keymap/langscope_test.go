package keymap

import (
	"strings"
	"testing"
)

// langscope_test.go covers language-scoped bindings (#1876): the editor[lang]
// context level, its override-key grammar, the three-level lookup precedence
// and the widened shadow detection.

// TestLangScopedContextSemantics: editor[lang] matches only an active editor
// context carrying the same language, outranks both editor and Global, and
// Shadows reports exactly the pairs that can overlap.
func TestLangScopedContextSemantics(t *testing.T) {
	http := WithLang(Editor, "http")
	golang := WithLang(Editor, "go")

	if got := string(http); got != "editor[http]" {
		t.Fatalf("WithLang(Editor, http) = %q", got)
	}
	if b, l := http.Split(); b != Editor || l != "http" {
		t.Errorf("Split(editor[http]) = %q/%q", b, l)
	}
	// WithLang narrows the editor context only.
	if got := WithLang(Diff, "go"); got != Diff {
		t.Errorf("WithLang(Diff, go) = %q, want diff unchanged", got)
	}
	if got := WithLang(Editor, ""); got != Editor {
		t.Errorf("WithLang(Editor, \"\") = %q, want editor unchanged", got)
	}

	// Matches: the binding side may be broader than the active side, never
	// narrower.
	for c, active := range map[Context]Context{
		Global: http, Editor: http, http: http,
	} {
		if !c.Matches(active) {
			t.Errorf("%q must match active %q", c, active)
		}
	}
	if http.Matches(Editor) {
		t.Error("editor[http] must not match a language-less editor focus")
	}
	if http.Matches(golang) {
		t.Error("editor[http] must not match an editor[go] focus")
	}
	if http.Matches(Global) || http.Matches(Diff) {
		t.Error("editor[http] must not match outside the editor")
	}

	// Specificity: editor[lang] > editor > Global; language scopes are
	// mutually disjoint.
	if !http.MoreSpecific(Editor) || !http.MoreSpecific(Global) || !Editor.MoreSpecific(Global) {
		t.Error("specificity order editor[lang] > editor > global broken")
	}
	if Editor.MoreSpecific(http) || http.MoreSpecific(golang) {
		t.Error("inverse/sibling specificity must not hold")
	}

	// Shadows: only pairs that can both match a focus.
	if !http.Shadows(Editor) || !http.Shadows(Global) || !Editor.Shadows(Global) {
		t.Error("editor[http] must shadow editor and global; editor must shadow global")
	}
	if http.Shadows(golang) || http.Shadows(Diff) || Editor.Shadows(http) {
		t.Error("disjoint or broader scopes must not shadow")
	}
}

// TestParseLangScopedOverrideKey: the editor[lang] qualifier parses, round-trips
// and is rejected on non-editor contexts and malformed qualifiers.
func TestParseLangScopedOverrideKey(t *testing.T) {
	key, err := ParseOverrideKey("editor[http].cmd+e")
	if err != nil {
		t.Fatalf("editor[http].cmd+e: %v", err)
	}
	if !key.Qualified || key.Context != WithLang(Editor, "http") || key.Chord.String() != "cmd+e" {
		t.Errorf("parsed %+v", key)
	}
	if got := key.String(); got != "editor[http].cmd+e" {
		t.Errorf("round-trip = %q", got)
	}
	if got := BindingConfigKey(WithLang(Editor, "http"), "cmd+e", true); got != "keymap.bindings.editor[http].cmd+e" {
		t.Errorf("BindingConfigKey = %q", got)
	}
	// Invalid qualifiers are not contexts; the whole key then has to parse as
	// a chord, which these cannot, so they surface as errors (diagnostics).
	for _, raw := range []string{
		"diff[go].ctrl+g",      // language scope is editor-only
		"editor[].ctrl+g",      // empty language
		"editor[ht tp].ctrl+g", // qualifier shape
		"editor[http.ctrl+g",   // unclosed bracket
	} {
		if key, err := ParseOverrideKey(raw); err == nil {
			t.Errorf("ParseOverrideKey(%q) = %+v, want an error", raw, key)
		}
	}
	// Bracket chords keep working: "[" is a chord key, not a qualifier.
	if key, err := ParseOverrideKey("cmd+["); err != nil || key.Qualified {
		t.Errorf("cmd+[ parsed as %+v, %v", key, err)
	}
	if key, err := ParseOverrideKey("editor.cmd+["); err != nil || key.Context != Editor {
		t.Errorf("editor.cmd+[ parsed as %+v, %v", key, err)
	}
}

// TestLangScopedLookupPrecedence is the #1876 acceptance chord: an
// editor[http] override wins only in editors focused on an http buffer; a
// language-less or differently-classified editor falls back through editor to
// the global default.
func TestLangScopedLookupPrecedence(t *testing.T) {
	defaults := []Binding{
		{Chord: MustParseChord("cmd+e"), Command: "palette.recentFiles", Context: Global, Layer: LayerDefault},
	}
	overrides := map[string]string{
		"editor[http].cmd+e": "http.selectEnvironment",
	}
	table := BuildTable(defaults, overrides, "darwin")
	chord := MustParseChord("cmd+e")

	for active, want := range map[Context]string{
		WithLang(Editor, "http"): "http.selectEnvironment",
		WithLang(Editor, "go"):   "palette.recentFiles",
		Editor:                   "palette.recentFiles",
		Explorer:                 "palette.recentFiles",
		Global:                   "palette.recentFiles",
	} {
		if b, ok := table.Lookup(chord, active); !ok || b.Command != want {
			t.Errorf("lookup in %q = %q ok=%v, want %q", active, b.Command, ok, want)
		}
	}

	// All three levels stacked: the narrowest matching one wins.
	overrides["editor.cmd+e"] = "editor.other"
	table = BuildTable(defaults, overrides, "darwin")
	for active, want := range map[Context]string{
		WithLang(Editor, "http"): "http.selectEnvironment",
		WithLang(Editor, "go"):   "editor.other",
		Editor:                   "editor.other",
		Global:                   "palette.recentFiles",
	} {
		if b, ok := table.Lookup(chord, active); !ok || b.Command != want {
			t.Errorf("stacked lookup in %q = %q ok=%v, want %q", active, b.Command, ok, want)
		}
	}
}

// TestLangScopedResolverFeed: the resolver resolves per language through the
// active context it is fed — no signature change, the context carries the lang.
func TestLangScopedResolverFeed(t *testing.T) {
	defaults := []Binding{
		{Chord: MustParseChord("cmd+e"), Command: "palette.recentFiles", Context: Global, Layer: LayerDefault},
	}
	table := BuildTable(defaults, map[string]string{"editor[http].cmd+e": "http.selectEnvironment"}, "darwin")
	key := MustParseChord("cmd+e").Steps[0]

	r := NewResolver(table)
	if res := r.Feed(key, WithLang(Editor, "http")); res.Status != Resolved || res.Command != "http.selectEnvironment" {
		t.Errorf("editor[http] feed = %+v", res)
	}
	if res := r.Feed(key, WithLang(Editor, "go")); res.Status != Resolved || res.Command != "palette.recentFiles" {
		t.Errorf("editor[go] feed = %+v", res)
	}
	if res := r.Feed(key, Editor); res.Status != Resolved || res.Command != "palette.recentFiles" {
		t.Errorf("plain editor feed = %+v", res)
	}
}

// TestLangScopedShadowDetection: an editor[http] binding shadows the editor
// and Global bindings of the chord — only those — and the diagnostic names the
// language-qualified key to unbind. Sibling language scopes stay quiet.
func TestLangScopedShadowDetection(t *testing.T) {
	bindings := []Binding{
		{Chord: MustParseChord("cmd+e"), Command: "a.global", Context: Global, Layer: LayerDefault},
		{Chord: MustParseChord("cmd+e"), Command: "b.editor", Context: Editor, Layer: LayerUser},
		{Chord: MustParseChord("cmd+e"), Command: "c.http", Context: WithLang(Editor, "http"), Layer: LayerUser},
		{Chord: MustParseChord("cmd+e"), Command: "d.go", Context: WithLang(Editor, "go"), Layer: LayerUser},
	}
	got := map[string]bool{}
	for _, s := range detectShadows(bindings) {
		got[s.Winner.Command+">"+s.Hidden.Command] = true
	}
	want := []string{"b.editor>a.global", "c.http>a.global", "c.http>b.editor", "d.go>a.global", "d.go>b.editor"}
	for _, pair := range want {
		if !got[pair] {
			t.Errorf("missing shadow %s (got %v)", pair, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("extra shadows: got %v, want exactly %v", got, want)
	}

	// End to end through BuildTable: the diagnostic carries the qualified key.
	defaults := []Binding{
		{Chord: MustParseChord("cmd+e"), Command: "palette.recentFiles", Context: Global, Layer: LayerDefault},
	}
	table := BuildTable(defaults, map[string]string{"editor[http].cmd+e": "http.selectEnvironment"}, "darwin")
	if n := len(table.Shadows()); n != 1 {
		t.Fatalf("want 1 shadow, got %v", table.Shadows())
	}
	found := false
	for _, d := range table.Diagnostics() {
		if strings.Contains(d, "keymap shadow") && strings.Contains(d, "editor[http].cmd+e") {
			found = true
		}
	}
	if !found {
		t.Errorf("diagnostic lacks the qualified key: %v", table.Diagnostics())
	}
}

// TestLangQualifiedUnbind: a language-qualified ""-override drops only the
// language-scoped binding, leaving the broader levels alone.
func TestLangQualifiedUnbind(t *testing.T) {
	defaults := []Binding{
		{Chord: MustParseChord("cmd+e"), Command: "palette.recentFiles", Context: Global, Layer: LayerDefault},
	}
	overrides := map[string]string{
		"editor[http].cmd+e": "http.selectEnvironment",
	}
	table := BuildTable(defaults, overrides, "darwin")
	if b, _ := table.Lookup(MustParseChord("cmd+e"), WithLang(Editor, "http")); b.Command != "http.selectEnvironment" {
		t.Fatalf("precondition: lang override expected, got %q", b.Command)
	}
	overrides["editor[http].cmd+e"] = ""
	table = BuildTable(defaults, overrides, "darwin")
	if b, ok := table.Lookup(MustParseChord("cmd+e"), WithLang(Editor, "http")); !ok || b.Command != "palette.recentFiles" {
		t.Errorf("after unbind, editor[http] lookup = %q ok=%v, want the global default", b.Command, ok)
	}
}
