package keymap

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestChordParseFormatRoundTrip(t *testing.T) {
	cases := []struct {
		in   string
		want string // canonical form
	}{
		{"cmd+k cmd+c", "cmd+k cmd+c"},
		{"shift shift", "shift shift"},
		{"esc", "esc"},
		{"cmd+shift+a", "cmd+shift+a"},
		{"shift+cmd+a", "cmd+shift+a"}, // reordered to canonical
		{"alt+f7", "alt+f7"},
		{"cmd+left-bracket", "cmd+left-bracket"},
		{"A", "shift+a"}, // bare uppercase folds to base+shift
		{"escape", "esc"},
	}
	for _, c := range cases {
		got, err := ParseChord(c.in)
		if err != nil {
			t.Fatalf("ParseChord(%q): %v", c.in, err)
		}
		if got.String() != c.want {
			t.Errorf("ParseChord(%q).String() = %q, want %q", c.in, got.String(), c.want)
		}
		// Idempotent canonical round-trip.
		again := MustParseChord(got.String())
		if again.String() != got.String() {
			t.Errorf("round-trip not idempotent for %q: %q vs %q", c.in, got.String(), again.String())
		}
	}
}

func TestParseChordErrors(t *testing.T) {
	for _, s := range []string{"", "   ", "bogus+a", "cmd+"} {
		if _, err := ParseChord(s); err == nil {
			t.Errorf("ParseChord(%q): expected error", s)
		}
	}
}

func TestPlatformNormalisation(t *testing.T) {
	k := MustParseChord("cmd+s").Steps[0]
	// On macOS Cmd stays Meta.
	if got := NormalizeKey(k, "darwin"); !got.Has(ModMeta) || got.Has(ModCtrl) {
		t.Errorf("darwin: cmd+s = %q, want meta kept", got)
	}
	// Off macOS Cmd folds to Ctrl.
	if got := NormalizeKey(k, "linux"); got.Has(ModMeta) || !got.Has(ModCtrl) {
		t.Errorf("linux: cmd+s = %q, want ctrl", got)
	}
	// Idempotent.
	once := NormalizeKey(k, "windows")
	if NormalizeKey(once, "windows") != once {
		t.Errorf("normalisation not idempotent")
	}
}

func TestBuildTableLookupContextPrecedence(t *testing.T) {
	table := BuildTable(Defaults(PresetJetBrains), nil, "linux")
	// cmd+s → ctrl+s in Editor context.
	chord := NormalizeChord(MustParseChord("cmd+s"), "linux")
	if b, ok := table.Lookup(chord, Editor); !ok || b.Command != "editor.write" {
		t.Errorf("editor cmd+s lookup = %+v ok=%v, want editor.write", b, ok)
	}
	// Editor-scoped binding does not resolve in Explorer context.
	if _, ok := table.Lookup(chord, Explorer); ok {
		t.Errorf("editor-scoped cmd+s must not resolve in explorer context")
	}
	// Global binding resolves in any context.
	g := NormalizeChord(MustParseChord("cmd+shift+a"), "linux")
	if b, ok := table.Lookup(g, Explorer); !ok || b.Command != "palette.searchEverywhere" {
		t.Errorf("global cmd+shift+a in explorer = %+v ok=%v", b, ok)
	}
	// shift+f6 is context-aware (0082 sheet 13, #18): the Editor-scoped
	// lsp.rename shadows the Global file.rename, which keeps the chord
	// everywhere else.
	sf6 := MustParseChord("shift+f6")
	if b, ok := table.Lookup(sf6, Editor); !ok || b.Command != "lsp.rename" {
		t.Errorf("editor shift+f6 = %+v ok=%v, want lsp.rename", b, ok)
	}
	if b, ok := table.Lookup(sf6, Explorer); !ok || b.Command != "file.rename" {
		t.Errorf("explorer shift+f6 = %+v ok=%v, want file.rename", b, ok)
	}
	// Diagnostic navigation (#369): f2/shift+f2, Editor-scoped and delivered.
	if b, ok := table.Lookup(MustParseChord("f2"), Editor); !ok || b.Command != "lsp.nextDiagnostic" || b.Fragile {
		t.Errorf("editor f2 = %+v ok=%v, want delivered lsp.nextDiagnostic", b, ok)
	}
	if b, ok := table.Lookup(MustParseChord("shift+f2"), Editor); !ok || b.Command != "lsp.prevDiagnostic" || b.Fragile {
		t.Errorf("editor shift+f2 = %+v ok=%v, want delivered lsp.prevDiagnostic", b, ok)
	}
}

