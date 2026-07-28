package httppane

import (
	"strconv"

	"ike/internal/highlight"
)

// fold.go adds code folding to the response viewer (#1330). The ranges come
// from the body's own language — the same Tree-sitter fold nodes an editor
// buffer uses, resolved through the content type (highlight.FencedFolds) — and
// are stored in *row* coordinates, so everything the viewer already addresses
// by row (search matches, selection, copy) keeps working unchanged.
//
// A collapsed fold hides its body rows from the display only: `visible` is the
// row-index projection the viewport scrolls over, while `rows` stays the whole
// response. That is why copy and search operate on real content regardless of
// fold state — a selection spanning a collapsed fold copies what is inside it,
// and a match hidden in one reveals its fold instead of being skipped.

// foldMarkers are the gutter glyphs of a foldable row: open and collapsed.
const (
	foldOpenGlyph      = "▾"
	foldCollapsedGlyph = "▸"
)

// setFolds records the body's foldable ranges, shifted from body-line
// coordinates into row coordinates, and drops any collapse state (the rows
// belong to a different response now).
func (m *Model) setFolds(folds []highlight.Fold, bodyStart int) {
	m.folds = nil
	m.folded = nil
	for _, f := range folds {
		m.folds = append(m.folds, highlight.Fold{
			HeaderLine: f.HeaderLine + bodyStart,
			EndLine:    f.EndLine + bodyStart,
		})
	}
	m.syncVisible()
}

// syncVisible recomputes the display projection: every row that is not inside
// a collapsed fold, in order. It runs after every change to rows or folds.
func (m *Model) syncVisible() {
	m.visible = m.visible[:0]
	for i := 0; i < len(m.rows); i++ {
		m.visible = append(m.visible, i)
		if end, ok := m.folded[i]; ok && end > i {
			i = end // the body rows of a collapsed fold are not displayed
		}
	}
	m.Scroll(0) // the projection shrank or grew: clamp the viewport
}

// rowAt maps a display index to its row index, -1 out of range.
func (m *Model) rowAt(display int) int {
	if display < 0 || display >= len(m.visible) {
		return -1
	}
	return m.visible[display]
}

// displayOf maps a row index to its display index. A row hidden inside a
// collapsed fold reports the display index of the header hiding it, so the
// viewport can always scroll "to" a row.
func (m *Model) displayOf(row int) int {
	for d, r := range m.visible {
		if r == row {
			return d
		}
		if r > row {
			return max(0, d-1)
		}
	}
	return max(0, len(m.visible)-1)
}

// foldAt returns the fold whose header is row, if any.
func (m *Model) foldAt(row int) (highlight.Fold, bool) {
	for _, f := range m.folds {
		if f.HeaderLine == row {
			return f, true
		}
	}
	return highlight.Fold{}, false
}

// VisibleCount is the number of rows the viewport can scroll over — the whole
// response minus what collapsed folds hide (tests).
func (m *Model) VisibleCount() int { return len(m.visible) }

// RowVisible reports whether row is part of the display projection (tests).
func (m *Model) RowVisible(row int) bool {
	for _, r := range m.visible {
		if r == row {
			return true
		}
	}
	return false
}

// DisplayRow is row's index in the display projection — its y offset from the
// first visible row (tests, mouse coordinate math).
func (m *Model) DisplayRow(row int) int { return m.displayOf(row) }

// ScrollToRow puts row at the top of the viewport, as far as the viewport can
// scroll (tests).
func (m *Model) ScrollToRow(row int) {
	m.top = m.displayOf(row)
	m.Scroll(0)
}

// FoldedAt reports the end row of the collapsed fold headed by row (tests).
func (m *Model) FoldedAt(row int) (int, bool) {
	end, ok := m.folded[row]
	return end, ok
}

// Foldable reports whether row heads a foldable range (tests, mouse gutter).
func (m *Model) Foldable(row int) bool {
	_, ok := m.foldAt(row)
	return ok
}

// hiddenRow reports whether row sits inside a collapsed fold's body.
func (m *Model) hiddenRow(row int) bool {
	for h, e := range m.folded {
		if row > h && row <= e {
			return true
		}
	}
	return false
}

