package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
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
