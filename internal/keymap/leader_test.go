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

// TestUnboundCommandDefaults: the previously palette-only commands picked up
// defaults in #242 — f3/shift+f3 step retained search matches, alt+f1 reveals
// the open file, leader T opens a terminal, leader h the notification history.
func TestUnboundCommandDefaults(t *testing.T) {
	cases := []struct {
		keys []string
		cmd  string
	}{
		{[]string{"f3"}, "search.nextMatch"},
		{[]string{"shift+f3"}, "search.prevMatch"},
		{[]string{"alt+f1"}, "explorer.reveal"},
		{[]string{"space", "shift+t"}, "terminal.new"},
		{[]string{"ctrl+k", "shift+t"}, "terminal.new"},
		{[]string{"space", "h"}, "notifications.history"},
		{[]string{"ctrl+k", "h"}, "notifications.history"},
		// Delivered tab-cycling primaries (#248): the alt chords never arrive
		// on macOS (Option composes characters), ctrl+page keys always do.
		{[]string{"ctrl+pgdown"}, "editor.tab.next"},
		{[]string{"ctrl+pgup"}, "editor.tab.prev"},
		{[]string{"ctrl+shift+pgdown"}, "editor.tab.moveRight"},
		{[]string{"ctrl+shift+pgup"}, "editor.tab.moveLeft"},
	}
	for _, c := range cases {
		r := NewResolver(leaderTable(t, "space"))
		for i, k := range c.keys {
			res := r.Feed(key(t, k), Global)
			if i < len(c.keys)-1 {
				continue
			}
			if res.Status != Resolved || res.Command != c.cmd {
				t.Errorf("%v: got %+v, want %s", c.keys, res, c.cmd)
			}
		}
	}
}

// TestDoubleSpaceOpensSearchEverywhere: space space is the terminal stand-in
// for JetBrains' double-shift (0082 sheet 17, #263), riding the leader engine.
func TestDoubleSpaceOpensSearchEverywhere(t *testing.T) {
	prev := GOOS
	GOOS = "linux"
	defer func() { GOOS = prev }()

	rows := append(Defaults(PresetJetBrains), LeaderRows("")...)
	table := BuildTable(rows, nil, "linux")
	r := NewResolver(table)
	space := MustParseChord("space space").Steps[0]

	if res := r.Feed(space, Explorer); res.Status != Pending {
		t.Fatalf("first space = %+v, want Pending", res)
	}
	res := r.Feed(space, Explorer)
	if res.Status != Resolved || res.Command != "palette.searchEverywhere" {
		t.Fatalf("second space = %+v, want palette.searchEverywhere", res)
	}
}
