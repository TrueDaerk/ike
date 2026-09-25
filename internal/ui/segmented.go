package ui

// segmented.go is the clickable segmented toggle (#2766): a one-row strip of
// bracketed buttons — "[Source] [Preview]" — that a pane draws inside its own
// body to switch between views of the same content, the way JetBrains shows
// its editor view-mode buttons. The first user is the HTML editor tab's
// Source/Preview/Browser strip (internal/pane); any pane with a small set of
// mutually exclusive views, or an on/off mode beside them, draws the same
// strip and hit-tests it with the same arithmetic, so the buttons look and
// click alike everywhere.
//
// The strip is a pure value: the owner builds it from its current state on
// every View and every click — nothing to keep in sync — and decides what a
// click on a button does. Coordinates are strip-local: x 0 is the strip's
// first cell, which is the pane body's left edge when the strip is drawn
// flush left.

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
)

// Segment is one button of a Segmented strip: its label (drawn in brackets)
// and whether it is on — the view currently shown, or an active mode.
type Segment struct {
	Label string
	On    bool
}

// Segmented is a one-row strip of clickable buttons. Buttons are separated by
// one blank cell; the "on" ones are drawn in the palette's accent style, the
// others faint.
type Segmented struct {
	Segments []Segment
}

// segmentedGap is the blank cell between two buttons.
const segmentedGap = 1

// label is the drawn text of segment i: its label in brackets.
func (s Segmented) label(i int) string { return "[" + s.Segments[i].Label + "]" }

// Width is the cells the buttons take, gaps included — the strip's natural
// width before padding.
func (s Segmented) Width() int {
	w := 0
	for i := range s.Segments {
		if i > 0 {
			w += segmentedGap
		}
		w += ansi.StringWidth(s.label(i))
	}
	return w
}

// View renders the strip flush left and padded (or cut) to exactly width
// cells, so it can stand in for a full row of a pane body. The "on" buttons
// take the palette's accent colour, bold; the others are faint.
func (s Segmented) View(width int, pal *theme.Palette) string {
	if width <= 0 {
		return ""
	}
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	on := lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
	off := lipgloss.NewStyle().Faint(true)
	var b strings.Builder
	used := 0
	for i := range s.Segments {
		lab := s.label(i)
		gap := 0
		if i > 0 {
			gap = segmentedGap
		}
		if used+gap+ansi.StringWidth(lab) > width {
			break // a button that does not fit whole is left out
		}
		b.WriteString(strings.Repeat(" ", gap))
		st := off
		if s.Segments[i].On {
			st = on
		}
		b.WriteString(st.Render(lab))
		used += gap + ansi.StringWidth(lab)
	}
	b.WriteString(strings.Repeat(" ", width-used))
	return b.String()
}

// At returns the index of the button under strip-local column x, or -1 for a
// gap, the padding, or a button View would have left out at width.
func (s Segmented) At(x, width int) int {
	if x < 0 || x >= width {
		return -1
	}
	pos := 0
	for i := range s.Segments {
		if i > 0 {
			pos += segmentedGap
		}
		w := ansi.StringWidth(s.label(i))
		if pos+w > width {
			return -1
		}
		if x >= pos && x < pos+w {
			return i
		}
		pos += w
	}
	return -1
}
