package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"ike/internal/theme"
)

// TestTabbedLinesStayInWidth ensures tab-indented lines render within the pane
// width (tabs expanded to spaces) and never emit a raw tab the terminal would
// expand past the budget.
func TestTabbedLinesStayInWidth(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 40; i++ {
		sb.WriteString("\t\t// some indented comment that runs fairly long across the pane\n")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "host.go")
	os.WriteFile(path, []byte(sb.String()), 0o644)
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetFocused(true)
	w, h := 30, 12
	m.SetSize(w, h)
	v := m.View()
	if strings.Contains(v, "\t") {
		t.Error("view still contains a raw tab; tabs must be expanded")
	}
	for i, ln := range strings.Split(v, "\n") {
		if got := lipgloss.Width(ln); got > w {
			t.Errorf("line %d width %d exceeds pane width %d", i, got, w)
		}
	}
	if lipgloss.Height(v) > h {
		t.Errorf("height %d exceeds %d", lipgloss.Height(v), h)
	}
}

// TestScrollXBy scrolls the viewport horizontally without moving the cursor,
// clamped so the longest visible line keeps its last character on screen (#230).
func TestScrollXBy(t *testing.T) {
	long := strings.Repeat("abcdefghij", 10) // 100 cols
	m, _ := loaded(t, long+"\nshort\n")
	m.SetSize(20, 10)

	m.ScrollXBy(5)
	if _, left := m.ScrollOffset(); left != 5 {
		t.Fatalf("left=%d want 5", left)
	}
	if m.cursor.Col != 0 {
		t.Fatalf("cursor moved to col %d; horizontal scroll must not move it", m.cursor.Col)
	}

	m.ScrollXBy(1000) // clamp: last char of the longest visible line stays on screen
	if _, left := m.ScrollOffset(); left != len(long)-1 {
		t.Fatalf("left=%d want %d", left, len(long)-1)
	}

	m.ScrollXBy(-1000) // clamp at 0
	if _, left := m.ScrollOffset(); left != 0 {
		t.Fatalf("left=%d want 0", left)
	}
}

// TestCaretColorTracksMode guards the mode-coloured caret (#1323): the caret
// cell is painted in the input mode's colour, so the mode is visible right
// where the user is typing, and the colour changes with the mode.
func TestCaretColorTracksMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	os.WriteFile(path, []byte("package main\n"), 0o644)
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetFocused(true)
	m.SetSize(40, 5)

	bgSGR := func(md Mode) string {
		r, g, b, _ := ModeColor(md, m.theme()).RGBA()
		return fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)
	}
	if v := m.View(); !strings.Contains(v, bgSGR(Normal)) {
		t.Fatalf("normal-mode caret missing its colour: %q", v)
	}
	m.mode = Insert
	m.bumpRender()
	v := m.View()
	if !strings.Contains(v, bgSGR(Insert)) {
		t.Fatalf("insert-mode caret missing its colour: %q", v)
	}
	if strings.Contains(v, bgSGR(Normal)) {
		t.Fatalf("insert mode must not paint the normal caret colour: %q", v)
	}
}

// TestModeColorsAreDistinct guards that the mode signals stay tellable apart:
// normal, insert, visual and replace each get their own colour.
func TestModeColorsAreDistinct(t *testing.T) {
	seen := map[string]Mode{}
	for _, md := range []Mode{Normal, Insert, Visual, Replace, Command} {
		r, g, b, _ := ModeColor(md, nil).RGBA()
		key := fmt.Sprintf("%d/%d/%d", r, g, b)
		if other, dup := seen[key]; dup {
			t.Fatalf("%v and %v share a colour", other, md)
		}
		seen[key] = md
	}
}

// TestModeBadgeContrast guards readability (#1323, #384): on every built-in
// theme the caret/badge text colour picked for a mode colour clears the WCAG AA
// threshold for normal text.
func TestModeBadgeContrast(t *testing.T) {
	const minContrast = 4.5
	for _, th := range theme.Builtins() {
		pal := theme.NewPalette(th)
		for _, md := range []Mode{Normal, Insert, Visual, VisualLine, VisualBlock, Replace, Command} {
			bg := ModeColor(md, pal)
			fg := theme.Readable(bg, pal.Background, pal.Foreground)
			if r := theme.ContrastRatio(fg, bg); r < minContrast {
				t.Errorf("%s/%s: contrast %.2f:1, want >= %.1f:1", th.Name, md, r, minContrast)
			}
		}
	}
}
