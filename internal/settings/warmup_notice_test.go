package settings

// warmup_notice_test.go covers the Settings-UI half of the silent-server
// notice (#2629): lsp.warmup_notice_ms is a real, bounded entry on the
// Language Support page that validates, persists and renders.

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// warmupNoticeEntry returns the schema entry for lsp.warmup_notice_ms.
func warmupNoticeEntry(t *testing.T) Entry {
	t.Helper()
	for _, p := range BasePages(nil, nil, nil) {
		for _, e := range p.Entries {
			if e.Key == "lsp.warmup_notice_ms" {
				if p.Title != "Language Support" {
					t.Fatalf("entry lives on page %q, want Language Support", p.Title)
				}
				return e
			}
		}
	}
	t.Fatal("lsp.warmup_notice_ms must be configurable in the settings UI")
	return Entry{}
}

func TestWarmupNoticeEntryBounded(t *testing.T) {
	e := warmupNoticeEntry(t)
	if e.Type != Int {
		t.Fatalf("type = %v, want Int", e.Type)
	}
	if e.Min != 0 || e.Max != 600000 {
		t.Fatalf("range = %d–%d, want 0–600000", e.Min, e.Max)
	}
	if e.Scope != config.UserScope || e.Title == "" || e.Description == "" {
		t.Fatalf("entry needs a user-scoped title and description: %#v", e)
	}
	if !strings.Contains(e.Description, "0 turns") {
		t.Errorf("the description must document the off value: %q", e.Description)
	}
}

func TestWarmupNoticeValidatesAndPersists(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "Language Support", Entries: []Entry{warmupNoticeEntry(t)}}}, testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter"))
	ed, ok := m.editor.(*intEditor)
	if !ok {
		t.Fatalf("editor = %T, want *intEditor", m.editor)
	}
	// Non-numeric input is rejected inline, without a write.
	ed.tf.Set("later")
	if cmd := m.Update(key("enter")); cmd != nil || ed.err == "" {
		t.Fatalf("invalid threshold must not write (err=%q)", ed.err)
	}
	// A negative threshold is out of range and clamps to the 0 = off end.
	ed.tf.Set("-1")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().LSP.WarmupNoticeMs; got != 0 {
		t.Fatalf("warmup_notice_ms = %d, want clamped 0", got)
	}
	// Past the upper bound it clamps the other way.
	m.Update(key("enter"))
	m.editor.(*intEditor).tf.Set("9999999")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().LSP.WarmupNoticeMs; got != 600000 {
		t.Fatalf("warmup_notice_ms = %d, want clamped 600000", got)
	}

	// A sane value persists and shows up in the list rendering.
	m.Update(key("enter"))
	m.editor.(*intEditor).tf.Set("8000")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().LSP.WarmupNoticeMs; got != 8000 {
		t.Fatalf("warmup_notice_ms = %d, want 8000", got)
	}
	if view := m.View(); !strings.Contains(view, "8000") {
		t.Fatalf("the entry list must show the value:\n%s", view)
	}
}
