package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// complexityhints_test.go guards the inline complexity hints toggle
// (#2919): present as a boolean on the Editor pages, on by default, and
// editable end-to-end like any schema entry.

// TestComplexityMarksEntryShape: the key is a boolean and defaults to on.
func TestComplexityMarksEntryShape(t *testing.T) {
	if e := testsEntry(t, "editor.marks.complexity"); e.Type != Bool {
		t.Fatalf("editor.marks.complexity type = %v, want Bool", e.Type)
	}
	if d := config.Defaults(); d["editor.marks.complexity"] != "true" {
		t.Fatal("editor.marks.complexity must default to on")
	}
}

// TestComplexityMarksRoundTrip: toggling through the panel persists, and the
// reloaded config reads it back.
func TestComplexityMarksRoundTrip(t *testing.T) {
	restoreConfig(t)
	e := testsEntry(t, "editor.marks.complexity")
	m := New([]Page{{Title: "Editor", Entries: []Entry{e}}}, testOpts(t))
	m.SetSize(90, 24)
	m.Open()

	m.writeValue(e, false)
	commit(t, m)
	if config.Get().Editor.Marks.Complexity {
		t.Fatal("persisted editor.marks.complexity must be false")
	}
	if got := m.value("editor.marks.complexity"); got != "false" {
		t.Fatalf("panel value = %q, want false", got)
	}
	if v := m.View(); !strings.Contains(v, " off") {
		t.Fatalf("the entry row must render the value:\n%s", v)
	}
}
