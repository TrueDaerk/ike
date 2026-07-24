package keymap

import (
	"strings"
	"testing"
)

func liveTable(t *testing.T) *LiveBindings {
	t.Helper()
	l := &LiveBindings{}
	l.Set(BuildTable(Defaults(PresetJetBrains), nil, "darwin"))
	return l
}

func TestLiveBindingsHonestLabels(t *testing.T) {
	l := liveTable(t)

	// Delivered primary wins the label outright.
	if got, ok := l.Binding("editor.write"); !ok || got != "ctrl+s" {
		t.Fatalf("editor.write = %q ok=%v", got, ok)
	}
	// A delivered default primary wins over the fragile JetBrains chord
	// (0082 sheet 11, #18: f4 outranks cmd+b).
	if got, _ := l.Binding("lsp.definition"); got != "f4" {
		t.Fatalf("lsp.definition = %q", got)
	}
	// Fragile-only commands show their chord unadorned: the per-binding
	// "⚠ terminal-dependent" suffix is gone (#720) — a deficient terminal
	// raises one startup notification instead.
	if got, _ := l.Binding("lsp.references"); got != "alt+f7" {
		t.Fatalf("lsp.references = %q", got)
	}
	if got, _ := l.Binding("editor.duplicateLine"); got != "cmd+d" {
		t.Fatalf("editor.duplicateLine = %q", got)
	}
	// Blocked commands are labelled, never hidden. The real ledger emptied
	// with 0320 (#466), so the machinery is exercised through a stub entry.
	remove := StubBlockedForTest("vcs.revertFile", "unit-test dependency")
	if got, _ := l.Binding("vcs.revertFile"); !strings.HasPrefix(got, "✗ blocked:") {
		t.Fatalf("stubbed blocked binding = %q", got)
	}
	remove()
	// Without the stub the fragile cmd+alt+z chord is shown plain.
	if got, _ := l.Binding("vcs.revertFile"); got != "cmd+alt+z" {
		t.Fatalf("vcs.revertFile = %q", got)
	}
	// Unbound ids degrade gracefully.
	if _, ok := l.Binding("no.such.command"); ok {
		t.Fatal("unknown id should report no binding")
	}
}

func TestLiveBindingsFollowReloads(t *testing.T) {
	l := liveTable(t)
	before, _ := l.Binding("project.goToFile")
	l.Set(BuildTable(Defaults(PresetJetBrains), map[string]string{"f9": "project.goToFile"}, "darwin"))
	after, _ := l.Binding("project.goToFile")
	if before == after || after != "f9" {
		t.Fatalf("reload should re-resolve: before=%q after=%q", before, after)
	}
}

func TestContinuationsForHeldPrefix(t *testing.T) {
	table := BuildTable(Defaults(PresetJetBrains), nil, "darwin")
	conts := table.Continuations(MustParseChord("cmd+k"), Global)
	if len(conts) == 0 {
		t.Fatal("cmd+k prefix should offer continuations")
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
	if byKey["z"].Command != "pane.maximize" || byKey["z"].Title == "" {
		t.Fatalf("z continuation = %+v", byKey["z"])
	}
	if byKey["down"].Command != "pane.splitDown" {
		t.Fatalf("down continuation = %+v", byKey["down"])
	}
	// A non-prefix chord offers nothing.
	if got := table.Continuations(MustParseChord("f6"), Global); len(got) != 0 {
		t.Fatalf("f6 is complete, got %v", got)
	}
}

func TestResolverPendingContinuations(t *testing.T) {
	r := NewResolver(BuildTable(Defaults(PresetJetBrains), nil, "darwin"))
	if prefix, conts := r.PendingContinuations(Global); prefix != "" || conts != nil {
		t.Fatal("idle resolver offers nothing")
	}
	r.Feed(key(t, "cmd+k"), Global)
	prefix, conts := r.PendingContinuations(Global)
	if prefix != "cmd+k" || len(conts) == 0 {
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
