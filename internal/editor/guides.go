package editor

import (
	"charm.land/lipgloss/v2"

	"ike/internal/highlight"
	"ike/internal/theme"
)

// guides.go colors indent guides by depth (#1628). The vertical lines drawn at
// each indent stop (#64) take the same cycling rainbow palette bracket pairs
// take, one slot per indent level — which is what makes structure readable in
// YAML or Python, where nesting has no brackets to color at all.
//
// The rainbow color is mixed toward the plain indent-guide color: a guide is
// chrome, and full-strength syntax colors in the indentation would out-shout
// the code. Turning editor.rainbow_indent_guides off returns every guide to
// the flat theme color; the guides themselves stay gated by
// editor.indent_guides, so the two features toggle independently of each other
// and of rainbow brackets.

// guideTintFrac is how much of the rainbow color survives the mix toward the
// theme's indent-guide color.
const guideTintFrac = 0.55

// guideStyles returns the indent-guide styles indexed by depth slot: one entry
// (the flat theme color) when depth coloring is off, RainbowColors entries
// when it is on. renderLine resolves a cell with guideSlot.
func (m Model) guideStyles() []lipgloss.Style {
	flat := lipgloss.NewStyle().Foreground(m.theme().IndentGuide)
	if !m.rainbowGuides {
		return []lipgloss.Style{flat}
	}
	out := make([]lipgloss.Style, highlight.RainbowColors)
	for i := range out {
		st, ok := m.hlTheme.Style(highlight.RainbowCapture(i))
		if !ok {
			out[i] = flat
			continue
		}
		out[i] = lipgloss.NewStyle().Foreground(theme.Mix(st.GetForeground(), m.theme().IndentGuide, guideTintFrac))
	}
	return out
}

// guideSlot maps a guide cell to its palette slot: abs is the cell's display
// offset from the line start, so the first guide (one tab stop in) is level 1
// and takes slot 0 — the depth its brackets would take.
func guideSlot(abs, tabWidth, slots int) int {
	if slots <= 1 || tabWidth <= 0 {
		return 0
	}
	level := abs/tabWidth - 1
	if level < 0 {
		return 0
	}
	return level % slots
}

// unmatchedBracketStyle is the style an unmatched bracket renders with
// (#1628): the theme's error color plus an underline, so the mistake reads as
// a mistake on a palette where the rainbow slots are colorful anyway. A
// `theme.captures.bracket.unmatched` override replaces the color and keeps the
// underline.
func (m Model) unmatchedBracketStyle(st lipgloss.Style, ok bool) lipgloss.Style {
	if !ok {
		st = lipgloss.NewStyle().Foreground(m.theme().Error)
	}
	return st.Underline(true)
}
