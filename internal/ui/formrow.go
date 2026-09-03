package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
)

// InputRow renders one labelled input line with a block cursor on the
// focused field, shared by the find-in-path form (allfind) and the
// find-usages form (finder), whose own inputRow was otherwise byte-for-byte
// the same function.
//
// isQuery marks the field eligible for the preselect highlight — the
// remembered query is "selected" (#277) so it reads as replace-on-type;
// preselect gates that highlight, and focused/cur drive the ordinary block
// cursor everywhere else.
//
// Hard truncate: lipgloss MaxWidth WRAPS overlong content (#971).
func InputRow(pal *theme.Palette, label, value string, isQuery, preselect, focused bool, cur, width int) string {
	lab := lipgloss.NewStyle().Faint(true).Render(label + " ")
	text := value
	switch {
	case isQuery && preselect && value != "":
		text = lipgloss.NewStyle().Reverse(true).Render(value)
		if focused {
			text += lipgloss.NewStyle().Reverse(true).Render(" ")
		}
	case focused:
		text = CursorView(value, cur)
	}
	row := lab + text
	if focused {
		row = lipgloss.NewStyle().Foreground(pal.Foreground).Render(lab) + text
	}
	return ansi.Truncate(row, width, "…")
}

// Toggle is one match-mode toggle's label and current state, as rendered by
// TogglesRow.
type Toggle struct {
	Label  string
	Active bool
}

// TogglesRow renders a row of "[x] Label" / "[ ] Label" toggles, indented and
// separated by two spaces, truncated to width. Shared by the find-in-path
// form (allfind) and the find-usages form (finder), whose own togglesRow was
// otherwise byte-for-byte the same function.
func TogglesRow(pal *theme.Palette, indent string, width int, toggles ...Toggle) string {
	on := lipgloss.NewStyle().Foreground(pal.BorderFocus).Bold(true)
	off := lipgloss.NewStyle().Faint(true)
	parts := make([]string, len(toggles))
	for i, t := range toggles {
		if t.Active {
			parts[i] = on.Render("[x] " + t.Label)
		} else {
			parts[i] = off.Render("[ ] " + t.Label)
		}
	}
	return ansi.Truncate(indent+strings.Join(parts, "  "), width, "…")
}
