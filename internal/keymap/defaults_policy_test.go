package keymap

import (
	"testing"
)

func key(t *testing.T, s string) Key {
	t.Helper()
	k, err := ParseKey(s)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// TestNoSameContextConflicts: the default table builds without same-chord/
// same-context clashes on either platform.
func TestNoSameContextConflicts(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		table := BuildTable(Defaults(PresetJetBrains), nil, goos)
		for _, c := range table.Conflicts() {
			t.Errorf("%s: conflict %+v", goos, c)
		}
	}
}

// TestFragileFlagsDeriveFromReachability: the hand-maintained flags are gone;
// every default row's Fragile mirrors the ground-truth classification.
func TestFragileFlagsDeriveFromReachability(t *testing.T) {
	for _, b := range Defaults(PresetJetBrains) {
		want := Classify(b.Chord) != Delivered
		if b.Fragile != want {
			t.Errorf("%s: Fragile=%v, reachability says %v", b.Chord, b.Fragile, want)
		}
	}
}

// TestFragileDefaultsHaveReachableAlternative documents the escape routes:
// every fragile, non-blocked default command has another delivered chord or
// sits on the documented alternatives list (vim-native editor operations,
// delivered keys, or the palette via esc esc).
func TestFragileDefaultsHaveReachableAlternative(t *testing.T) {
	exceptions := reachableAlternatives
	delivered := map[string]bool{}
	for _, b := range Defaults(PresetJetBrains) {
		if Classify(b.Chord) == Delivered {
			delivered[b.Command] = true
		}
	}
	for _, b := range Defaults(PresetJetBrains) {
		if !b.Fragile || b.Command == "" {
			continue
		}
		if _, blocked := BlockedReason(b.Command); blocked {
			continue // inert until its roadmap lands; nothing to alias yet
		}
		if delivered[b.Command] {
			continue
		}
		if _, ok := exceptions[b.Command]; !ok {
			t.Errorf("%s (%s) is fragile with no delivered alternative and no documented exception", b.Command, b.Chord)
		}
	}
}

// TestAllDefaultsAreModifierChords enforces the #711 policy: every default
// binding is a single modifier chord (or F-key/named key), except the
// deliberate cmd+k sequence family (at most five) and JetBrains' double-shift
// double-tap.
func TestAllDefaultsAreModifierChords(t *testing.T) {
	multiStep := map[string]bool{}
	for _, b := range Defaults(PresetJetBrains) {
		s := b.Chord.String()
		if b.Chord.Len() == 1 {
			continue
		}
		if s == "shift shift" {
			continue // JetBrains double-shift, a double-tap not a sequence
		}
		multiStep[s] = true
		if first := b.Chord.Steps[0]; first.Mods == 0 {
			t.Errorf("%s starts with an unmodified key — leader-style sequences are retired (#711)", s)
		}
	}
	if len(multiStep) > 5 {
		t.Errorf("multi-step default sequences = %d, policy allows at most 5: %v", len(multiStep), multiStep)
	}
}

// TestRetiredDefaults: replaced chords resolve their new owners.
func TestRetiredDefaults(t *testing.T) {
	cases := []struct {
		keys []string
		ctx  Context
		cmd  string
	}{
		{[]string{"cmd+shift+t"}, Global, "editor.tab.reopenClosed"},
		{[]string{"alt+shift+t"}, Global, "editor.tab.reopenClosed"},
		{[]string{"cmd+alt+z"}, Global, "vcs.revertFile"},
		{[]string{"cmd+9"}, Global, "vcs.panel"},
		{[]string{"cmd+alt+m"}, Editor, "markdown.preview"},
		{[]string{"cmd+alt+t"}, Global, "terminal.new"},
		{[]string{"cmd+alt+n"}, Global, "notifications.history"},
		{[]string{"cmd+alt+shift+right"}, Global, "editor.splitViewRight"},
		{[]string{"cmd+alt+shift+down"}, Global, "editor.splitViewDown"},
		{[]string{"cmd+k", "z"}, Global, "pane.maximize"},
		{[]string{"cmd+k", "down"}, Global, "pane.splitDown"},
	}
	for _, c := range cases {
		r := NewResolver(BuildTable(Defaults(PresetJetBrains), nil, "darwin"))
		for i, k := range c.keys {
			res := r.Feed(key(t, k), c.ctx)
			if i < len(c.keys)-1 {
				continue
			}
			if res.Status != Resolved || res.Command != c.cmd {
				t.Errorf("%v: got %+v, want %s", c.keys, res, c.cmd)
			}
		}
	}
	// The leader prefix is gone: a bare space matches nothing at the top level.
	r := NewResolver(BuildTable(Defaults(PresetJetBrains), nil, "darwin"))
	if res := r.Feed(key(t, "space"), Global); res.Status == Pending {
		t.Error("bare space must not open a pending sequence anymore")
	}
}

