package finder

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"ike/internal/locations"
	"ike/internal/search"
)

// TestViewNeverWrapsRows guards #971: the box renders boxW-2 total cells
// including the border, so rows built against a wider inner width soft-wrap
// word-wise onto extra lines.
func TestViewNeverWrapsRows(t *testing.T) {
	m := New(search.New(nil))
	m.Open("")
	m.SetSize(100, 40)
	m.query = "architecture/high"
	long := "* [Syntax Highlighting](/architecture/highlighting.md) - Tree-sitter lexical layer: per-language grammars parsed off-loop into theme-coloured spans"
	idx := strings.Index(long, "architecture/high")
	m.list.Append([]locations.Item{{Path: "wiki/architecture/index.md", Line: 26, Text: long, StartCol: idx, EndCol: idx + 25}})
	v := m.View()
	lines := strings.Split(v, "\n")
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w > 100 {
			t.Errorf("line %d width %d > terminal", i, w)
		}
	}
	// title, blank, search, toggles, include, exclude, blank, group header,
	// item, blank, status + 2 border rows = 13; a wrapped row adds a 14th.
	if len(lines) != 13 {
		t.Fatalf("view has %d lines, want 13 (a row wrapped)", len(lines))
	}
}