// collapse folds the range headed by row; no-op when it is not foldable.
func (m *Model) collapse(row int) bool {
	f, ok := m.foldAt(row)
	if !ok || m.hiddenRow(row) {
		return false
	}
	if m.folded == nil {
		m.folded = map[int]int{}
	}
	m.folded[f.HeaderLine] = f.EndLine
	m.syncVisible()
	return true
}

// expand opens the fold headed by row; no-op when it is not collapsed.
func (m *Model) expand(row int) bool {
	if _, ok := m.folded[row]; !ok {
		return false
	}
	delete(m.folded, row)
	m.syncVisible()
	return true
}

// ToggleFold collapses or expands the fold headed by row (mouse gutter click,
// za). Reports whether anything changed.
func (m *Model) ToggleFold(row int) bool {
	if _, collapsed := m.folded[row]; collapsed {
		return m.expand(row)
	}
	return m.collapse(row)
}

// targetFold picks the fold the keyboard commands act on. The viewer is a
// scroller without a cursor, so the target is "the fold at the top of the
// view": the innermost fold containing the top visible row, and failing that
// the first fold header below it. Repeated zc therefore walks outward as inner
// folds collapse and the viewport rides up. -1 when no fold is left below the
// viewport top; the mouse gutter is the way to hit one fold in particular.
func (m *Model) targetFold() int {
	top := m.rowAt(m.top)
	if top < 0 {
		return -1
	}
	best := -1
	for _, f := range m.folds {
		if f.HeaderLine <= top && top <= f.EndLine {
			if best < 0 || f.HeaderLine > best {
				best = f.HeaderLine // innermost containing fold
			}
		}
	}
	if best >= 0 {
		return best
	}
	next := -1
	for _, f := range m.folds {
		if f.HeaderLine > top && (next < 0 || f.HeaderLine < next) {
			next = f.HeaderLine
		}
	}
	return next
}

// FoldAll collapses every fold, outermost first — the viewer's zM.
func (m *Model) FoldAll() {
	if m.folded == nil {
		m.folded = map[int]int{}
	}
	for _, f := range m.folds {
		if !m.hiddenRow(f.HeaderLine) {
			m.folded[f.HeaderLine] = f.EndLine
		}
	}
	m.syncVisible()
}

// UnfoldAll expands everything — the viewer's zR.
func (m *Model) UnfoldAll() {
	m.folded = nil
	m.syncVisible()
}

// reveal opens every collapsed fold hiding row, so a search match inside one
// becomes visible instead of being silently skipped (#1330).
func (m *Model) reveal(row int) {
	for m.hiddenRow(row) {
		opened := false
		for h, e := range m.folded {
			if row > h && row <= e {
				delete(m.folded, h)
				opened = true
			}
		}
		if !opened {
			break
		}
	}
	m.syncVisible()
}

// foldGutter is the marker cell drawn left of a row: a glyph for a fold
// header, a space otherwise. It occupies the leading gutter column the rows
// already reserve, so nothing shifts.
func (m *Model) foldGutter(row int) string {
	if _, collapsed := m.folded[row]; collapsed {
		return foldCollapsedGlyph
	}
	if m.Foldable(row) {
		return foldOpenGlyph
	}
	return " "
}

// foldPlaceholder is the "⋯ N lines" suffix a collapsed header carries, the
// editor's wording (#144).
func (m *Model) foldPlaceholder(row int) string {
	end, ok := m.folded[row]
	if !ok {
		return ""
	}
	return " ⋯ " + strconv.Itoa(end-row) + " lines"
}

// foldKey runs the second key of a "z" fold command, the editor's set (#144)
// minus the cursor-relative ones the viewer cannot express: za toggles, zc
// collapses, zo opens, zM collapses everything, zR opens everything. Anything
// else cancels the sequence.
func (m *Model) foldKey(key string) {
	switch key {
	case "M":
		m.FoldAll()
	case "R":
		m.UnfoldAll()
	case "a", "c", "o":
		row := m.targetFold()
		if row < 0 {
			return
		}
		switch key {
		case "a":
			m.ToggleFold(row)
		case "c":
			m.collapse(row)
		case "o":
			m.expand(row)
		}
		// Keep the acted-on header on screen: collapsing the fold the view
		// starts inside would otherwise scroll its content away silently.
		if d := m.displayOf(row); d < m.top {
			m.top = d
		}
	}
}
