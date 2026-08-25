package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// covermarks_test.go guards the coverage-marks toggle (#2081): present as a
// boolean, on by default, and editable end-to-end like any schema entry.

// TestCoverageMarksEntryShape: the key is a boolean and defaults to on.
func TestCoverageMarksEntryShape(t *testing.T) {
	if e := testsEntry(t, "editor.marks.coverage"); e.Type != Bool {
		t.Fatalf("editor.marks.coverage type = %v, want Bool", e.Type)
	}
	if d := config.Defaults(); d["editor.marks.coverage"] != "true" {
		t.Fatal("editor.marks.coverage must default to on")
	}
}

// TestCoverageMarksRoundTrip: toggling through the panel persists, and the
// reloaded config reads it back.
func TestCoverageMarksRoundTrip(t *testing.T) {
	restoreConfig(t)
	e := testsEntry(t, "editor.marks.coverage")
	m := New([]Page{{Title: "Editor", Entries: []Entry{e}}}, testOpts(t))
	m.SetSize(90, 24)
	m.Open()

	m.writeValue(e, false)
	commit(t, m)
	if config.Get().Editor.Marks.Coverage {
		t.Fatal("persisted editor.marks.coverage must be false")
	}
	if got := m.value("editor.marks.coverage"); got != "false" {
		t.Fatalf("panel value = %q, want false", got)
	}
	if v := m.View(); !strings.Contains(v, "false") {
		t.Fatalf("the entry row must render the value:\n%s", v)
	}
}
