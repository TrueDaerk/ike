package ui

// listpane.go is the rendering half of the list-pane layer (#2462), the
// counterpart to listnav.go (keyboard) and listmouse.go (pointer). The tool
// panels — problems, usages, dependencies, timeline, archives, remote, the
// two doctors, the GitHub issues list — draw the same shape: a header line, a
// second line (a filter row or a status line), a fixed-height window of rows
// padded out with blanks, and a footer of key hints. Each of them had written
// that shape out by hand.

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"ike/internal/theme"
)

// RenderWindow draws the height-row window starting at top over a list of n
// rows, one line per row plus a trailing newline, so the block always has
// exactly height lines whatever the list length. An empty list renders the
// empty notice instead, padded to the same height; pass "" for a pane that
// has no notice.
func RenderWindow(top, height, n int, empty string, row func(i int) string) string {
	if height < 0 {
		height = 0
	}
	if n <= 0 {
		return empty + strings.Repeat("\n", height)
	}
	var b strings.Builder
	for k := 0; k < height; k++ {
		if i := top + k; i >= 0 && i < n {
			b.WriteString(row(i))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ListPaneView composes the four blocks of a list pane's View: the header
// line, the second line (filter row or status line), the row window — which
// already ends in a newline per row — and the footer.
func ListPaneView(header, second, rows, footer string) string {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(second)
	b.WriteString("\n")
	b.WriteString(rows)
	b.WriteString(footer)
	return b.String()
}

// FileHeader is the header line of a pane bound to one file: the file's base
// name, accented and bold while the pane holds the focus, followed by the
// row count in parentheses. An empty path renders "(no file)".
func FileHeader(pal *theme.Palette, path string, count int, focused bool) string {
	name := "(no file)"
	if path != "" {
		name = BaseName(path)
	}
	s := lipgloss.NewStyle().Foreground(pal.Secondary)
	if focused {
		s = lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
	}
	return " " + s.Render(name) + " " + lipgloss.NewStyle().Faint(true).Render("("+strconv.Itoa(count)+")")
}

// BaseName is the last path segment, cutting at either separator so a Windows
// path taken from a language server reads the same as a POSIX one.
func BaseName(p string) string {
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// TargetRow resolves row i to the row an action acts on: a group header
// stands for the first entry beneath it, every other row for itself. ok is
// false past the list's ends and on a header with no entry under it.
func TargetRow[T any](rows []T, i int, isHeader func(T) bool) (T, bool) {
	var zero T
	if i < 0 || i >= len(rows) {
		return zero, false
	}
	r := rows[i]
	if isHeader(r) {
		if i+1 >= len(rows) || isHeader(rows[i+1]) {
			return zero, false
		}
		r = rows[i+1]
	}
	return r, true
}

// FilterStepper is the part of a pane's filter row that StepFiltered needs:
// whether the search is open, and how a step outcome is reported back to the
// user. internal/filterbar.Model satisfies it; the interface keeps the
// dependency pointing that way, since filterbar builds on this package.
type FilterStepper interface {
	Active() bool
	ShowStep(MatchStep) MatchStep
}

// StepFiltered walks the cursor to the next matching row in direction delta,
// skipping the rows keep rejects (the group headers, which are chrome rather
// than results), scrolls the window onto it and reports the outcome through
// the filter row. A pane whose search is closed does not answer the chord.
func StepFiltered(f FilterStepper, cursor, top *int, n, height, delta int, keep func(i int) bool) MatchStep {
	if !f.Active() {
		return NoStep
	}
	next, st := StepOver(*cursor, n, delta, keep)
	*cursor = next
	ClampWindow(cursor, top, n, height)
	return f.ShowStep(st)
}
