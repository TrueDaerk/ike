package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// issues_filter_test.go covers the Issues Window page's filter settings
// (#2115): issues.default_filter is a validated free-text expression and
// issues.saved_filters a validated list, both reachable in the Settings UI —
// a config-file-only setting would be a regression.

// issuesPages is the real Issues Window page, isolated for the form tests.
func issuesPages() []Page {
	for _, p := range BasePages(nil, nil, nil) {
		if p.Title == "Issues Window" {
			return []Page{p}
		}
	}
	return nil
}

// issuesEntry finds one entry of the Issues Window page.
func issuesEntry(t *testing.T, key string) Entry {
	t.Helper()
	pages := issuesPages()
	if len(pages) != 1 {
		t.Fatal("the settings schema has no Issues Window page")
	}
	for _, e := range pages[0].Entries {
		if e.Key == key {
			return e
		}
	}
	t.Fatalf("the Issues Window page has no %s entry", key)
	return Entry{}
}

func TestIssuesDefaultFilterEntry(t *testing.T) {
	e := issuesEntry(t, "issues.default_filter")
	if e.Type != String || e.ValidateString == nil {
		t.Fatalf("entry = %+v, want a String with a validator", e)
	}
	if msg := e.ValidateString(""); msg != "" {
		t.Errorf("the empty filter was rejected with %q, want it accepted", msg)
	}
	if msg := e.ValidateString(`is:open label:"good first issue" crash`); msg != "" {
		t.Errorf("a valid expression was rejected with %q", msg)
	}
	// The message has to name what is wrong — the form shows it verbatim.
	msg := e.ValidateString("author:me")
	if !strings.Contains(msg, "unknown qualifier") || !strings.Contains(msg, "is:") {
		t.Errorf("rejection = %q, want it to name the valid qualifiers", msg)
	}
	if !strings.Contains(e.Description, "is:open") {
		t.Errorf("the description must document the syntax: %q", e.Description)
	}
}

func TestIssuesSavedFiltersEntry(t *testing.T) {
	e := issuesEntry(t, "issues.saved_filters")
	if e.Type != List || e.ValidateEntry == nil {
		t.Fatalf("entry = %+v, want a List with element validation", e)
	}
	if msg := e.ValidateEntry(nil, "triage=is:open label:bug"); msg != "" {
		t.Errorf("a valid saved filter was rejected with %q", msg)
	}
	for _, bad := range []string{"triage", "=is:open", "triage=", "triage=is:merged"} {
		if msg := e.ValidateEntry(nil, bad); msg == "" {
			t.Errorf("%q accepted, want a rejection message", bad)
		}
	}
}

// The free-text editor refuses a value its entry cannot parse, keeps the
// editor open with the message, and leaves the config untouched.
func TestDefaultFilterRejectsABadExpression(t *testing.T) {
	m := issuesFilterPanel(t)
	ed := m.editor.(*textEditor)
	ed.tf.Set("author:me")
	m.Update(key("enter"))
	if ed.err == "" {
		t.Fatal("a broken expression must be refused with a message")
	}
	if m.editor == nil {
		t.Fatal("the editor must stay open so the value can be fixed")
	}
	if got := config.Get().Issues.DefaultFilter; got != "" {
		t.Fatalf("committed %q, want the broken value never written", got)
	}
}

func TestDefaultFilterCommits(t *testing.T) {
	m := issuesFilterPanel(t)
	ed := m.editor.(*textEditor)
	ed.tf.Set("is:all label:bug crash")
	m.Update(key("enter"))
	if ed.err != "" {
		t.Fatalf("a valid expression was refused: %s", ed.err)
	}
	commit(t, m)
	if got := config.Get().Issues.DefaultFilter; got != "is:all label:bug crash" {
		t.Fatalf("committed = %q", got)
	}
}

// issuesFilterPanel opens a panel on the real Issues Window page with the
// default filter's text editor focused.
func issuesFilterPanel(t *testing.T) *Model {
	t.Helper()
	restoreConfig(t)
	m := New(issuesPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.focus = formColumn
	for i, r := range m.rows() {
		if r.kind == rowEntry && r.entry.Key == "issues.default_filter" {
			m.sel = i
		}
	}
	m.Update(key("enter"))
	if _, ok := m.editor.(*textEditor); !ok {
		t.Fatalf("editor = %T, want the free-text editor", m.editor)
	}
	return m
}
