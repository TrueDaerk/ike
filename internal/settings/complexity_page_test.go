package settings

// complexity_page_test.go covers the Settings-UI half of the project
// complexity index (#2916): the "Complexity" page carries the [complexity]
// keys, the exclude list rejects an invalid glob in the form, every edit
// persists and shows in the list.

import (
	"strings"
	"testing"

	"ike/internal/config"
)

func complexityPage(t *testing.T) Page {
	t.Helper()
	for _, p := range BasePages(nil, nil, nil) {
		if p.Title == "Complexity" {
			return p
		}
	}
	t.Fatal("no Complexity settings page")
	return Page{}
}

func complexityEntry(t *testing.T, key string) Entry {
	t.Helper()
	for _, e := range complexityPage(t).Entries {
		if e.Key == key {
			return e
		}
	}
	t.Fatalf("%s must be configurable on the Complexity page", key)
	return Entry{}
}

func TestComplexityPageEntries(t *testing.T) {
	p := complexityPage(t)
	if p.Description == "" {
		t.Fatal("the Complexity page needs a description")
	}
	want := []string{"complexity.enabled", "complexity.exclude", "complexity.ccn_warn", "complexity.cognitive_warn", "complexity.problems"}
	if len(p.Entries) != len(want) {
		t.Fatalf("Complexity page has %d entries, want %d: %+v", len(p.Entries), len(want), p.Entries)
	}
	for i, key := range want {
		e := p.Entries[i]
		if e.Key != key || e.Title == "" || e.Description == "" || e.Scope != config.UserScope {
			t.Errorf("entry %d = %+v, want user-scoped %s with title and description", i, e, key)
		}
	}
	for _, key := range []string{"complexity.enabled", "complexity.problems"} {
		if e := complexityEntry(t, key); e.Type != Bool {
			t.Errorf("%s type = %v, want Bool", key, e.Type)
		}
	}
	if e := complexityEntry(t, "complexity.exclude"); e.Type != List || e.ValidateEntry == nil {
		t.Errorf("exclude = %+v, want a validated List", e)
	}
	for _, key := range []string{"complexity.ccn_warn", "complexity.cognitive_warn"} {
		if e := complexityEntry(t, key); e.Type != Int || e.Min != 1 || e.Max != 200 {
			t.Errorf("%s = %+v, want an Int bounded 1..200", key, e)
		}
	}
}

// TestComplexityExcludeRejectsInvalidGlob: a malformed pattern is refused in
// place with a message and nothing is staged; valid globs commit and persist
// as the typed list the index reads.
func TestComplexityExcludeRejectsInvalidGlob(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "Complexity", Entries: []Entry{complexityEntry(t, "complexity.exclude")}}}, testOpts(t))
	m.SetSize(90, 24)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter"))
	ed, ok := m.editor.(*listEditor)
	if !ok {
		t.Fatalf("editor = %T, want *listEditor", m.editor)
	}
	ed.idx = len(ed.items)
	m.Update(key("enter"))
	ed.tf.Set("src/[gen")
	m.Update(key("enter"))
	if !ed.editing || ed.err != "invalid glob pattern" {
		t.Fatalf("an invalid glob must stay in edit mode with a message (editing=%v err=%q)", ed.editing, ed.err)
	}
	if len(ed.items) != 0 || m.Dirty() {
		t.Fatalf("nothing may be staged: items=%v dirty=%v", ed.items, m.Dirty())
	}
	for _, g := range []string{"vendor/**", "**/*_test.go"} {
		ed.tf.Set(g)
		m.Update(key("enter"))
		if ed.err != "" {
			t.Fatalf("%q: err = %q, want accepted", g, ed.err)
		}
		ed.idx = len(ed.items)
		m.Update(key("enter"))
	}
	m.Update(key("esc"))
	commit(t, m)
	got := config.Get().Complexity.Exclude
	if len(got) != 2 || got[0] != "vendor/**" || got[1] != "**/*_test.go" {
		t.Fatalf("exclude = %v, want [vendor/** **/*_test.go]", got)
	}
	if view := m.View(); !strings.Contains(view, "vendor/**") {
		t.Fatalf("the entry list must show the value:\n%s", view)
	}
}

// TestComplexityEnabledToggles: the master switch flips on enter and
// persists.
func TestComplexityEnabledToggles(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "Complexity", Entries: complexityPage(t).Entries}}, testOpts(t))
	m.SetSize(100, 24)
	m.Open()
	m.Update(key("tab"))
	before := config.Get().Complexity.Enabled
	if !before {
		t.Fatal("complexity.enabled must default to on")
	}
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Complexity.Enabled; got == before {
		t.Fatalf("enabled did not toggle from %v", before)
	}
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Complexity.Enabled; got != before {
		t.Fatalf("enabled did not toggle back to %v", before)
	}
}

// TestComplexityProblemsToggles: the Problems feed switch (#2920) defaults
// on, shows in the list, flips on enter and persists.
func TestComplexityProblemsToggles(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "Complexity", Entries: []Entry{complexityEntry(t, "complexity.problems")}}}, testOpts(t))
	m.SetSize(100, 24)
	m.Open()
	m.Update(key("tab"))
	if !config.Get().Complexity.Problems {
		t.Fatal("complexity.problems must default to on")
	}
	if view := m.View(); !strings.Contains(view, "Problems feed") {
		t.Fatalf("the entry list must show the setting:\n%s", view)
	}
	m.Update(key("enter"))
	commit(t, m)
	if config.Get().Complexity.Problems {
		t.Fatal("problems did not toggle off")
	}
	m.Update(key("enter"))
	commit(t, m)
	if !config.Get().Complexity.Problems {
		t.Fatal("problems did not toggle back on")
	}
}
