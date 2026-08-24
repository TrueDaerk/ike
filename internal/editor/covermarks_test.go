package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/coverage"
	"ike/internal/host"
	"ike/internal/lang"
)

// covermarks_test.go covers the gutter coverage marks (#2081): push, render,
// the editor.marks.coverage toggle, clearing, and edit-driven staleness.

func coverEditor(t *testing.T, cfg host.MapConfig) Model {
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
	m.SetFocused(true)
	return m
}

func coverMsg(m Model, stale bool) coverage.MarksMsg {
	return coverage.MarksMsg{Path: m.Path(), Stale: stale, Marks: map[int]lang.CoverKind{
		0: lang.CoverCovered,
		1: lang.CoverUncovered,
		2: lang.CoverPartial,
	}}
}

// TestGutterRendersCoverageBars: the bar glyph lands in the sign column for
// every marked line.
func TestGutterRendersCoverageBars(t *testing.T) {
	m := coverEditor(t, nil)
	m, _ = m.Update(coverMsg(m, false))
	if got := strings.Count(m.View(), "▎"); got != 3 {
		t.Fatalf("expected 3 coverage bars, got %d:\n%s", got, m.View())
	}
	if k, stale, ok := m.coverMarkAt(1); !ok || stale || k != lang.CoverUncovered {
		t.Fatalf("coverMarkAt(1) = %v %v %v", k, stale, ok)
	}
	if _, _, ok := m.coverMarkAt(4); ok {
		t.Fatal("an unmarked line must report no mark")
	}
}

// TestCoverageMarksIgnoreOtherPaths: a foreign document's marks never land.
func TestCoverageMarksIgnoreOtherPaths(t *testing.T) {
	m := coverEditor(t, nil)
	m, _ = m.Update(coverage.MarksMsg{Path: "/elsewhere/x.go", Marks: map[int]lang.CoverKind{0: lang.CoverCovered}})
	if strings.Contains(m.View(), "▎") {
		t.Fatal("marks for another path must not render")
	}
}

// TestCoverageMarksToggleHides: the editor.marks.coverage switch stops
// rendering; cached marks survive the round-trip.
func TestCoverageMarksToggleHides(t *testing.T) {
	cfg := host.MapConfig{}
	m := coverEditor(t, cfg)
	m, _ = m.Update(coverMsg(m, false))
	cfg["editor.marks.coverage"] = "false"
	if strings.Contains(m.View(), "▎") {
		t.Fatal("toggle off must hide the bars")
	}
	cfg["editor.marks.coverage"] = "true"
	if !strings.Contains(m.View(), "▎") {
		t.Fatal("toggle on must restore the cached bars")
	}
}

// TestCoverageMarksClear: nil marks (the display toggle hiding them) clear.
func TestCoverageMarksClear(t *testing.T) {
	m := coverEditor(t, nil)
	m, _ = m.Update(coverMsg(m, false))
	m, _ = m.Update(coverage.MarksMsg{Path: m.Path()})
	if strings.Contains(m.View(), "▎") {
		t.Fatal("nil marks must clear the bars")
	}
}

// TestCoverageMarksGoStaleOnEdit: any buffer mutation neutralizes the data —
// the mark stays visible but reports stale.
func TestCoverageMarksGoStaleOnEdit(t *testing.T) {
	m := coverEditor(t, nil)
	m, _ = m.Update(coverMsg(m, false))
	m = send(m, key('i'), key('x'), special(27)) // insert a char, back to normal
	if _, stale, ok := m.coverMarkAt(0); !ok || !stale {
		t.Fatalf("marks must survive the edit but turn stale (ok=%v stale=%v)", ok, stale)
	}
	if !strings.Contains(m.View(), "▎") {
		t.Fatal("stale marks still render (faint)")
	}
}

// TestCoverageMarksStaleFlag: app-pushed store staleness carries through.
func TestCoverageMarksStaleFlag(t *testing.T) {
	m := coverEditor(t, nil)
	m, _ = m.Update(coverMsg(m, true))
	if _, stale, ok := m.coverMarkAt(0); !ok || !stale {
		t.Fatal("a Stale push must report stale marks")
	}
}
