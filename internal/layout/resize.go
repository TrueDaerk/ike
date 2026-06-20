package layout

// ResizeTo updates the owning split's ratio so its divider follows mouse cell
// (x,y), clamped so neither child drops below minCell along the split axis. A
// span too small to honour both minimums leaves the ratio unchanged.
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
