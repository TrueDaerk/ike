package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

func inheritEditor(t *testing.T, cfg host.MapConfig) Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte("l0\nl1\nl2\nl3\nl4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if cfg == nil {
		cfg = host.MapConfig{}
	}
	cfg["editor.line_numbers"] = "true"
	m.Configure(cfg)
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(40, 10)
	return m
}

func marksMsg(m Model, marks ...ilsp.InheritanceMark) ilsp.InheritanceMarksMsg {
	return ilsp.InheritanceMarksMsg{Path: m.Path(), Marks: marks}
}

// TestGutterRendersInheritanceArrows: ↑ and ↓ land in the sign column.
func TestGutterRendersInheritanceArrows(t *testing.T) {
	m := inheritEditor(t, nil)
	m, _ = m.Update(marksMsg(m,
		ilsp.InheritanceMark{Line: 1, Kind: ilsp.InheritanceImplemented},
		ilsp.InheritanceMark{Line: 3, Kind: ilsp.InheritanceImplements},
	))
	view := m.View()
	if !strings.Contains(view, "↓") || !strings.Contains(view, "↑") {
		t.Fatalf("expected both arrows in the gutter:\n%s", view)
	}
}

// TestInheritanceMarksIgnoreOtherPaths: a foreign document's marks never land.
func TestInheritanceMarksIgnoreOtherPaths(t *testing.T) {
	m := inheritEditor(t, nil)
	m, _ = m.Update(ilsp.InheritanceMarksMsg{Path: "/elsewhere/x.go", Marks: []ilsp.InheritanceMark{{Line: 1, Kind: ilsp.InheritanceImplements}}})
	if strings.Contains(m.View(), "↑") {
		t.Fatal("marks for another path must not render")
	}
}

// TestInheritanceMarksToggleHides: the editor.marks.inheritance switch stops
// rendering; cached marks survive the round-trip.
func TestInheritanceMarksToggleHides(t *testing.T) {
	m := inheritEditor(t, host.MapConfig{"editor.marks.inheritance": "false"})
	m, _ = m.Update(marksMsg(m, ilsp.InheritanceMark{Line: 1, Kind: ilsp.InheritanceImplements}))
	if strings.Contains(m.View(), "↑") {
		t.Fatal("toggle off must hide the arrows")
	}
	m.Configure(host.MapConfig{"editor.line_numbers": "true", "editor.marks.inheritance": "true"})
	if !strings.Contains(m.View(), "↑") {
		t.Fatal("cached marks must render again after toggling back on")
	}
}

// TestInheritanceMarksClearOnEmpty: an empty set clears previous marks.
func TestInheritanceMarksClearOnEmpty(t *testing.T) {
	m := inheritEditor(t, nil)
	m, _ = m.Update(marksMsg(m, ilsp.InheritanceMark{Line: 1, Kind: ilsp.InheritanceImplements}))
	m, _ = m.Update(marksMsg(m))
	if strings.Contains(m.View(), "↑") {
		t.Fatal("an empty set must clear the arrows")
	}
}

// TestBreakpointOutranksInheritance: the actionable ● keeps the sign cell.
func TestBreakpointOutranksInheritance(t *testing.T) {
	m := inheritEditor(t, nil)
	m.SetBreakpointSource(func(string) []int { return []int{1} })
	m, _ = m.Update(marksMsg(m, ilsp.InheritanceMark{Line: 1, Kind: ilsp.InheritanceImplements}))
	view := m.View()
	if !strings.Contains(view, "●") {
		t.Fatalf("breakpoint glyph missing:\n%s", view)
	}
	if strings.Contains(view, "↑") {
		t.Fatal("the breakpoint must outrank the inheritance arrow on the same line")
	}
}
