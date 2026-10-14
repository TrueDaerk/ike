package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// TestComplexityOverridesForm: the threshold overrides list (#2960) refuses
// a malformed entry in place with a message naming it, commits good ones,
// persists them in order and shows them in the list.
func TestComplexityOverridesForm(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "Complexity", Entries: []Entry{complexityEntry(t, "complexity.overrides")}}}, testOpts(t))
	m.SetSize(120, 24)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter"))
	ed, ok := m.editor.(*listEditor)
	if !ok {
		t.Fatalf("editor = %T, want *listEditor", m.editor)
	}
	for _, bad := range []string{"ccn=0", "foo", "lang: cog=3", "legacy/** x=1", "legacy/** ccn=0"} {
		ed.idx = len(ed.items)
		m.Update(key("enter"))
		ed.tf.Set(bad)
		m.Update(key("enter"))
		if !ed.editing || !strings.Contains(ed.err, bad) {
			t.Fatalf("%q must stay in edit mode with a message naming it (editing=%v err=%q)", bad, ed.editing, ed.err)
		}
		ed.tf.Set("")
		m.Update(key("enter"))
	}
	if len(ed.items) != 0 || m.Dirty() {
		t.Fatalf("nothing may be staged: items=%v dirty=%v", ed.items, m.Dirty())
	}
	good := []string{"legacy/** ccn=30", "lang:php cog=25 cmt=0", "*_test.go ccn=40 cog=40"}
	for _, g := range good {
		ed.idx = len(ed.items)
		m.Update(key("enter"))
		ed.tf.Set(g)
		m.Update(key("enter"))
		if ed.err != "" {
			t.Fatalf("%q: err = %q, want accepted", g, ed.err)
		}
	}
	m.Update(key("esc"))
	commit(t, m)
	got := config.Get().Complexity.Overrides
	if strings.Join(got, "|") != strings.Join(good, "|") {
		t.Fatalf("overrides = %q, want %q", got, good)
	}
	if view := m.View(); !strings.Contains(view, "Threshold overrides") || !strings.Contains(view, "legacy/** ccn=30") {
		t.Fatalf("the entry list must show the setting and its value:\n%s", view)
	}
}

// TestComplexityOverridesSingleEntry: a lone entry holds spaces and no
// comma; it must stay one element in the list editor, the row rendering and
// a re-commit, never split into its words.
func TestComplexityOverridesSingleEntry(t *testing.T) {
	restoreConfig(t)
	c := config.Get()
	c.Complexity.Overrides = []string{"legacy/** ccn=30 cog=30"}
	config.Set(c)
	m := New([]Page{{Title: "Complexity", Entries: []Entry{complexityEntry(t, "complexity.overrides")}}}, testOpts(t))
	m.SetSize(120, 24)
	m.Open()
	m.Update(key("tab"))
	if view := m.View(); !strings.Contains(view, "legacy/** ccn=30") || strings.Contains(view, "legacy/**, ccn") {
		t.Fatalf("the row must show the entry whole, not its words:\n%s", view)
	}
	m.Update(key("enter"))
	ed := m.editor.(*listEditor)
	if len(ed.items) != 1 || ed.items[0] != "legacy/** ccn=30 cog=30" {
		t.Fatalf("editor items = %q, want the one entry", ed.items)
	}
	ed.idx = len(ed.items)
	m.Update(key("enter"))
	ed.tf.Set("lang:php cog=25")
	m.Update(key("enter"))
	m.Update(key("esc"))
	commit(t, m)
	if got := config.Get().Complexity.Overrides; strings.Join(got, "|") != "legacy/** ccn=30 cog=30|lang:php cog=25" {
		t.Fatalf("overrides = %q", got)
	}
	if got := splitList(flatten([]string{"a b"})); len(got) != 1 || got[0] != "a b" {
		t.Fatalf("a staged one-element list must round-trip: %q", got)
	}
	if got := splitList("[80 120]"); len(got) != 2 {
		t.Fatalf("the bracketed typed form still splits on spaces: %q", got)
	}
}