// TestAuditDefaultChords (#1378): the unbound-command audit's new defaults
// resolve on both platforms in their scoped context.
func TestAuditDefaultChords(t *testing.T) {
	cases := []struct {
		chord string
		ctx   Context
		cmd   string
	}{
		{"cmd+f12", Global, "lsp.documentSymbols"},
		{"cmd+y", Editor, "lsp.peekDefinition"},
		{"cmd+alt+f7", Editor, "lsp.referencesPanel"},
		{"ctrl+shift+f10", Global, "run.testAtCursor"},
		{"cmd+f3", Global, "nav.bookmarks"},
	}
	for _, goos := range []string{"darwin", "linux"} {
		table := BuildTable(Defaults(PresetJetBrains), nil, goos)
		for _, c := range cases {
			chord := NormalizeChord(MustParseChord(c.chord), goos)
			if b, ok := table.Lookup(chord, c.ctx); !ok || b.Command != c.cmd {
				t.Errorf("%s: %s = %+v ok=%v, want %s", goos, c.chord, b, ok, c.cmd)
			}
		}
	}
}

// TestDebugFKeyPlatformDefaults (#1374): the run/debug F-key family ships the
// JetBrains macOS cmd form as the darwin primary with the Windows-scheme ctrl
// form alongside; off macOS both fold onto the ctrl chord. Plain ctrl+F-keys
// classify fragile on darwin (macOS system shortcuts swallow them) and
// delivered elsewhere.
func TestDebugFKeyPlatformDefaults(t *testing.T) {
	prev := GOOS
	defer func() { GOOS = prev }()

	pairs := []struct {
		cmd, ctrl, id string
		ctx           Context
	}{
		{"cmd+f8", "ctrl+f8", "debug.toggleBreakpoint", Global},
		{"cmd+f2", "ctrl+f2", "debug.stop", Global},
		{"cmd+f5", "ctrl+f5", "run.rerun", Global},
		{"cmd+f1", "ctrl+f1", "lsp.diagnosticInfo", Editor},
	}
	for _, goos := range []string{"darwin", "linux"} {
		GOOS = goos
		table := BuildTable(Defaults(PresetJetBrains), nil, goos)
		for _, p := range pairs {
			for _, chord := range []string{p.cmd, p.ctrl} {
				c := NormalizeChord(MustParseChord(chord), goos)
				if b, ok := table.Lookup(c, p.ctx); !ok || b.Command != p.id {
					t.Errorf("%s: %s = %+v ok=%v, want %s", goos, chord, b, ok, p.id)
				}
			}
		}
	}

	// Classification: plain ctrl+F-keys are system-eaten on darwin only;
	// shifted ones carry their modifier in the CSI parameter and stay
	// delivered everywhere.
	GOOS = "darwin"
	if got := Classify(MustParseChord("ctrl+f8")); got != Fragile {
		t.Errorf("darwin ctrl+f8 = %v, want Fragile (macOS system shortcut)", got)
	}
	if got := Classify(MustParseChord("ctrl+shift+f10")); got != Delivered {
		t.Errorf("darwin ctrl+shift+f10 = %v, want Delivered", got)
	}
	GOOS = "linux"
	if got := Classify(MustParseChord("ctrl+f8")); got != Delivered {
		t.Errorf("linux ctrl+f8 = %v, want Delivered", got)
	}
}

// TestCloseProjectDefaultChords (#1358): project.close ships on cmd+shift+w
// with the delivered ctrl+shift+w secondary, mirroring project.switch.
func TestCloseProjectDefaultChords(t *testing.T) {
	want := map[string]bool{"cmd+shift+w": false, "ctrl+shift+w": false}
	for _, b := range Defaults(PresetJetBrains) {
		if b.Command != "project.close" {
			continue
		}
		if _, ok := want[b.Chord.String()]; ok {
			want[b.Chord.String()] = true
		}
	}
	for chord, found := range want {
		if !found {
			t.Errorf("project.close default %s missing", chord)
		}
	}
}