// TestDoubleShiftResolvesOffMacOS drives the resolver's multi-step chord path
// on a non-darwin table (#236): the first bare shift holds as a pending
// prefix, the second resolves palette.searchEverywhere.
func TestDoubleShiftResolvesOffMacOS(t *testing.T) {
	prev := GOOS
	GOOS = "linux"
	defer func() { GOOS = prev }()

	table := BuildTable(Defaults(PresetJetBrains), nil, "linux")
	r := NewResolver(table)
	shift := MustParseChord("shift shift").Steps[0]

	if res := r.Feed(shift, Explorer); res.Status != Pending {
		t.Fatalf("first shift = %+v, want Pending", res)
	}
	res := r.Feed(shift, Explorer)
	if res.Status != Resolved || res.Command != "palette.searchEverywhere" {
		t.Fatalf("second shift = %+v, want palette.searchEverywhere resolved", res)
	}
}

func TestPaneScopeShadowsGlobal(t *testing.T) {
	defs := []Binding{
		{Chord: MustParseChord("ctrl+g"), Command: "global.cmd", Context: Global, Layer: LayerDefault},
		{Chord: MustParseChord("ctrl+g"), Command: "editor.cmd", Context: Editor, Layer: LayerDefault},
	}
	table := BuildTable(defs, nil, "linux")
	c := MustParseChord("ctrl+g")
	if b, _ := table.Lookup(c, Editor); b.Command != "editor.cmd" {
		t.Errorf("editor context should prefer pane-scoped binding, got %q", b.Command)
	}
	if b, _ := table.Lookup(c, Explorer); b.Command != "global.cmd" {
		t.Errorf("explorer context should fall to global binding, got %q", b.Command)
	}
}

func TestOverrideRebindAndUnbind(t *testing.T) {
	overrides := map[string]string{
		"cmd+d":      "editor.somethingElse", // rebind existing
		"cmd+s":      "",                     // unbind
		"ctrl+y":     "custom.thing",         // brand-new binding
		"focus_left": "ctrl+left",            // stopgap non-chord key, ignored
	}
	table := BuildTable(Defaults(PresetJetBrains), overrides, "linux")
	dup := NormalizeChord(MustParseChord("cmd+d"), "linux")
	if b, ok := table.Lookup(dup, Editor); !ok || b.Command != "editor.somethingElse" || b.Layer != LayerUser {
		t.Errorf("rebind cmd+d = %+v ok=%v", b, ok)
	}
	save := NormalizeChord(MustParseChord("cmd+s"), "linux")
	if _, ok := table.Lookup(save, Editor); ok {
		t.Errorf("cmd+s should be unbound")
	}
	if b, ok := table.Lookup(MustParseChord("ctrl+y"), Global); !ok || b.Command != "custom.thing" {
		t.Errorf("new binding ctrl+y = %+v ok=%v", b, ok)
	}
	// The non-chord stopgap key must be ignored as a diagnostic, not crash.
	foundDiag := false
	for _, d := range table.Diagnostics() {
		if contains(d, "focus_left") {
			foundDiag = true
		}
	}
	if !foundDiag {
		t.Errorf("expected diagnostic for ignored override key focus_left; got %v", table.Diagnostics())
	}
}

func TestConflictDetection(t *testing.T) {
	defs := []Binding{
		{Chord: MustParseChord("ctrl+x"), Command: "a.cmd", Context: Global, Layer: LayerDefault},
		{Chord: MustParseChord("ctrl+x"), Command: "b.cmd", Context: Global, Layer: LayerUser},
	}
	table := BuildTable(defs, nil, "linux")
	if len(table.Conflicts()) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(table.Conflicts()))
	}
	// Highest layer (user) wins.
	if b, _ := table.Lookup(MustParseChord("ctrl+x"), Global); b.Command != "b.cmd" {
		t.Errorf("conflict winner = %q, want b.cmd (higher layer)", b.Command)
	}
}

