package keymap

import (
	"strings"
	"testing"
)

func liveTable(t *testing.T, withLeader bool) *LiveBindings {
	t.Helper()
	rows := Defaults(PresetJetBrains)
	if withLeader {
		rows = append(rows, LeaderRows(DefaultLeader)...)
	}
	l := &LiveBindings{}
	l.Set(BuildTable(rows, nil, "darwin"))
	return l
}

func TestLiveBindingsHonestLabels(t *testing.T) {
	l := liveTable(t, true)

	// Delivered primary wins the label outright.
	if got, ok := l.Binding("editor.write"); !ok || got != "ctrl+s" {
		t.Fatalf("editor.write = %q ok=%v", got, ok)
	}
	// A delivered default primary wins even when a leader mnemonic exists
	// (0082 sheet 11, #18: f4 outranks both cmd+b and space d).
	if got, _ := l.Binding("lsp.definition"); got != "f4" {
		t.Fatalf("lsp.definition = %q", got)
	}
	// A leader row is a delivered chord, so covered fragile commands show it.
	if got, _ := l.Binding("lsp.references"); got != "space u" {
		t.Fatalf("lsp.references = %q", got)
	}
	// Fragile-only with no alternative: honest warning.
	if got, _ := l.Binding("editor.duplicateLine"); !strings.Contains(got, "cmd+d ⚠") {
		t.Fatalf("editor.duplicateLine = %q", got)
	}
	// Blocked commands are labelled, never hidden. The real ledger emptied
	// with 0320 (#466), so the machinery is exercised through a stub entry.
	remove := StubBlockedForTest("vcs.commit", "unit-test dependency")
	if got, _ := l.Binding("vcs.commit"); !strings.HasPrefix(got, "✗ blocked:") {
		t.Fatalf("stubbed blocked binding = %q", got)
	}
	remove()
	// Without the stub the VCS ids resolve to their leader mnemonics (0320).
	if got, _ := l.Binding("vcs.commit"); got != "space v c" {
		t.Fatalf("vcs.commit = %q", got)
	}
	// Unbound ids degrade gracefully.
	if _, ok := l.Binding("no.such.command"); ok {
		t.Fatal("unknown id should report no binding")
	}
}

func TestLiveBindingsFragileUseAlternative(t *testing.T) {
	// Without the leader layer in the table, the label points at the leader
	// path as the escape route.
	l := liveTable(t, false)
	got, _ := l.Binding("lsp.references")
	if !strings.Contains(got, "alt+f7 ⚠ use space u") {
		t.Fatalf("lsp.references = %q", got)
	}
}

func TestLiveBindingsFollowReloads(t *testing.T) {
	l := liveTable(t, true)
	before, _ := l.Binding("project.goToFile")
	l.Set(BuildTable(Defaults(PresetJetBrains), map[string]string{"f9": "project.goToFile"}, "darwin"))
	after, _ := l.Binding("project.goToFile")
	if before == after || after != "f9" {
		t.Fatalf("reload should re-resolve: before=%q after=%q", before, after)
	}
}

func TestContinuationsForHeldPrefix(t *testing.T) {
	rows := append(Defaults(PresetJetBrains), LeaderRows(DefaultLeader)...)
	table := BuildTable(rows, nil, "darwin")
	conts := table.Continuations(MustParseChord(DefaultLeader), Global)
	if len(conts) == 0 {
		t.Fatal("leader prefix should offer continuations")
	}
	byKey := map[string]Continuation{}
	for i, c := range conts {
		byKey[c.Key] = c
		if i > 0 {
			prev := conts[i-1]
			if keyRank(prev.Key) > keyRank(c.Key) {
				t.Fatalf("letters must sort before digits/punctuation at %d: %v", i, conts)
			}
		}
	}
	if byKey["f"].Command != "project.goToFile" || byKey["f"].Title == "" {
		t.Fatalf("f continuation = %+v", byKey["f"])
	}
	// Editor context sees the universal ctrl+k continuations too.
	if got := table.Continuations(MustParseChord("ctrl+k"), Editor); len(got) == 0 {
		t.Fatal("ctrl+k should offer continuations in the editor")
	}
	// A non-prefix chord offers nothing.
	if got := table.Continuations(MustParseChord("f6"), Global); len(got) != 0 {
		t.Fatalf("f6 is complete, got %v", got)
	}
}

func TestResolverPendingContinuations(t *testing.T) {
	rows := append(Defaults(PresetJetBrains), LeaderRows(DefaultLeader)...)
	r := NewResolver(BuildTable(rows, nil, "darwin"))
	if prefix, conts := r.PendingContinuations(Global); prefix != "" || conts != nil {
		t.Fatal("idle resolver offers nothing")
	}
	r.Feed(key(t, "space"), Global)
	prefix, conts := r.PendingContinuations(Global)
	if prefix != "space" || len(conts) == 0 {
		t.Fatalf("pending = %q %v", prefix, conts)
	}
}

func TestFormatContinuationsCaps(t *testing.T) {
	conts := []Continuation{{Key: "a", Title: "A"}, {Key: "b", Title: "B"}, {Key: "c", Title: "C"}}
	rows := FormatContinuations(conts, 2)
	if len(rows) != 3 || rows[0] != "a  A" || rows[2] != "…" {
		t.Fatalf("rows = %v", rows)
	}
}
