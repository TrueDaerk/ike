package keymap

import (
	"testing"
)

// leaderTable builds the full default table plus the leader layer.
func leaderTable(t *testing.T, leader string) *BindingTable {
	t.Helper()
	rows := append(Defaults(PresetJetBrains), LeaderRows(leader)...)
	return BuildTable(rows, nil, "darwin")
}

func key(t *testing.T, s string) Key {
	t.Helper()
	k, err := ParseKey(s)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestLeaderResolvesThroughChordEngine(t *testing.T) {
	r := NewResolver(leaderTable(t, "space"))

	res := r.Feed(key(t, "space"), Global)
	if res.Status != Pending {
		t.Fatalf("leader press should hold pending, got %v", res.Status)
	}
	res = r.Feed(key(t, "f"), Global)
	if res.Status != Resolved || res.Command != "project.goToFile" {
		t.Fatalf("space f should resolve go-to-file, got %+v", res)
	}

	// The universal ctrl+k variant resolves the same mnemonic.
	res = r.Feed(key(t, "ctrl+k"), Editor)
	if res.Status != Pending {
		t.Fatalf("ctrl+k should hold pending, got %v", res.Status)
	}
	res = r.Feed(key(t, "e"), Editor)
	if res.Status != Resolved || res.Command != "explorer.toggle" {
		t.Fatalf("ctrl+k e should resolve explorer toggle, got %+v", res)
	}
}

func TestLeaderTimeoutDropsThePrefix(t *testing.T) {
	r := NewResolver(leaderTable(t, "space"))
	if res := r.Feed(key(t, "space"), Global); res.Status != Pending {
		t.Fatalf("status = %v", res.Status)
	}
	res := r.Timeout(Global)
	if res.Status == Resolved && res.Command != "" {
		t.Fatalf("a lone leader must not resolve a command, got %+v", res)
	}
	if r.Pending() {
		t.Fatal("timeout should clear the pending prefix")
	}
	// The engine is usable again afterwards.
	r.Feed(key(t, "space"), Global)
	if res := r.Feed(key(t, "p"), Global); res.Status != Resolved || res.Command != "project.switch" {
		t.Fatalf("space p after timeout should resolve, got %+v", res)
	}
}

func TestLeaderConfigurablePrefix(t *testing.T) {
	r := NewResolver(leaderTable(t, "§"))
	r.Feed(key(t, "§"), Global)
	if res := r.Feed(key(t, "t"), Global); res.Status != Resolved || res.Command != "terminal.toggle" {
		t.Fatalf("custom leader should drive the same mnemonics, got %+v", res)
	}
	// An empty leader falls back to the default.
	rows := LeaderRows("")
	found := false
	for _, b := range rows {
		if b.Chord.String() == DefaultLeader+" f" {
			found = true
		}
	}
	if !found {
		t.Fatal("empty leader should fall back to space")
	}
}

// TestNoSameContextConflictsAfterRePick guards the issue's conflict clause:
// the combined table (re-picked fragile flags + both leader layers) builds
// without same-chord/same-context clashes on either platform.
func TestNoSameContextConflictsAfterRePick(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		rows := append(Defaults(PresetJetBrains), LeaderRows(DefaultLeader)...)
		table := BuildTable(rows, nil, goos)
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
// every fragile, non-blocked default command is leader-covered, has another
// delivered chord, or sits on the deliberate exception list (vim-native
// editor operations and palette-only reach).
func TestFragileDefaultsHaveReachableAlternative(t *testing.T) {
	// The shared alternatives map (matrix.go) is the single source of truth
	// for the documented escapes.
	exceptions := reachableAlternatives
	leader := LeaderCommands()
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
		if leader[b.Command] || delivered[b.Command] {
			continue
		}
		if _, ok := exceptions[b.Command]; !ok {
			t.Errorf("%s (%s) is fragile with no leader/delivered alternative and no documented exception", b.Command, b.Chord)
		}
	}
}
