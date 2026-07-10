package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
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
