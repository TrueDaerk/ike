package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// html_open_mode_test.go covers the Settings-UI half of the HTML tab's view
// modes (#2766): preview.html_open_mode is an enum entry on the Markdown
// Preview page next to the other preview.html_* settings, offering exactly the
// validated views, rendering its value in the list and persisting a pick.

func TestHTMLOpenModeEntryInSchema(t *testing.T) {
	e := markdownPreviewEntry(t, "preview.html_open_mode")
	if e.Type != Enum || e.Scope != config.UserScope || e.Title == "" || len(e.Description) < 40 {
		t.Fatalf("entry = %#v, want a described, user-scoped Enum", e)
	}
	if got := strings.Join(e.Options, ","); got != "preview,source" {
		t.Fatalf("options = %q, want preview,source", got)
	}
	if d := config.Defaults(); d["preview.html_open_mode"] != "preview" {
		t.Fatalf("shipped default = %q, want preview", d["preview.html_open_mode"])
	}
}

func TestHTMLOpenModeEditAndPersist(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "Markdown Preview", Entries: []Entry{markdownPreviewEntry(t, "preview.html_open_mode")}}}, testOpts(t))
	m.SetSize(130, 20)
	m.Open()
	if v := m.View(); !strings.Contains(v, "preview") {
		t.Fatalf("the list must render the default value:\n%s", v)
	}
	m.Update(key("tab"))
	m.Update(key("enter"))
	ed, ok := m.editor.(*enumEditor)
	if !ok {
		t.Fatalf("editor = %T, want *enumEditor", m.editor)
	}
	// No free text: only the two validated views can be picked.
	if got := ed.matches(); len(got) != 2 {
		t.Fatalf("option list = %v, want the two views", got)
	}
	m.Update(key("down"))
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Preview.HTMLOpenMode; got != config.HTMLOpenSource {
		t.Fatalf("html_open_mode = %q, want source", got)
	}
	if v := m.View(); !strings.Contains(v, "source") {
		t.Fatalf("the list must render the new value:\n%s", v)
	}
}
