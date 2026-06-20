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
// columns by h rows. Each overlaid row is spliced into the base row by visual
// column, preserving the ANSI styling on both sides of the pane. base is
// returned unchanged when top is empty or does not fit the canvas.
func Center(base, top string, w, h int) string {
	if top == "" {
		return base
	}
	topLines := strings.Split(top, "\n")
	topH := len(topLines)
	topW := 0
	for _, l := range topLines {
		if lw := ansi.StringWidth(l); lw > topW {
			topW = lw
		}
	}
	if topW > w || topH > h {
		return base // does not fit; leave the base untouched
	}

	baseLines := strings.Split(base, "\n")
	// Pad the canvas to h rows so centering math is stable.
	for len(baseLines) < h {
		baseLines = append(baseLines, "")
	}
	x := (w - topW) / 2
	y := (h - topH) / 2

	for i, tl := range topLines {
		row := y + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		baseLines[row] = spliceLine(baseLines[row], tl, x, topW)
	}
	return strings.Join(baseLines, "\n")
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
