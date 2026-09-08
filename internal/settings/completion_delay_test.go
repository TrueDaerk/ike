package settings

// completion_delay_test.go covers the Settings-UI half of the completion
// auto-trigger delay (#2541): lsp.completion_delay_ms is a real, bounded
// entry on the Language Support page that validates, persists and renders.

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// completionDelayEntry returns the schema entry for lsp.completion_delay_ms.
func completionDelayEntry(t *testing.T) Entry {
	t.Helper()
	for _, p := range BasePages(nil, nil, nil) {
		for _, e := range p.Entries {
			if e.Key == "lsp.completion_delay_ms" {
				if p.Title != "Language Support" {
					t.Fatalf("entry lives on page %q, want Language Support", p.Title)
				}
				return e
			}
		}
	}
	t.Fatal("lsp.completion_delay_ms must be configurable in the settings UI")
	return Entry{}
}

func TestCompletionDelayEntryBounded(t *testing.T) {
	e := completionDelayEntry(t)
	if e.Type != Int {
		t.Fatalf("type = %v, want Int", e.Type)
	}
	if e.Min != 0 || e.Max != 2000 {
		t.Fatalf("range = %d–%d, want 0–2000", e.Min, e.Max)
	}
	if e.Scope != config.UserScope || e.Title == "" || e.Description == "" {
		t.Fatalf("entry needs a user-scoped title and description: %#v", e)
	}
}

func TestCompletionDelayValidatesAndPersists(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "Language Support", Entries: []Entry{completionDelayEntry(t)}}}, testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter"))
	ed, ok := m.editor.(*intEditor)
	if !ok {
		t.Fatalf("editor = %T, want *intEditor", m.editor)
	}
	// Non-numeric input is rejected inline, without a write.
	ed.tf.Set("soon")
	if cmd := m.Update(key("enter")); cmd != nil || ed.err == "" {
		t.Fatalf("invalid delay must not write (err=%q)", ed.err)
	}
	// Out of range clamps to the maximum…
	ed.tf.Set("99999")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().LSP.CompletionDelayMs; got != 2000 {
		t.Fatalf("completion_delay_ms = %d, want clamped 2000", got)
	}

	// …and a sane value persists and shows up in the list rendering.
	m.Update(key("enter"))
	m.editor.(*intEditor).tf.Set("250")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().LSP.CompletionDelayMs; got != 250 {
		t.Fatalf("completion_delay_ms = %d, want 250", got)
	}
	if view := m.View(); !strings.Contains(view, "250") {
		t.Fatalf("the entry list must show the value:\n%s", view)
	}
}
