package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// popup_scope_test.go covers the Settings UI surface of terminal.popup_scope
// (#2406): the setting must be editable in the panel — an enum offering
// exactly the two scopes — render its current value in the list, and persist
// the pick through the staged write.

// popupScopeEntry finds the schema entry, failing when the setting is
// config-file-only.
func popupScopeEntry(t *testing.T) Entry {
	t.Helper()
	for _, p := range BasePages([]string{"default"}, nil, nil) {
		for _, e := range p.Entries {
			if e.Key == "terminal.popup_scope" {
				return e
			}
		}
	}
	t.Fatal("terminal.popup_scope missing from the settings schema")
	return Entry{}
}

func TestPopupScopeEntryInSchema(t *testing.T) {
	e := popupScopeEntry(t)
	if e.Type != Enum {
		t.Fatalf("entry type = %v, want Enum", e.Type)
	}
	if got := strings.Join(e.Options, ","); got != "project,global" {
		t.Fatalf("options = %q, want the two scopes project,global", got)
	}
	if e.Scope != config.UserScope {
		t.Fatalf("scope = %v, want UserScope", e.Scope)
	}
	if e.Title == "" || e.Description == "" {
		t.Fatalf("entry = %+v, want a title and a description", e)
	}
}

// TestPopupScopeEditAndPersist drives the real entry through the panel: the
// row shows the default, the enum editor picks "global", and the staged
// commit lands it in the config.
func TestPopupScopeEditAndPersist(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "Terminal", Entries: []Entry{popupScopeEntry(t)}}}, testOpts(t))
	m.SetSize(130, 20)
	m.Open()
	if v := m.View(); !strings.Contains(v, "project") {
		t.Fatalf("the list must render the current value:\n%s", v)
	}
	m.Update(key("tab"))
	if cmd := m.Update(key("enter")); cmd != nil || m.focus != detailColumn {
		t.Fatalf("enter must focus the enum editor, focus=%v", m.focus)
	}
	ed, ok := m.editor.(*enumEditor)
	if !ok {
		t.Fatalf("editor = %T, want *enumEditor", m.editor)
	}
	// The editor offers exactly the two valid scopes — no free text, so an
	// unknown scope cannot be entered from the UI at all.
	if got := ed.matches(); len(got) != 2 {
		t.Fatalf("option list = %v, want the two scopes", got)
	}
	m.Update(key("down"))
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Terminal.PopupScope; got != "global" {
		t.Fatalf("popup_scope = %q, want %q", got, "global")
	}
	if v := m.View(); !strings.Contains(v, "global") {
		t.Fatalf("the list must render the new value:\n%s", v)
	}
}
