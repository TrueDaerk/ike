package keymap

import (
	"strings"
	"testing"
)

func liveTable(t *testing.T) *LiveBindings {
	t.Helper()
	l := &LiveBindings{}
	l.Set(BuildTable(DefaultsFor(PresetJetBrains, "darwin"), nil, "darwin"))
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
	table := BuildTable(DefaultsFor(PresetJetBrains, "darwin"), nil, "darwin")
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

// whichKeyTable is a hand-built table exercising the which-key data (#1909):
// a three-step sequence for narrowing plus per-context continuations of the
// same prefix.
func whichKeyTable(t *testing.T) *BindingTable {
	t.Helper()
	bindings := []Binding{
		{Chord: MustParseChord("cmd+k g"), Command: "wk.group", Title: "Group", Context: Global},
		{Chord: MustParseChord("cmd+k g s"), Command: "wk.groupSave", Title: "Group save", Context: Global},
		{Chord: MustParseChord("cmd+k g b"), Command: "wk.groupBackup", Title: "Group backup", Context: Global},
		{Chord: MustParseChord("cmd+k e"), Command: "wk.editorOnly", Title: "Editor only", Context: Editor},
		{Chord: MustParseChord("cmd+k x"), Command: "wk.explorerOnly", Title: "Explorer only", Context: Explorer},
	}
	return BuildTable(bindings, nil, "darwin")
}

func contKeys(conts []Continuation) []string {
	out := make([]string, len(conts))
	for i, c := range conts {
		out[i] = c.Key
	}
	return out
}

func TestContinuationsFilterByContext(t *testing.T) {
	table := whichKeyTable(t)
	got := strings.Join(contKeys(table.Continuations(MustParseChord("cmd+k"), Editor)), ",")
	if !strings.Contains(got, "e") || strings.Contains(got, "x") {
		t.Fatalf("editor context should offer e and not x, got %q", got)
	}
	got = strings.Join(contKeys(table.Continuations(MustParseChord("cmd+k"), Explorer)), ",")
	if !strings.Contains(got, "x") || strings.Contains(got, "e") {
		t.Fatalf("explorer context should offer x and not e, got %q", got)
	}
	// The Global sequence stays offered in every context.
	for _, ctx := range []Context{Global, Editor, Explorer} {
		if !strings.Contains(strings.Join(contKeys(table.Continuations(MustParseChord("cmd+k"), ctx)), ","), "g") {
			t.Fatalf("global continuation g missing in context %v", ctx)
		}
	}
}

func TestContinuationsNarrowWithThePrefix(t *testing.T) {
	table := whichKeyTable(t)
	// One step deeper the popup lists the third step only, and each row names
	// the command that completing it runs.
	conts := table.Continuations(MustParseChord("cmd+k g"), Global)
	if got := strings.Join(contKeys(conts), ","); got != "b,s" {
		t.Fatalf("cmd+k g continuations = %q", got)
	}
	byKey := map[string]Continuation{}
	for _, c := range conts {
		byKey[c.Key] = c
	}
	if byKey["s"].Command != "wk.groupSave" || byKey["s"].Title != "Group save" {
		t.Fatalf("s continuation = %+v", byKey["s"])
	}
	// The complete chord offers nothing more.
	if got := table.Continuations(MustParseChord("cmd+k g s"), Global); len(got) != 0 {
		t.Fatalf("a complete chord offers nothing, got %v", got)
	}
	// An empty prefix is not a pending sequence.
	if got := table.Continuations(Chord{}, Global); got != nil {
		t.Fatalf("empty prefix = %v", got)
	}
}

func TestContinuationsDeduplicateByKey(t *testing.T) {
	// Two commands behind the same next step collapse into one row: the popup
	// names the key once.
	table := BuildTable([]Binding{
		{Chord: MustParseChord("cmd+k g s"), Command: "wk.a", Title: "A", Context: Global},
		{Chord: MustParseChord("cmd+k g s t"), Command: "wk.b", Title: "B", Context: Global},
	}, nil, "darwin")
	if got := contKeys(table.Continuations(MustParseChord("cmd+k g"), Global)); len(got) != 1 || got[0] != "s" {
		t.Fatalf("continuations = %v", got)
	}
}

func TestResolverContinues(t *testing.T) {
	r := NewResolver(whichKeyTable(t))
	if r.Continues(key(t, "esc"), Global) {
		t.Fatal("an idle resolver continues nothing")
	}
	r.Feed(key(t, "cmd+k"), Global)
	if !r.Continues(key(t, "g"), Global) {
		t.Fatal("g extends cmd+k toward the three-step chord")
	}
	if r.Continues(key(t, "esc"), Global) {
		t.Fatal("esc matches no continuation — it cancels the sequence")
	}
	// Context-scoped continuations are only offered in their context.
	if r.Continues(key(t, "x"), Editor) {
		t.Fatal("the explorer-scoped step must not continue in the editor")
	}
	if !r.Continues(key(t, "x"), Explorer) {
		t.Fatal("the explorer-scoped step continues in the explorer")
	}
	// Continues never mutates the held sequence.
	if prefix, _ := r.PendingContinuations(Global); prefix != "cmd+k" {
		t.Fatalf("pending prefix changed to %q", prefix)
	}
}

func TestResolverPendingContinuations(t *testing.T) {
	r := NewResolver(BuildTable(DefaultsFor(PresetJetBrains, "darwin"), nil, "darwin"))
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
