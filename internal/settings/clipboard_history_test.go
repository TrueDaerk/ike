package settings

// clipboard_history_test.go covers the Settings-UI half of the clipboard
// history (#2061): editor.clipboard_history_size is a real entry on the Editor
// page — bounded, rendered in the list with its current value, and persisted
// through the staged apply like every other schema key.

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// clipHistEntry returns the schema entry for editor.clipboard_history_size.
func clipHistEntry(t *testing.T) Entry {
	t.Helper()
	for _, p := range BasePages(nil, nil, nil) {
		for _, e := range p.Entries {
			if e.Key == "editor.clipboard_history_size" {
				if p.Title != "Editor" {
					t.Fatalf("entry lives on page %q, want Editor", p.Title)
				}
				return e
			}
		}
	}
	t.Fatal("editor.clipboard_history_size must be configurable in the settings UI")
	return Entry{}
}

func TestClipboardHistorySizeEntryBounded(t *testing.T) {
	e := clipHistEntry(t)
	if e.Type != Int {
		t.Fatalf("type = %v, want Int", e.Type)
	}
	if e.Min != 1 || e.Max != 200 {
		t.Fatalf("range = %d–%d, want 1–200", e.Min, e.Max)
	}
	if e.Scope != config.UserScope || e.Title == "" || e.Description == "" {
		t.Fatalf("entry needs a user-scoped title and description: %#v", e)
	}
}

// clipHistPages is a one-entry catalog over the real schema entry, so the form
// test exercises the shipped bounds rather than invented ones.
func clipHistPages(t *testing.T) []Page {
	return []Page{{Title: "Editor", Entries: []Entry{clipHistEntry(t)}}}
}

func TestClipboardHistorySizeValidatesAndPersists(t *testing.T) {
	restoreConfig(t)
	m := New(clipHistPages(t), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter"))
	ed, ok := m.editor.(*intEditor)
	if !ok {
		t.Fatalf("editor = %T, want *intEditor", m.editor)
	}
	// Non-numeric input is rejected inline, without a write.
	ed.tf.Set("many")
	if cmd := m.Update(key("enter")); cmd != nil || ed.err == "" {
		t.Fatalf("invalid size must not write (err=%q)", ed.err)
	}
	// Out of range clamps to the maximum…
	ed.tf.Set("9999")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Editor.ClipboardHistorySize; got != 200 {
		t.Fatalf("clipboard_history_size = %d, want clamped 200", got)
	}

	// …and a sane value persists and shows up in the list rendering.
	m.Update(key("enter"))
	m.editor.(*intEditor).tf.Set("50")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Editor.ClipboardHistorySize; got != 50 {
		t.Fatalf("clipboard_history_size = %d, want 50", got)
	}
	if view := m.View(); !strings.Contains(view, "50") {
		t.Fatalf("the entry list must show the value:\n%s", view)
	}
}
