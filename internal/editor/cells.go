package editor

import (
	"github.com/charmbracelet/x/ansi"

	"ike/internal/unihint"
)

// cells.go — the editor's cell layout (#2526): how many terminal cells each
// buffer column of a line occupies.
//
// The render loop, the mouse map (displayClickCol), overlay anchoring
// (DisplayOffset), soft wrap and horizontal follow-scroll all used to assume
// one buffer rune is one display cell (tabs aside). That broke on anything a
// terminal draws wider or narrower than one rune: an emoji or CJK glyph takes
// two cells, so everything after it drifted one column right per glyph; an
// emoji ZWJ sequence (🤷🏼‍♂️ is five runes) got torn into a base glyph, a lone
// skin-tone swatch, a ∅ joiner placeholder and a separate ♂ — even in a
// terminal that renders the joined glyph perfectly on its own.
//
// lineCells replaces the assumption with a per-column width table computed
// from grapheme clusters (the same ansi.FirstGraphemeCluster walk the terminal
// pane's cursor overlay uses, #1865): the first column of a cluster carries
// the cluster's whole width, the columns it absorbs carry 0 and render
// nothing. Every consumer walks the same table, so what the loop draws, where
// a click lands and where the caret is anchored stay in agreement.
//
// Two things deliberately stay one-rune-one-cell: control runes (#1469) and
// the invisible/format runes unihint replaces with a placeholder (#1654) — a
// stray zero-width joiner must still show as ∅, so a cluster containing one
// outside a legitimate joining context falls apart into single-rune cells.
// Cursor motion is untouched: h/l still step by rune, so the caret can sit on
// an absorbed column; it then renders on the cluster it belongs to.

// cellWidths holds the display width of every rune column of a line, or is
// nil for a line where every rune is one cell (tabs aside) — the pure-ASCII
// fast path, which is the overwhelmingly common case and must cost nothing.
type cellWidths []int

// lineCells computes the cell layout of runes. Tabs report tabWidth through
// at; the table itself stores them as 1 so a caller may still special-case
// them by rune.
func (m Model) lineCells(runes []rune) cellWidths {
	if asciiRunes(runes) {
		return nil
	}
	w := make(cellWidths, len(runes))
	s := string(runes)
	col := 0
	for len(s) > 0 {
		cluster, cw := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		s = s[len(cluster):]
		n := 0
		for range cluster {
			n++
		}
		if n == 1 || !cleanCluster(runes, col, col+n) {
			// A single rune, or a cluster carrying a control/placeholder rune:
			// one cell per rune. A lone wide rune keeps its width; a lone
			// combining mark (nothing to combine with) still gets a cell so
			// it never renders as nothing.
			for i := col; i < col+n; i++ {
				w[i] = 1
			}
			if n == 1 && cw > 1 && !isCtrlRune(runes[col]) {
				if _, ok := unihint.Placeholder(runes[col]); !ok {
					w[col] = cw
				}
			}
		} else {
			if cw < 1 {
				cw = 1
			}
			w[col] = cw
		}
		col += n
	}
	return w
}

// cleanCluster reports whether the multi-rune cluster runes[start:end] may
// render as one glyph: no control rune, and no placeholder rune except a
// joiner sitting in a legitimate joining context (unihint.JoiningContext).
func cleanCluster(runes []rune, start, end int) bool {
	for i := start; i < end; i++ {
		r := runes[i]
		if isCtrlRune(r) {
			return false
		}
		if _, ok := unihint.Placeholder(r); !ok {
			continue
		}
		if (r == 0x200C || r == 0x200D) && unihint.JoiningContext(runes, i, i+1) {
			continue
		}
		return false
	}
	return true
}

// at returns the display width of buffer column col: tabWidth for a tab, 1
// past the line end (padding), 0 for a column absorbed into the cluster
// starting before it, else the cluster's width.
func (w cellWidths) at(runes []rune, col, tabWidth int) int {
	if col >= len(runes) {
		return 1
	}
	if runes[col] == '\t' {
		return tabWidth
	}
	if w == nil {
		return 1
	}
	return w[col]
}

// head returns the first column of the cluster containing col — col itself
// unless it is an absorbed column. Columns past the line end are their own.
func (w cellWidths) head(runes []rune, col int) int {
	if w == nil || col >= len(runes) {
		return col
	}
	for col > 0 && w[col] == 0 {
		col--
	}
	return col
}

// clusterEnd returns one past the last column of the cluster starting at col:
// the next column with a width of its own, or the line end.
func (w cellWidths) clusterEnd(runes []rune, col int) int {
	end := col + 1
	if w == nil {
		return end
	}
	for end < len(runes) && w[end] == 0 {
		end++
	}
	return end
}

// prefix returns the display-cell prefix sums of the line (entry i is the
// cell offset of buffer column i; length = rune count + 1) — the shape
// viewport.WrapSegmentsDisplay and the follow-scroll fix consume.
func (w cellWidths) prefix(runes []rune, tabWidth int) []int {
	p := make([]int, len(runes)+1)
	for c := range runes {
		p[c+1] = p[c] + w.at(runes, c, tabWidth)
	}
	return p
}

// asciiRunes reports whether every rune is ASCII — the layout fast path.
func asciiRunes(runes []rune) bool {
	for _, r := range runes {
		if r >= 0x80 {
			return false
		}
	}
	return true
}
