package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// complexityhints_test.go guards the inline complexity hints mode
// (#2919, #2966): an all / warn / off enum on the Editor pages, all by
// default, and editable end-to-end like any schema entry.

// TestComplexityMarksEntryShape: the key is a three-value enum, default all.
func TestComplexityMarksEntryShape(t *testing.T) {
	if e := testsEntry(t, "editor.marks.complexity"); e.Type != Enum || strings.Join(e.Options, ",") != "all,warn,off" {
		t.Fatalf("editor.marks.complexity = %v %v, want Enum all/warn/off", e.Type, e.Options)
	}
	if d := config.Defaults(); d["editor.marks.complexity"] != "all" {
		t.Fatal("editor.marks.complexity must default to all")
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

	m.writeValue(e, "warn")
	commit(t, m)
	if config.Get().Editor.Marks.Complexity != config.HintWarn {
		t.Fatal("persisted editor.marks.complexity must be warn")
	}
	if got := m.value("editor.marks.complexity"); got != "warn" {
		t.Fatalf("panel value = %q, want warn", got)
	}
	if v := m.View(); !strings.Contains(v, "warn") {
		t.Fatalf("the entry row must render the value:\n%s", v)
	}
}
