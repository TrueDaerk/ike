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
