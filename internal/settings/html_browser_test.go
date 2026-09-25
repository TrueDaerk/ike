package settings

// html_browser_test.go covers the Settings-UI half of the HTML preview's
// browser screenshot mode (#2746): preview.html_browser is a Path entry that
// refuses a missing file and a directory, preview.html_browser_timeout_s a
// bounded Int; both persist and render in the list.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/config"
)

// markdownPreviewEntry returns the shipped entry for key, asserting it lives
// on the Markdown Preview page.
func markdownPreviewEntry(t *testing.T, key string) Entry {
	t.Helper()
	for _, p := range BasePages(nil, nil, nil) {
		for _, e := range p.Entries {
			if e.Key == key {
				if p.Title != "Markdown Preview" {
					t.Fatalf("%s lives on page %q, want Markdown Preview", key, p.Title)
				}
				return e
			}
		}
	}
	t.Fatalf("%s must be configurable in the settings UI", key)
	return Entry{}
}

func TestHTMLBrowserEntries(t *testing.T) {
	bin := markdownPreviewEntry(t, "preview.html_browser")
	if bin.Type != Path || bin.Dirs || bin.Scope != config.UserScope || bin.Description == "" || bin.ValidateString == nil {
		t.Fatalf("browser entry = %#v, want a described, validated user-scoped file Path", bin)
	}
	to := markdownPreviewEntry(t, "preview.html_browser_timeout_s")
	if to.Type != Int || to.Scope != config.UserScope || to.Min != config.HTMLBrowserTimeoutSMin || to.Max != config.HTMLBrowserTimeoutSMax {
		t.Fatalf("timeout entry = %#v, want a user-scoped Int bounded %d–%d", to, config.HTMLBrowserTimeoutSMin, config.HTMLBrowserTimeoutSMax)
	}
	if d := config.Defaults(); d["preview.html_browser_timeout_s"] != "20" || d["preview.html_browser"] != "" {
		t.Fatalf("shipped defaults = %q / %q, want 20 / empty", d["preview.html_browser_timeout_s"], d["preview.html_browser"])
	}
}

func TestHTMLBrowserPathValidatesAndPersists(t *testing.T) {
	restoreConfig(t)
	dir := t.TempDir()
	exe := filepath.Join(dir, "chrome")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := New([]Page{{Title: "Markdown Preview", Entries: []Entry{markdownPreviewEntry(t, "preview.html_browser")}}}, testOpts(t))
	m.SetSize(120, 20)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter"))
	ed, ok := m.editor.(*pathEditor)
	if !ok {
		t.Fatalf("editor = %T, want *pathEditor", m.editor)
	}
	// A missing file and a directory (an .app bundle) are refused inline.
	ed.tf.Set(filepath.Join(dir, "missing"))
	if cmd := m.Update(key("enter")); cmd != nil || ed.err == "" || m.Dirty() {
		t.Fatalf("a missing browser must not write (err=%q)", ed.err)
	}
	ed.tf.Set(dir)
	if cmd := m.Update(key("enter")); cmd != nil || !strings.Contains(ed.err, "directory") || m.Dirty() {
		t.Fatalf("a directory must be refused with the fix (err=%q)", ed.err)
	}
	// An executable persists and shows in the list rendering.
	ed.tf.Set(exe)
	m.Update(key("enter"))
	if ed.err != "" {
		t.Fatalf("a real binary must commit, err=%q", ed.err)
	}
	commit(t, m)
	if got := config.Get().Preview.HTMLBrowser; got != exe {
		t.Fatalf("html_browser = %q, want %q", got, exe)
	}
	if view := m.View(); !strings.Contains(view, "chrome") {
		t.Fatalf("the entry list must show the value:\n%s", view)
	}
}

func TestHTMLBrowserTimeoutValidatesAndPersists(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "Markdown Preview", Entries: []Entry{markdownPreviewEntry(t, "preview.html_browser_timeout_s")}}}, testOpts(t))
	m.SetSize(100, 20)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter"))
	ed, ok := m.editor.(*intEditor)
	if !ok {
		t.Fatalf("editor = %T, want *intEditor", m.editor)
	}
	ed.tf.Set("soon")
	if cmd := m.Update(key("enter")); cmd != nil || ed.err == "" {
		t.Fatalf("a non-number must not write (err=%q)", ed.err)
	}
	ed.tf.Set("45")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Preview.HTMLBrowserTimeoutS; got != 45 {
		t.Fatalf("html_browser_timeout_s = %d, want 45", got)
	}
	if view := m.View(); !strings.Contains(view, "45") {
		t.Fatalf("the entry list must show the value:\n%s", view)
	}
	// Past the ceiling clamps to it rather than letting a hung browser run.
	m.Update(key("enter"))
	m.editor.(*intEditor).tf.Set("9999")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Preview.HTMLBrowserTimeoutS; got != config.HTMLBrowserTimeoutSMax {
		t.Fatalf("html_browser_timeout_s = %d, want the ceiling %d", got, config.HTMLBrowserTimeoutSMax)
	}
}
