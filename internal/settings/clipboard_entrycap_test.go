package settings

// clipboard_entrycap_test.go covers the Settings-UI half of the per-entry
// clipboard-history cap (#2250): editor.clipboard_history_max_kb is a real,
// bounded entry on the Editor page that validates, persists and renders.

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// clipCapEntry returns the schema entry for editor.clipboard_history_max_kb.
func clipCapEntry(t *testing.T) Entry {
	t.Helper()
	for _, p := range BasePages(nil, nil, nil) {
		for _, e := range p.Entries {
			if e.Key == "editor.clipboard_history_max_kb" {
				if p.Title != "Editor" {
					t.Fatalf("entry lives on page %q, want Editor", p.Title)
				}
				return e
			}
		}
	}
	t.Fatal("editor.clipboard_history_max_kb must be configurable in the settings UI")
	return Entry{}
}

func TestClipboardHistoryMaxKBEntryBounded(t *testing.T) {
	e := clipCapEntry(t)
	if e.Type != Int {
		t.Fatalf("type = %v, want Int", e.Type)
	}
	if e.Min != 1 || e.Max != 10240 {
		t.Fatalf("range = %d–%d, want 1–10240", e.Min, e.Max)
	}
	if e.Scope != config.UserScope || e.Title == "" || e.Description == "" {
		t.Fatalf("entry needs a user-scoped title and description: %#v", e)
	}
}

func TestClipboardHistoryMaxKBValidatesAndPersists(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "Editor", Entries: []Entry{clipCapEntry(t)}}}, testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter"))
	ed, ok := m.editor.(*intEditor)
	if !ok {
		t.Fatalf("editor = %T, want *intEditor", m.editor)
	}
	// Non-numeric input is rejected inline, without a write.
	ed.tf.Set("huge")
	if cmd := m.Update(key("enter")); cmd != nil || ed.err == "" {
		t.Fatalf("invalid cap must not write (err=%q)", ed.err)
	}
	// Out of range clamps to the maximum…
	ed.tf.Set("999999")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Editor.ClipboardHistoryMaxKB; got != 10240 {
		t.Fatalf("clipboard_history_max_kb = %d, want clamped 10240", got)
	}

	// …and a sane value persists and shows up in the list rendering.
	m.Update(key("enter"))
	m.editor.(*intEditor).tf.Set("64")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Editor.ClipboardHistoryMaxKB; got != 64 {
		t.Fatalf("clipboard_history_max_kb = %d, want 64", got)
	}
	if view := m.View(); !strings.Contains(view, "64") {
		t.Fatalf("the entry list must show the value:\n%s", view)
	}
}
