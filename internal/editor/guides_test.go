package editor

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"ike/internal/bracket"
	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	"ike/internal/host"
	"ike/internal/theme"
)

// guideModel is a model with a real palette, indent guides on and a tab width
// of 4 — the state every guide test needs.
func guideModel(t *testing.T, text string) Model {
	t.Helper()
	m, _ := loaded(t, text)
	m.SetPalette(theme.DefaultPalette())
	m.Configure(host.MapConfig{
		"editor.indent_guides": "true", "editor.tab_width": "4", "editor.line_numbers": "false",
	})
	return m
}

// TestGuideStylesFlatWhenRainbowOff (#1628): with depth coloring off there is
// exactly one guide style, the theme's own indent-guide colour.
func TestGuideStylesFlatWhenRainbowOff(t *testing.T) {
	m := guideModel(t, "            deep\n")
	m.Configure(host.MapConfig{"editor.rainbow_indent_guides": "false"})
	styles := m.guideStyles()
	if len(styles) != 1 {
		t.Fatalf("got %d guide styles, want 1 while depth coloring is off", len(styles))
	}
	if styles[0].GetForeground() != m.theme().IndentGuide {
		t.Errorf("flat guide colour = %v want the theme's IndentGuide", styles[0].GetForeground())
	}
}

// TestGuideStylesCycleByDepth (#1628): depth coloring yields one style per
// rainbow slot, and the slots differ from each other and from the flat colour.
func TestGuideStylesCycleByDepth(t *testing.T) {
	m := guideModel(t, "            deep\n")
	m.Configure(host.MapConfig{"editor.rainbow_indent_guides": "true"})
	styles := m.guideStyles()
	if len(styles) != highlight.RainbowColors {
		t.Fatalf("got %d guide styles, want %d", len(styles), highlight.RainbowColors)
	}
	seen := map[string]bool{}
	for i, st := range styles {
		fg := st.GetForeground()
		if fg == m.theme().IndentGuide {
			t.Errorf("slot %d kept the flat guide colour", i)
		}
		seen[fgKey(t, fg)] = true
	}
	if len(seen) < 4 {
		t.Errorf("only %d distinct guide colours across %d slots", len(seen), len(styles))
	}
}

// TestGuideColourIsMixedTowardChrome (#1628): a guide is chrome, so its colour
// is the rainbow slot pulled toward the plain guide colour — never the raw
// syntax colour, which would out-shout the code it indents.
func TestGuideColourIsMixedTowardChrome(t *testing.T) {
	m := guideModel(t, "        deep\n")
	m.Configure(host.MapConfig{"editor.rainbow_indent_guides": "true"})
	raw, ok := m.hlTheme.Style(highlight.RainbowCapture(0))
	if !ok {
		t.Fatal("the default theme must define rainbow.0")
	}
	if got := m.guideStyles()[0].GetForeground(); fgKey(t, got) == fgKey(t, raw.GetForeground()) {
		t.Errorf("guide colour = %v, want it mixed toward the guide colour", got)
	}
}

// TestGuideSlotByLevel (#1628): the first guide (one tab stop in) takes slot
// 0, the next slot 1, and the palette cycles.
func TestGuideSlotByLevel(t *testing.T) {
	slots := highlight.RainbowColors
	for _, c := range []struct{ abs, want int }{
		{4, 0}, {8, 1}, {12, 2}, {4 * slots, slots - 1}, {4 * (slots + 1), 0},
	} {
		if got := guideSlot(c.abs, 4, slots); got != c.want {
			t.Errorf("guideSlot(%d) = %d want %d", c.abs, got, c.want)
		}
	}
	// A single style (depth coloring off) always resolves to its only entry.
	if got := guideSlot(12, 4, 1); got != 0 {
		t.Errorf("guideSlot with one style = %d want 0", got)
	}
}

// TestDepthColoredGuidesStillRender (#1628): colouring changes the style of a
// guide cell, never which cells carry a guide.
func TestDepthColoredGuidesStillRender(t *testing.T) {
	m := guideModel(t, "            deep\n")
	m.Configure(host.MapConfig{"editor.rainbow_indent_guides": "true"})
	if got := strings.Count(firstRow(m), "│"); got != 2 {
		t.Fatalf("rendered %d guides, want 2 (cells 4 and 8)", got)
	}
}

// TestUnmatchedBracketRendersAsError (#1628): the bracket.Unmatched capture
// resolves to the theme's error colour with an underline, so a bracket without
// a partner reads as a mistake rather than as another nesting level.
func TestUnmatchedBracketRendersAsError(t *testing.T) {
	m := New()
	m.SetPalette(theme.DefaultPalette())
	m.buf = buffer.FromString("f(x")
	m.path = "main.go"
	m = feedSpans(t, m, highlight.SpansMsg{
		Path:  "main.go",
		Spans: []highlight.Span{{Line: 0, StartCol: 1, EndCol: 2, Capture: bracket.Unmatched}},
	})
	st, ok := m.styleAt(0, 1)
	if !ok {
		t.Fatal("an unmatched bracket must resolve to a style")
	}
	if st.GetForeground() != m.theme().Error {
		t.Errorf("unmatched bracket colour = %v want the theme's Error", st.GetForeground())
	}
	if !st.GetUnderline() {
		t.Error("an unmatched bracket must be underlined")
	}
}

// TestMatchedBracketKeepsItsRainbowSlot (#1628): the depth capture still
// resolves as a plain colour — the error styling is only for the unmatched
// ones.
func TestMatchedBracketKeepsItsRainbowSlot(t *testing.T) {
	m := New()
	m.SetPalette(theme.DefaultPalette())
	m.buf = buffer.FromString("f(x)")
	m.path = "main.go"
	m = feedSpans(t, m, highlight.SpansMsg{
		Path:  "main.go",
		Spans: []highlight.Span{{Line: 0, StartCol: 1, EndCol: 2, Capture: highlight.RainbowCapture(0)}},
	})
	st, ok := m.styleAt(0, 1)
	if !ok {
		t.Fatal("a rainbow slot must resolve to a style")
	}
	if st.GetUnderline() {
		t.Error("a matched bracket must not be underlined")
	}
	if st.GetForeground() == m.theme().Error {
		t.Error("a matched bracket must not take the error colour")
	}
}

// fgKey renders a colour as a comparable string.
func fgKey(t *testing.T, c color.Color) string {
	t.Helper()
	if c == nil {
		return "none"
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("%d,%d,%d", r, g, b)
}
