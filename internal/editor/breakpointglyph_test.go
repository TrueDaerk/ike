package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/host"
)

// glyphEditor loads a 5-line file with the four breakpoint line sources
// wired over fixed sets (#2245).
func glyphEditor(t *testing.T, all, off, cond, log []int) Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bp.txt")
	if err := os.WriteFile(path, []byte("l0\nl1\nl2\nl3\nl4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	m.Configure(host.MapConfig{"editor.line_numbers": "true"})
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(60, 8)
	m.SetBreakpointSource(func(string) []int { return all })
	m.SetBreakpointDisabledSource(func(string) []int { return off })
	m.SetBreakpointConditionalSource(func(string) []int { return cond })
	m.SetBreakpointLogpointSource(func(string) []int { return log })
	return m
}

// TestGutterGlyphsPerKind draws a distinct marker per breakpoint kind: ●
// plain, ◉ conditional, ◆ logpoint (#2245).
func TestGutterGlyphsPerKind(t *testing.T) {
	m := glyphEditor(t, []int{0, 1, 2}, nil, []int{1}, []int{2})
	rows := strings.Split(m.View(), "\n")
	for i, want := range []string{"●", "◉", "◆"} {
		if !strings.Contains(rows[i], want) {
			t.Fatalf("row %d = %q, want the %q glyph", i, rows[i], want)
		}
	}
	// Each glyph appears exactly once — no kind leaks onto another line.
	for _, glyph := range []string{"●", "◉", "◆"} {
		if n := strings.Count(m.View(), glyph); n != 1 {
			t.Fatalf("glyph %q drawn %d times, want 1:\n%s", glyph, n, m.View())
		}
	}
}

// TestGutterGlyphsDisabledKeepTheirShape renders the hollow member of each
// pair for a disabled breakpoint, so a muted logpoint still reads as one.
func TestGutterGlyphsDisabledKeepTheirShape(t *testing.T) {
	m := glyphEditor(t, []int{0, 1, 2}, []int{0, 1, 2}, []int{1}, []int{2})
	rows := strings.Split(m.View(), "\n")
	for i, want := range []string{"○", "◎", "◇"} {
		if !strings.Contains(rows[i], want) {
			t.Fatalf("row %d = %q, want the disabled %q glyph", i, rows[i], want)
		}
	}
}

// TestGutterGlyphsWithoutKindSources stays on the plain marker when the app
// wires no kind lookups (a parked workspace, #2245).
func TestGutterGlyphsWithoutKindSources(t *testing.T) {
	m := glyphEditor(t, []int{0}, nil, nil, nil)
	if !strings.Contains(strings.Split(m.View(), "\n")[0], "●") {
		t.Fatalf("plain breakpoint lost its glyph:\n%s", m.View())
	}
}

// TestPausedLineOutranksBreakpointKind keeps the paused marker on top of the
// kind glyphs, as it already outranks the plain one (#579).
func TestPausedLineOutranksBreakpointKind(t *testing.T) {
	m := glyphEditor(t, []int{1}, nil, nil, []int{1})
	m.SetPausedLine(1)
	row := strings.Split(m.View(), "\n")[1]
	if !strings.Contains(row, "▶") || strings.Contains(row, "◆") {
		t.Fatalf("row = %q, want the paused ▶ to win", row)
	}
}