// TestSaveBindsToCtrlS guards that save is reachable via ctrl+s — a key the
// terminal actually delivers — on every platform, including darwin where cmd+s
// is undeliverable. The target id is editor.write, the command the editor
// actually registers (the ex-command ":w").
func TestSaveBindsToCtrlS(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		r := NewResolver(BuildTable(Defaults(PresetJetBrains), nil, goos))
		if res := r.Feed(Key{Base: "s", Mods: ModCtrl}, Editor); res.Status != Resolved || res.Command != "editor.write" {
			t.Fatalf("%s: ctrl+s in editor = %+v, want editor.write", goos, res)
		}
	}
}

// TestUndoBindsToCtrlZ guards that undo is reachable via ctrl+z — a key the
// terminal actually delivers — on every platform, including darwin where cmd+z
// is undeliverable. The chord resolves per context: editor.undo in the editor,
// explorer.undo in the explorer.
func TestUndoBindsToCtrlZ(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		r := NewResolver(BuildTable(Defaults(PresetJetBrains), nil, goos))
		if res := r.Feed(Key{Base: "z", Mods: ModCtrl}, Editor); res.Status != Resolved || res.Command != "editor.undo" {
			t.Fatalf("%s: ctrl+z in editor = %+v, want editor.undo", goos, res)
		}
		if res := r.Feed(Key{Base: "z", Mods: ModCtrl}, Explorer); res.Status != Resolved || res.Command != "explorer.undo" {
			t.Fatalf("%s: ctrl+z in explorer = %+v, want explorer.undo", goos, res)
		}
	}
}

// TestHoverBindsToCtrlQ guards that quick documentation is reachable via
// ctrl+q — the JetBrains Windows/Linux quick-doc chord, delivered on every
// platform because raw mode disables XON flow control (#378) — and that the
// chord classifies as delivered, so the palette shows a non-fragile primary.
func TestHoverBindsToCtrlQ(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		r := NewResolver(BuildTable(Defaults(PresetJetBrains), nil, goos))
		if res := r.Feed(Key{Base: "q", Mods: ModCtrl}, Editor); res.Status != Resolved || res.Command != "lsp.hover" {
			t.Fatalf("%s: ctrl+q in editor = %+v, want lsp.hover", goos, res)
		}
	}
	if got := Classify(MustParseChord("ctrl+q")); got != Delivered {
		t.Fatalf("ctrl+q classifies as %v, want delivered", got)
	}
	if !LeaderCommands()["lsp.hover"] {
		t.Fatal("lsp.hover missing from the leader mnemonic table")
	}
}

func TestMultiStepChordAndTimeout(t *testing.T) {
	table := BuildTable(Defaults(PresetJetBrains), nil, "linux")
	r := NewResolver(table)
	// First step ctrl+k (cmd+k normalised) is both an exact binding (vcs.commit)
	// and a prefix of ctrl+k ctrl+c / ctrl+k ctrl+s → must wait.
	res := r.Feed(Key{Base: "k", Mods: ModCtrl}, Editor)
	if res.Status != Pending {
		t.Fatalf("after ctrl+k: status=%v, want Pending", res.Status)
	}
	// Second step ctrl+c completes the editor.commentLine chord.
	res = r.Feed(Key{Base: "c", Mods: ModCtrl}, Editor)
	if res.Status != Resolved || res.Command != "editor.commentLine" {
		t.Fatalf("ctrl+k ctrl+c = %+v, want editor.commentLine", res)
	}
	// Timeout path: ctrl+k then nothing → exact match vcs.commit resolves.
	res = r.Feed(Key{Base: "k", Mods: ModCtrl}, Global)
	if res.Status != Pending {
		t.Fatalf("ctrl+k pending expected, got %v", res.Status)
	}
	res = r.Timeout(Global)
	if res.Status != Resolved || res.Command != "vcs.commit" {
		t.Fatalf("timeout resolve = %+v, want vcs.commit", res)
	}
}

func TestResolverNoMatchFallsThrough(t *testing.T) {
	table := BuildTable(Defaults(PresetJetBrains), nil, "linux")
	r := NewResolver(table)
	res := r.Feed(Key{Base: "j"}, Editor) // plain j: no global binding
	if res.Status != NoMatch {
		t.Errorf("plain j = %v, want NoMatch", res.Status)
	}
	if r.Pending() {
		t.Errorf("resolver should hold no partial state after NoMatch")
	}
}

