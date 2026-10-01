package tracepanel

// snake.go is the graph view's layout (#2858), pure over widths so it is
// testable without a terminal: the stops of a change path become boxes laid
// out left-to-right from the top-left; when the next box does not fit the
// pane width the path turns down and continues right-to-left, at
// the left edge it turns down again (boustrophedon). The first box of a row
// sits directly under the last box of the row before, so the turn is a
// vertical connector ("│" over "▼") in the rows between. A break (a new
// question: every prompt but the first, #2866) forces a turn even when the
// box would still fit, with a turn rule between the rows. A pane narrower
// than two boxes falls back to a single column. The renderer and the mouse
// hit test both read the same Layout, so a click lands where the box was
// drawn.

const (
	// boxH is the rows of a box: border with the kind glyph, label, detail,
	// closed bottom border (#2872).
	boxH = 4
	// gapW is the cells between two boxes on a row: the "──▶" connector.
	gapW = 3
	// turnH is the rows between two path rows: the "│" and "▼" of a turn,
	// room enough that consecutive rows read apart.
	turnH = 2
	// breakH is the rows between two path rows at a break: the "│", the
	// turn rule ("── #2 ──…") and the "▼".
	breakH = 3
	// minBoxW, prefBoxW and maxBoxW bound the box width: as many boxes per
	// row as keep them at least prefBoxW wide, else at least minBoxW; a
	// pane that cannot hold two minBoxW boxes is a single column.
	minBoxW  = 18
	prefBoxW = 22
	maxBoxW  = 28
	// sepW is the width of a separator marker's slot.
	sepW = 3
)

// Slot is the cell rectangle of one stop on the pane's content grid (y
// counts from the first content row, before scrolling).
type Slot struct {
	X, Y, W, H int
	// Row is the path row the slot lies on; Dir its direction (+1
	// left-to-right, -1 right-to-left).
	Row, Dir int
	// Break reports that the slot starts its row because of a forced
	// break; the turn rule sits on row Y-2.
	Break bool
}

// CenterX is the column of the slot's middle, where a turn connector sits.
func (s Slot) CenterX() int { return s.X + s.W/2 }

// Layout is the placed path.
type Layout struct {
	Slots []Slot
	// BoxW is the width of a (non-separator) box; Single reports the
	// vertical single-column fallback.
	BoxW   int
	Single bool
	// Height is the content rows the layout spans.
	Height int
	// DetailY and DetailH place the expanded detail block beneath its box
	// row; DetailH is 0 when nothing is expanded.
	DetailY, DetailH int
}

// BoxWidth picks the box width for a pane width: the largest count of
// boxes per row that keeps every box at least prefBoxW wide (capped at
// maxBoxW), else the largest count that keeps minBoxW; single is true when
// not even two minBoxW boxes fit, in which case w is the pane width.
func BoxWidth(paneW int) (w int, single bool) {
	if paneW < 2*minBoxW+gapW {
		return max(paneW, 1), true
	}
	best := 0
	for k := 2; ; k++ {
		bw := (paneW - gapW*(k-1)) / k
		if bw < minBoxW {
			break
		}
		if bw >= prefBoxW || best == 0 {
			best = bw
		}
	}
	return min(best, maxBoxW), false
}

// Snake lays out n stops of the given widths (a stop's width is BoxW, a
// separator's sepW) into a pane paneW cells wide. breaks are the indices
// of the stops that start a new row (a new question); a break at index 0
// is ignored. expanded is the index of the expanded stop (-1 for none) and
// detailH the rows its detail block needs beneath its row.
func Snake(widths []int, breaks []int, paneW, expanded, detailH int) Layout {
	boxW, single := BoxWidth(paneW)
	l := Layout{BoxW: boxW, Single: single, DetailY: -1}
	if expanded < 0 || expanded >= len(widths) {
		detailH = 0
	}
	l.Slots = make([]Slot, len(widths))
	brk := make(map[int]bool, len(breaks))
	for _, b := range breaks {
		if b > 0 {
			brk[b] = true
		}
	}
	gap := func(i int) int {
		if brk[i] {
			return breakH
		}
		return turnH
	}
	y, row, dir := 0, 0, 1
	rowDetail := 0 // the detail rows the current row carries
	for i, w := range widths {
		if single {
			w = min(w, paneW)
			if i > 0 {
				y += boxH + rowDetail + gap(i)
				rowDetail = 0
			}
			l.Slots[i] = Slot{X: 0, Y: y, W: w, H: boxH, Row: i, Dir: 1, Break: brk[i]}
		} else {
			var x int
			switch {
			case i == 0:
				x = 0
			case !brk[i] && dir > 0 && l.Slots[i-1].X+l.Slots[i-1].W+gapW+w <= paneW:
				x = l.Slots[i-1].X + l.Slots[i-1].W + gapW
			case !brk[i] && dir < 0 && l.Slots[i-1].X-gapW-w >= 0:
				x = l.Slots[i-1].X - gapW - w
			default:
				// Turn (the row is full, or a break forces it): the next row
				// starts under the last box, sharing the edge the path came
				// from.
				y += boxH + rowDetail + gap(i)
				rowDetail = 0
				row++
				dir = -dir
				last := l.Slots[i-1]
				if dir < 0 {
					x = last.X + last.W - w
				} else {
					x = last.X
				}
				x = max(0, min(x, paneW-w))
			}
			l.Slots[i] = Slot{X: x, Y: y, W: w, H: boxH, Row: row, Dir: dir, Break: brk[i]}
		}
		if i == expanded {
			rowDetail = detailH
			l.DetailY, l.DetailH = y+boxH, detailH
		}
	}
	if len(widths) > 0 {
		l.Height = y + boxH + rowDetail
	}
	return l
}

// At returns the index of the slot containing content cell (x, y), -1 when
// none does.
func (l Layout) At(x, y int) int {
	for i, s := range l.Slots {
		if x >= s.X && x < s.X+s.W && y >= s.Y && y < s.Y+s.H {
			return i
		}
	}
	return -1
}

// Below returns the index of the slot on the row after slot i whose centre
// is nearest to slot i's, -1 on the last row. ok filters the candidates.
func (l Layout) Below(i int, ok func(int) bool) int { return l.neighbour(i, 1, ok) }

// Above is Below's counterpart for the row before.
func (l Layout) Above(i int, ok func(int) bool) int { return l.neighbour(i, -1, ok) }

func (l Layout) neighbour(i, step int, ok func(int) bool) int {
	if i < 0 || i >= len(l.Slots) {
		return -1
	}
	cur := l.Slots[i]
	best, bestD := -1, 0
	for j, s := range l.Slots {
		if s.Row != cur.Row+step || (ok != nil && !ok(j)) {
			continue
		}
		d := s.CenterX() - cur.CenterX()
		if d < 0 {
			d = -d
		}
		if best < 0 || d < bestD {
			best, bestD = j, d
		}
	}
	return best
}
