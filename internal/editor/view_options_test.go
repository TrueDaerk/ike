package editor

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/host"
)

// firstRow returns the first rendered row without styling or the gutter-free
// trailing padding.
func firstRow(m Model) string {
	return strings.TrimRight(strings.SplitN(ansi.Strip(m.View()), "\n", 2)[0], "\n")
}

func TestWhitespaceTrailingRendersOnlyLineEndRun(t *testing.T) {
	m, _ := loaded(t, "\tab cd  \nx\n")
	m.Configure(host.MapConfig{"editor.show_whitespace": "trailing", "editor.line_numbers": "false"})
	row := firstRow(m)
	if got := strings.Count(row, "·"); got != 2 {
		t.Errorf("trailing mode rendered %d dots, want 2 in %q", got, row)
	}
	if strings.Contains(row, "→") {
		t.Errorf("trailing mode rendered the leading tab in %q", row)
	}
}

func TestWhitespaceAllRendersEveryRun(t *testing.T) {
	m, _ := loaded(t, "\tab cd  \n")
	m.Configure(host.MapConfig{"editor.show_whitespace": "all", "editor.line_numbers": "false"})
	row := firstRow(m)
	if !strings.Contains(row, "→") {
		t.Errorf("all mode did not render the tab in %q", row)
	}
	if got := strings.Count(row, "·"); got != 3 {
		t.Errorf("all mode rendered %d dots, want 3 in %q", got, row)
	}
}

func TestIndentGuidesAtIndentStops(t *testing.T) {
	m, _ := loaded(t, "        deep\n")
	m.Configure(host.MapConfig{"editor.indent_guides": "true", "editor.tab_width": "4", "editor.line_numbers": "false"})
	row := firstRow(m)
	if got := strings.Count(row, "│"); got != 1 {
		t.Fatalf("rendered %d indent guides, want 1 (at cell 4) in %q", got, row)
	}
	if idx := strings.IndexRune(row, '│'); len([]rune(row[:idx])) != 4 {
		t.Errorf("guide sits at cell %d, want 4 in %q", len([]rune(row[:idx])), row)
	}
}

func TestIndentGuidesOnTabs(t *testing.T) {
	m, _ := loaded(t, "\t\tdeep\n")
	m.Configure(host.MapConfig{"editor.indent_guides": "true", "editor.tab_width": "4", "editor.line_numbers": "false"})
	row := firstRow(m)
	// The second tab starts at cell 4 — an indent stop — so it carries a guide.
	if got := strings.Count(row, "│"); got != 1 {
		t.Fatalf("rendered %d indent guides, want 1 in %q", got, row)
	}
}

func TestWhitespaceWinsOverIndentGuides(t *testing.T) {
	m, _ := loaded(t, "        x\n")
	m.Configure(host.MapConfig{
		"editor.indent_guides": "true", "editor.show_whitespace": "all",
		"editor.tab_width": "4", "editor.line_numbers": "false",
	})
	row := firstRow(m)
	if strings.Contains(row, "│") {
		t.Errorf("indent guide rendered under whitespace mode all in %q", row)
	}
	if got := strings.Count(row, "·"); got != 8 {
		t.Errorf("rendered %d dots, want 8 in %q", got, row)
	}
}

func TestRulerPadsShortLines(t *testing.T) {
	m, _ := loaded(t, "ab\n")
	m.Configure(host.MapConfig{"editor.rulers": "8", "editor.line_numbers": "false"})
	m.SetSize(40, 5)
	rows := strings.Split(m.View(), "\n")
	if got := lipgloss.Width(rows[0]); got != 9 {
		t.Fatalf("row width %d, want 9 (padded through the ruler column) in %q", got, rows[0])
	}
	// The ruler cell carries a background — the row is styled beyond the content.
	if !strings.Contains(rows[0], "\x1b[") {
		t.Errorf("ruler row %q carries no styling", rows[0])
	}
}

func TestRulerRespectsPaneWidth(t *testing.T) {
	m, _ := loaded(t, "ab\n")
	m.Configure(host.MapConfig{"editor.rulers": "120", "editor.line_numbers": "false"})
	m.SetSize(20, 5)
	for i, r := range strings.Split(m.View(), "\n") {
		if got := lipgloss.Width(r); got > 20 {
			t.Errorf("row %d width %d exceeds pane width 20", i, got)
		}
	}
}
