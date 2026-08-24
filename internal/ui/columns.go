package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Result-row bounds shared by the find-in-path overlay and the find-usages
// popup (#2047): both keep a stable body between a floor — the popup never
// collapses onto two matches — and a ceiling, past which the list scrolls
// instead of growing.
const (
	MinResultRows = 11
	MaxResultRows = 40
)

// ClampResultRows bounds a computed result-row count to [MinResultRows,
// MaxResultRows].
func ClampResultRows(n int) int {
	if n < MinResultRows {
		return MinResultRows
	}
	if n > MaxResultRows {
		return MaxResultRows
	}
	return n
}

// JoinColumns lays two blocks of already-truncated rows side by side (#2047):
// the result list on the left, a code preview on the right, separated by rule
// — a single (possibly styled) cell — with one space of air on either side.
//
// Left rows are padded to leftW display cells so the rule forms an unbroken
// vertical line, and the taller block decides the row count; a missing row on
// either side renders blank. It is the shared seam behind the find-in-path
// overlay and the find-usages popup, which both need the same two-column body.
func JoinColumns(left []string, leftW int, rule string, right []string) string {
	n := max(len(left), len(right))
	if n == 0 {
		return ""
	}
	out := make([]string, 0, n)
	for i := range n {
		l := ""
		if i < len(left) {
			l = left[i]
		}
		if gap := leftW - ansi.StringWidth(l); gap > 0 {
			l += strings.Repeat(" ", gap)
		}
		r := ""
		if i < len(right) {
			r = right[i]
		}
		out = append(out, l+" "+rule+" "+r)
	}
	return strings.Join(out, "\n")
}

// PadRows returns rows extended with blank lines to exactly n entries (or cut
// to n when longer), so a popup keeps a stable height whatever it has to show.
func PadRows(rows []string, n int) []string {
	if n < 0 {
		n = 0
	}
	for len(rows) < n {
		rows = append(rows, "")
	}
	return rows[:n]
}
