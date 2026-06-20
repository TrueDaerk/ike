package ui

// Box chrome and default bounds for the floating shell.
const (
	borderH = 2 // left + right border columns
	borderV = 2 // top + bottom border rows
	padH    = 3 // horizontal padding each side
	padV    = 1 // vertical padding each side

	// frameH/frameV are the total non-content chrome the box adds around its
	// inner content (border + padding on both axes).
	frameH = borderH + 2*padH
	frameV = borderV + 2*padV

	// titleRow is the heading line reserved at the top of the inner content.
	titleRow = 1

	// defaultMargin is the gap kept between the pane and each terminal edge so
	// the floating box never bleeds to the very border.
	defaultMargin = 2
)

// budget reports the width and height available to the hosted content, after
// removing the margin, box chrome, the title row, and any optional max
// width/height fraction. Both dimensions are floored at 1.
func budget(termW, termH, margin int, maxWFrac, maxHFrac float64) (w, h int) {
	if margin < 0 {
		margin = 0
	}
	w = termW - 2*margin - frameH
	h = termH - 2*margin - frameV - titleRow

	if maxWFrac > 0 {
		if c := int(float64(termW)*maxWFrac) - frameH; c < w {
			w = c
		}
	}
	if maxHFrac > 0 {
		if c := int(float64(termH)*maxHFrac) - frameV - titleRow; c < h {
			h = c
		}
	}

	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}
