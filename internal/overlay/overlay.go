// Package overlay is pure string→string compositing for floating panes: it
// splices a content box centered on top of a base canvas while preserving the
// ANSI styling on both sides of the seam. It holds no bubbletea state and knows
// nothing about what it renders — the stateful shell lives in internal/ui.
package overlay

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Center composites the floating pane top centered over base, a canvas w
// columns by h rows. base is returned unchanged when top is empty or does not
// fit the canvas.
func Center(base, top string, w, h int) string {
	tw, th := measure(top)
	if top == "" || tw > w || th > h {
		return base
	}
	return Place(base, top, (w-tw)/2, (h-th)/2, w, h)
}

// Place composites top onto base, a canvas w columns by h rows, with top's
// upper-left corner at cell (x,y). Each overlaid row is spliced into the base
// row by visual column, preserving the ANSI styling on both sides of the seam.
// Rows or columns of top that fall outside the canvas are clipped. base is
// returned unchanged when top is empty.
func Place(base, top string, x, y, w, h int) string {
	if top == "" {
		return base
	}
	topLines := strings.Split(top, "\n")
	baseLines := strings.Split(base, "\n")
	// Pad the canvas to h rows so the splice math is stable.
	for len(baseLines) < h {
		baseLines = append(baseLines, "")
	}
	for i, tl := range topLines {
		row := y + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		baseLines[row] = spliceLine(baseLines[row], tl, x, ansi.StringWidth(tl))
	}
	return strings.Join(baseLines, "\n")
}

// measure returns the visual width and line count of s.
func measure(s string) (w, h int) {
	if s == "" {
		return 0, 0
	}
	lines := strings.Split(s, "\n")
	for _, l := range lines {
		if lw := ansi.StringWidth(l); lw > w {
			w = lw
		}
	}
	return w, len(lines)
}

// spliceLine overwrites line with top (visual width topW) starting at visual
// column x, keeping the styled remainder of line to the right. Short base lines
// are left-padded to x with spaces.
func spliceLine(line, top string, x, topW int) string {
	left := ansi.Truncate(line, x, "")
	if pad := x - ansi.StringWidth(left); pad > 0 {
		left += strings.Repeat(" ", pad)
	}
	right := ansi.TruncateLeft(line, x+topW, "")
	return left + "\x1b[0m" + top + "\x1b[0m" + right
}
