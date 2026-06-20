package layout

// Rect is an integer cell rectangle: origin (X,Y) with width W and height H.
type Rect struct{ X, Y, W, H int }

// Contains reports whether cell (x,y) lies inside r.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// HitKind classifies what a mouse cell fell on.
type HitKind int

const (
	HitNone    HitKind = iota
	HitDivider         // a split's gutter — start a resize
	HitTitle           // a pane's title-bar row — start a move
	HitPane            // a pane interior below the title bar
)

// Hit is the result of hit-testing a point against a computed Layout.
type Hit struct {
	Kind    HitKind
	Pane    string   // set for HitTitle / HitPane
	Divider *Divider // set for HitDivider (points into the Layout's slice)
}

// TitleBarRows is the height of a pane's title-bar move handle: the top border
// row plus the title text row rendered just inside it. A press anywhere in this
// band starts a move, so grabbing the visible title (not just the 1-pixel
// border) works.
const TitleBarRows = 2

// Hit classifies cell (x,y): a divider takes precedence (gutters never overlap
// pane interiors), then a pane — its top TitleBarRows are the move handle.
func (l *Layout) Hit(x, y int) Hit {
	for i := range l.Dividers {
		if l.Dividers[i].Rect.Contains(x, y) {
			return Hit{Kind: HitDivider, Divider: &l.Dividers[i]}
		}
	}
	for pane, r := range l.Panes {
		if r.Contains(x, y) {
			if y < r.Y+TitleBarRows {
				return Hit{Kind: HitTitle, Pane: pane}
			}
			return Hit{Kind: HitPane, Pane: pane}
		}
	}
	return Hit{Kind: HitNone}
}

// PaneAt returns the pane id whose rectangle contains (x,y), if any.
func (l *Layout) PaneAt(x, y int) (string, bool) {
	for pane, r := range l.Panes {
		if r.Contains(x, y) {
			return pane, true
		}
	}
	return "", false
}

// Zone is a drop region within a target pane, choosing the side the dragged pane
// lands on and therefore the resulting split's orientation.
type Zone int

const (
	ZoneLeft Zone = iota
	ZoneRight
	ZoneTop
	ZoneBottom
)

// DropZone resolves which edge of rect cell (x,y) is nearest, picking the
// horizontal pair (left/right) when the point is closer to a vertical edge and
// the vertical pair (top/bottom) otherwise.
func DropZone(r Rect, x, y int) Zone {
	if r.W <= 0 || r.H <= 0 {
		return ZoneRight
	}
	fx := (float64(x-r.X) + 0.5) / float64(r.W)
	fy := (float64(y-r.Y) + 0.5) / float64(r.H)
	distH := min2(fx, 1-fx) // distance to nearest vertical edge
	distV := min2(fy, 1-fy) // distance to nearest horizontal edge
	if distH <= distV {
		if fx < 0.5 {
			return ZoneLeft
		}
		return ZoneRight
	}
	if fy < 0.5 {
		return ZoneTop
	}
	return ZoneBottom
}

func min2(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
