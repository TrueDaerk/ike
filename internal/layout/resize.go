package layout

import "math"

// ResizeTo updates the owning split's ratio so the children's shared edge
// follows mouse cell (x,y), clamped so neither child drops below minCell along
// the split axis. A span too small to honour both minimums leaves the ratio
// unchanged.
func (d Divider) ResizeTo(x, y int) {
	pos := x
	if d.Orient == Vertical {
		pos = y
	}
	if d.Span < 2*minCell {
		return
	}
	off := clampInt(pos-d.Start, minCell, d.Span-minCell)
	d.Split.Ratio = float64(off) / float64(d.Span)
}

// ResizeStep moves the divider delta cells along its axis (positive = right or
// down), the keyboard counterpart of ResizeTo's mouse drag (#2150). It reads
// the split's current ratio back into a cell offset so repeated single-cell
// steps accumulate exactly, and clamps like ResizeTo so neither child drops
// below minCell.
func (d Divider) ResizeStep(delta int) {
	if d.Span < 2*minCell {
		return
	}
	cur := int(math.Round(d.Split.Ratio * float64(d.Span)))
	off := clampInt(cur+delta, minCell, d.Span-minCell)
	d.Split.Ratio = float64(off) / float64(d.Span)
}

// EdgeDivider picks the divider a keyboard resize of pane moves for direction
// dir (#2150): the pane's trailing edge (right for the horizontal pair,
// bottom for the vertical one) when a split owns it, else its leading edge.
// The caller moves the returned divider with ResizeStep — so "right" always
// means "move that edge one cell right", which grows the pane when it owns
// its trailing edge and shrinks it when it only owns the leading one. Among
// dividers meeting the same edge the innermost (smallest cross extent) wins:
// that is the pane's nearest enclosing split.
func (l Layout) EdgeDivider(pane string, dir Zone) (Divider, bool) {
	r, ok := l.Panes[pane]
	if !ok || r.W <= 0 || r.H <= 0 {
		return Divider{}, false
	}
	orient := Horizontal
	if dir == ZoneTop || dir == ZoneBottom {
		orient = Vertical
	}
	if d, ok := l.edgeDivider(r, orient, true); ok {
		return d, true
	}
	return l.edgeDivider(r, orient, false)
}

// edgeDivider finds the divider band sitting on r's trailing (or leading)
// edge along orient and spanning r completely — the boundary of one of the
// pane's ancestor splits, never a neighbour's internal one.
func (l Layout) edgeDivider(r Rect, orient Orient, trailing bool) (Divider, bool) {
	var best Divider
	found := false
	for _, d := range l.Dividers {
		if d.Orient != orient || d.Rect.W <= 0 || d.Rect.H <= 0 {
			continue
		}
		var on bool
		if orient == Horizontal {
			edge := r.X - 1
			if trailing {
				edge = r.X + r.W - 1
			}
			on = d.Rect.X == edge && d.Rect.Y <= r.Y && d.Rect.Y+d.Rect.H >= r.Y+r.H
		} else {
			edge := r.Y - 1
			if trailing {
				edge = r.Y + r.H - 1
			}
			on = d.Rect.Y == edge && d.Rect.X <= r.X && d.Rect.X+d.Rect.W >= r.X+r.W
		}
		if !on {
			continue
		}
		if !found || crossExtent(d) < crossExtent(best) {
			best, found = d, true
		}
	}
	return best, found
}

// crossExtent is a divider band's length across its own axis: the enclosing
// split's extent, so a smaller one means a nearer ancestor.
func crossExtent(d Divider) int {
	if d.Orient == Horizontal {
		return d.Rect.H
	}
	return d.Rect.W
}