func TestResolverAbortedPrefixRestarts(t *testing.T) {
	table := BuildTable(Defaults(PresetJetBrains), nil, "linux")
	r := NewResolver(table)
	r.Feed(Key{Base: "k", Mods: ModCtrl}, Global) // pending ctrl+k
	// A key that neither extends ctrl+k nor matches restarts fresh: f1 resolves.
	res := r.Feed(Key{Base: "f1"}, Global)
	if res.Status != Resolved || res.Command != "palette.keymapHelp" {
		t.Errorf("aborted-prefix restart f1 = %+v, want palette.keymapHelp", res)
	}
}

func TestFromKeyMsg(t *testing.T) {
	cases := []struct {
		msg  tea.KeyPressMsg
		base string
		mods Mod
	}{
		{tea.KeyPressMsg{Text: "a", Code: 'a'}, "a", 0},
		{tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}, "a", ModCtrl},
		{tea.KeyPressMsg{Code: tea.KeyEscape}, "esc", 0},
		{tea.KeyPressMsg{Code: tea.KeyF7}, "f7", 0},
		{tea.KeyPressMsg{Text: "A", Code: 'a', Mod: tea.ModShift}, "a", ModShift},
		{tea.KeyPressMsg{Code: 'a', Mod: tea.ModAlt}, "a", ModAlt},
		// Bracket glyphs normalize to their named bases with and without
		// modifiers (#284) — cmd+[ must match the table's cmd+left-bracket.
		{tea.KeyPressMsg{Text: "[", Code: '['}, "left-bracket", 0},
		{tea.KeyPressMsg{Text: "]", Code: ']'}, "right-bracket", 0},
		{tea.KeyPressMsg{Code: '[', Mod: tea.ModSuper}, "left-bracket", ModMeta},
		{tea.KeyPressMsg{Code: ']', Mod: tea.ModSuper}, "right-bracket", ModMeta},
	}
	for _, c := range cases {
		k, ok := FromKeyMsg(c.msg)
		if !ok {
			t.Errorf("FromKeyMsg(%v): ok=false", c.msg)
			continue
		}
		if k.Base != c.base || k.Mods != c.mods {
			t.Errorf("FromKeyMsg(%v) = %+v, want base=%q mods=%d", c.msg, k, c.base, c.mods)
		}
	}
}

func TestInertBindingMetadataPreserved(t *testing.T) {
	// vcs.* commands have no owner yet; the binding must still exist (inert) so
	// the help sheet can show it. Resolution returns the id regardless of whether
	// a command is registered — that check is the caller's.
	table := BuildTable(Defaults(PresetJetBrains), nil, "linux")
	commit := NormalizeChord(MustParseChord("cmd+k"), "linux")
	if b, ok := table.Lookup(commit, Global); !ok || b.Command != "vcs.commit" {
		t.Errorf("inert vcs.commit binding = %+v ok=%v", b, ok)
	}
}

func TestHelpGroupsSorted(t *testing.T) {
	table := BuildTable(Defaults(PresetJetBrains), nil, "linux")
	groups := table.Help()
	if len(groups) == 0 {
		t.Fatal("no help groups")
	}
	if groups[0].Context != Global {
		t.Errorf("first help group = %q, want global", groups[0].Label)
	}
	for _, g := range groups {
		for i := 1; i < len(g.Entries); i++ {
			if g.Entries[i-1].Chord > g.Entries[i].Chord {
				t.Errorf("group %q entries not sorted by chord", g.Label)
			}
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestPageKeyAliases(t *testing.T) {
	for in, want := range map[string]string{
		"ctrl+pageup":   "ctrl+pgup",
		"ctrl+pagedown": "ctrl+pgdown",
		"ctrl+pgdn":     "ctrl+pgdown",
	} {
		k, err := ParseKey(in)
		if err != nil {
			t.Fatalf("ParseKey(%q): %v", in, err)
		}
		if k.String() != want {
			t.Errorf("ParseKey(%q) = %q, want %q", in, k, want)
		}
	}
}
