package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
)

// TestInputRowPreselect covers the remembered-query highlight (#277): the
// preselect style only applies to the field marked isQuery, and only while
// preselect is on and the value is non-empty.
func TestInputRowPreselect(t *testing.T) {
	pal := theme.DefaultPalette()

	preselected := InputRow(pal, "Search", "term", true, true, false, 0, 80)
	if !strings.Contains(preselected, "\x1b[7m") {
		t.Errorf("preselected query field must render reverse video: %q", preselected)
	}

	notQuery := InputRow(pal, "Replace", "term", false, true, false, 0, 80)
	if strings.Contains(notQuery, "\x1b[7m") {
		t.Errorf("a non-query field must not get the preselect highlight: %q", notQuery)
	}

	notPreselected := InputRow(pal, "Search", "term", true, false, false, 0, 80)
	if strings.Contains(notPreselected, "\x1b[7m") {
		t.Errorf("preselect=false must not render reverse video: %q", notPreselected)
	}
}

// TestInputRowHardTruncate pins the #971 rule: overlong content is cut, not
// wrapped by lipgloss MaxWidth.
func TestInputRowHardTruncate(t *testing.T) {
	pal := theme.DefaultPalette()
	row := InputRow(pal, "Search", strings.Repeat("x", 50), false, false, false, 0, 10)
	if w := ansi.StringWidth(row); w > 10 {
		t.Errorf("InputRow must truncate to width 10, rendered width %d: %q", w, row)
	}
	if strings.Contains(row, "\n") {
		t.Errorf("InputRow must not wrap: %q", row)
	}
}

// TestTogglesRow covers the [x]/[ ] rendering and the truncation rule shared
// by both toggle rows.
func TestTogglesRow(t *testing.T) {
	pal := theme.DefaultPalette()
	row := TogglesRow(pal, "  ", 80,
		Toggle{Label: "Case", Active: true},
		Toggle{Label: "Word", Active: false},
	)
	if !strings.Contains(row, "[x] Case") {
		t.Errorf("active toggle must render [x]: %q", row)
	}
	if !strings.Contains(row, "[ ] Word") {
		t.Errorf("inactive toggle must render [ ]: %q", row)
	}

	truncated := TogglesRow(pal, "", 5, Toggle{Label: strings.Repeat("y", 40), Active: true})
	if w := ansi.StringWidth(truncated); w > 5 {
		t.Errorf("TogglesRow must truncate to width 5, rendered width %d: %q", w, truncated)
	}
}
