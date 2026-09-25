package settings

// html_budget_test.go covers the Settings-UI half of the HTML preview's render
// budget (#2745): preview.html_render_budget_kb is a bounded Int on the
// Markdown Preview page that refuses nonsense, persists and renders.

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// htmlBudgetEntry returns the shipped preview.html_render_budget_kb entry.
func htmlBudgetEntry(t *testing.T) Entry {
	t.Helper()
	for _, p := range BasePages(nil, nil, nil) {
		for _, e := range p.Entries {
			if e.Key == "preview.html_render_budget_kb" {
				if p.Title != "Markdown Preview" {
					t.Fatalf("entry lives on page %q, want Markdown Preview", p.Title)
				}
				return e
			}
		}
	}
	t.Fatal("preview.html_render_budget_kb must be configurable in the settings UI")
	return Entry{}
}

func TestHTMLRenderBudgetEntry(t *testing.T) {
	e := htmlBudgetEntry(t)
	if e.Type != Int || e.Scope != config.UserScope || e.Title != "HTML preview render budget (KB)" || e.Description == "" {
		t.Fatalf("entry = %#v, want a titled, described user-scoped Int", e)
	}
	if e.Min != config.HTMLRenderBudgetKBMin || e.Max != config.HTMLRenderBudgetKBMax {
		t.Fatalf("range = %d–%d, want %d–%d", e.Min, e.Max, config.HTMLRenderBudgetKBMin, config.HTMLRenderBudgetKBMax)
	}
	if config.Defaults()["preview.html_render_budget_kb"] != "2048" {
		t.Fatalf("the shipped default must be 2048, got %q", config.Defaults()["preview.html_render_budget_kb"])
	}
}

func TestHTMLRenderBudgetValidatesAndPersists(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "Markdown Preview", Entries: []Entry{htmlBudgetEntry(t)}}}, testOpts(t))
	m.SetSize(100, 20)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter"))
	ed, ok := m.editor.(*intEditor)
	if !ok {
		t.Fatalf("editor = %T, want *intEditor", m.editor)
	}
	// Non-numeric input is rejected inline, without a write.
	ed.tf.Set("lots")
	if cmd := m.Update(key("enter")); cmd != nil || ed.err == "" {
		t.Fatalf("invalid budget must not write (err=%q)", ed.err)
	}
	// A sane value persists and shows up in the list rendering.
	ed.tf.Set("512")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Preview.HTMLRenderBudgetKB; got != 512 {
		t.Fatalf("html_render_budget_kb = %d, want 512", got)
	}
	if view := m.View(); !strings.Contains(view, "512") {
		t.Fatalf("the entry list must show the value:\n%s", view)
	}
	// Below the floor clamps to it, with a notice, instead of persisting a
	// budget that would show next to nothing.
	m.Update(key("enter"))
	m.editor.(*intEditor).tf.Set("1")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Preview.HTMLRenderBudgetKB; got != config.HTMLRenderBudgetKBMin {
		t.Fatalf("html_render_budget_kb = %d, want the floor %d", got, config.HTMLRenderBudgetKBMin)
	}
}
