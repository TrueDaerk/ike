package settings

// php_page_test.go covers the Settings-UI half of the PHP declaration
// index (#2667): the "PHP" page carries the four [php] keys, the bounded
// ones validate with a message, every edit persists and shows in the list.

import (
	"strings"
	"testing"

	"ike/internal/config"
)

func phpPage(t *testing.T) Page {
	t.Helper()
	for _, p := range BasePages(nil, nil, nil) {
		if p.Title == "PHP" {
			return p
		}
	}
	t.Fatal("no PHP settings page")
	return Page{}
}

func phpEntry(t *testing.T, key string) Entry {
	t.Helper()
	for _, e := range phpPage(t).Entries {
		if e.Key == key {
			return e
		}
	}
	t.Fatalf("%s must be configurable on the PHP page", key)
	return Entry{}
}

func TestPHPPageEntries(t *testing.T) {
	p := phpPage(t)
	if p.Description == "" {
		t.Fatal("the PHP page needs a description")
	}
	want := []string{"php.trait_index", "php.index.parent_depth", "php.index.include_vendor", "php.index.max_files"}
	if len(p.Entries) != len(want) {
		t.Fatalf("PHP page has %d entries, want %d: %+v", len(p.Entries), len(want), p.Entries)
	}
	for i, key := range want {
		e := p.Entries[i]
		if e.Key != key || e.Title == "" || e.Description == "" || e.Scope != config.UserScope {
			t.Errorf("entry %d = %+v, want user-scoped %s with title and description", i, e, key)
		}
	}
	if e := phpEntry(t, "php.trait_index"); e.Type != Bool {
		t.Errorf("trait_index type = %v, want Bool", e.Type)
	}
	if e := phpEntry(t, "php.index.include_vendor"); e.Type != Bool {
		t.Errorf("include_vendor type = %v, want Bool", e.Type)
	}
	if e := phpEntry(t, "php.index.parent_depth"); e.Type != Int || e.Min != 0 || e.Max != 10 {
		t.Errorf("parent_depth = %+v, want Int 0–10", e)
	}
	if e := phpEntry(t, "php.index.max_files"); e.Type != Int || e.Min != 100 || e.Max <= 20000 {
		t.Errorf("max_files = %+v, want Int with min 100 and a max above the default", e)
	}
}

func TestPHPParentDepthValidatesAndPersists(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "PHP", Entries: []Entry{phpEntry(t, "php.index.parent_depth")}}}, testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter"))
	ed, ok := m.editor.(*intEditor)
	if !ok {
		t.Fatalf("editor = %T, want *intEditor", m.editor)
	}
	// Non-numeric input is rejected inline, without a write.
	ed.tf.Set("deep")
	if cmd := m.Update(key("enter")); cmd != nil || ed.err == "" {
		t.Fatalf("invalid depth must not write (err=%q)", ed.err)
	}
	// Out of range clamps to the maximum with a notice…
	ed.tf.Set("42")
	m.Update(key("enter"))
	if !strings.Contains(m.notice, "clamped to 10") {
		t.Fatalf("notice = %q, want a clamp message", m.notice)
	}
	commit(t, m)
	if got := config.Get().PHP.Index.ParentDepth; got != 10 {
		t.Fatalf("parent_depth = %d, want clamped 10", got)
	}
	// …and a sane value persists and shows up in the list rendering.
	m.Update(key("enter"))
	m.editor.(*intEditor).tf.Set("2")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().PHP.Index.ParentDepth; got != 2 {
		t.Fatalf("parent_depth = %d, want 2", got)
	}
	if view := m.View(); !strings.Contains(view, "2") {
		t.Fatalf("the entry list must show the value:\n%s", view)
	}
}

func TestPHPMaxFilesFloorAndSwitches(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "PHP", Entries: phpPage(t).Entries}}, testOpts(t))
	m.SetSize(100, 24)
	m.Open()
	m.Update(key("tab"))
	// Row 0: the master switch toggles on enter.
	before := config.Get().PHP.TraitIndex
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().PHP.TraitIndex; got == before {
		t.Fatalf("trait_index did not toggle from %v", before)
	}
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().PHP.TraitIndex; got != before {
		t.Fatalf("trait_index did not toggle back to %v", before)
	}
	// Row 3: max_files below the floor clamps to 100.
	m.Update(key("down"))
	m.Update(key("down"))
	m.Update(key("down"))
	m.Update(key("enter"))
	ed, ok := m.editor.(*intEditor)
	if !ok {
		t.Fatalf("editor = %T, want *intEditor", m.editor)
	}
	ed.tf.Set("5")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().PHP.Index.MaxFiles; got != 100 {
		t.Fatalf("max_files = %d, want clamped 100", got)
	}
	if view := m.View(); !strings.Contains(view, "Trait consumer index") || !strings.Contains(view, "Index vendor/") {
		t.Fatalf("the PHP page must list its switches:\n%s", view)
	}
}
