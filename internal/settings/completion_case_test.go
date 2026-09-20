package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// completion_case_test.go covers the Settings-UI half of the completion hump
// filter's case rule (#2650): completion.case_sensitivity is an enum entry on
// the Language Support page that offers exactly the validated values,
// renders its value in the list and persists a pick.

func completionCaseEntry(t *testing.T) Entry {
	t.Helper()
	for _, p := range BasePages(nil, nil, nil) {
		for _, e := range p.Entries {
			if e.Key == "completion.case_sensitivity" {
				if p.Title != "Language Support" {
					t.Fatalf("entry lives on page %q, want Language Support", p.Title)
				}
				return e
			}
		}
	}
	t.Fatal("completion.case_sensitivity must be configurable in the settings UI")
	return Entry{}
}

func TestCompletionCaseEntryInSchema(t *testing.T) {
	e := completionCaseEntry(t)
	if e.Type != Enum {
		t.Fatalf("type = %v, want Enum", e.Type)
	}
	if got := strings.Join(e.Options, ","); got != "none,first_letter,all" {
		t.Fatalf("options = %q, want none,first_letter,all", got)
	}
	if e.Scope != config.UserScope || e.Title == "" || len(e.Description) < 40 {
		t.Fatalf("entry needs a user-scoped title and a real description: %#v", e)
	}
}

func TestCompletionCaseEditAndPersist(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "Language Support", Entries: []Entry{completionCaseEntry(t)}}}, testOpts(t))
	m.SetSize(130, 20)
	m.Open()
	if v := m.View(); !strings.Contains(v, "first_letter") {
		t.Fatalf("the list must render the default value:\n%s", v)
	}
	m.Update(key("tab"))
	m.Update(key("enter"))
	ed, ok := m.editor.(*enumEditor)
	if !ok {
		t.Fatalf("editor = %T, want *enumEditor", m.editor)
	}
	// No free text: only the three validated values can be picked.
	if got := ed.matches(); len(got) != 3 {
		t.Fatalf("option list = %v, want the three case rules", got)
	}
	m.Update(key("down"))
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Completion.CaseSensitivity; got != "all" {
		t.Fatalf("case_sensitivity = %q, want all", got)
	}
	if v := m.View(); !strings.Contains(v, "all") {
		t.Fatalf("the list must render the new value:\n%s", v)
	}
}
