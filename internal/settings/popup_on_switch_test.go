package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// popup_on_switch_test.go covers the Settings UI surface of
// terminal.popup_on_switch (#2362): the setting must be editable in the panel
// — an enum offering exactly the two modes — render its current value in the
// list, and persist the pick through the staged write.

// popupOnSwitchEntry finds the schema entry, failing when the setting is
// config-file-only.
func popupOnSwitchEntry(t *testing.T) Entry {
	t.Helper()
	for _, p := range BasePages([]string{"default"}, nil, nil) {
		for _, e := range p.Entries {
			if e.Key == "terminal.popup_on_switch" {
				return e
			}
		}
	}
	t.Fatal("terminal.popup_on_switch missing from the settings schema")
	return Entry{}
}

func TestPopupOnSwitchEntryInSchema(t *testing.T) {
	e := popupOnSwitchEntry(t)
	if e.Type != Enum {
		t.Fatalf("entry type = %v, want Enum", e.Type)
	}
	if got := strings.Join(e.Options, ","); got != "restore,always-open" {
		t.Fatalf("options = %q, want the two modes restore,always-open", got)
	}
	if e.Scope != config.UserScope {
		t.Fatalf("scope = %v, want UserScope", e.Scope)
	}
	if e.Title == "" || e.Description == "" {
		t.Fatalf("entry = %+v, want a title and a description", e)
	}
}

// TestPopupOnSwitchEditAndPersist drives the real entry through the panel:
// the row shows the default, the enum editor picks "always-open", and the
// staged commit lands it in the config.
func TestPopupOnSwitchEditAndPersist(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "Terminal", Entries: []Entry{popupOnSwitchEntry(t)}}}, testOpts(t))
	m.SetSize(130, 20)
	m.Open()
	if v := m.View(); !strings.Contains(v, "restore") {
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
	// The editor offers exactly the two valid modes — no free text, so an
	// unknown mode cannot be entered from the UI at all.
	if got := ed.matches(); len(got) != 2 {
		t.Fatalf("option list = %v, want the two modes", got)
	}
	m.Update(key("down"))
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Terminal.PopupOnSwitch; got != "always-open" {
		t.Fatalf("popup_on_switch = %q, want %q", got, "always-open")
	}
	if v := m.View(); !strings.Contains(v, "always-open") {
		t.Fatalf("the list must render the new value:\n%s", v)
	}
}
