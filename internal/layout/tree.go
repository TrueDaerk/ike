// Package layout is the pure split-tree model behind IKE's tiled pane layout.
// It owns geometry and structure only: no bubbletea, no I/O. A tree is a binary
// split tree whose leaves are pane ids; Compute walks it to assign each leaf an
// integer cell rectangle that exactly tiles a viewport, reserving one cell for
// every divider. The host (internal/app) holds the mutable drag state, turns
// mouse events into Resize/Move calls on the tree, and renders each leaf into
// its rectangle. state.go converts the tree to/from plain data for persistence.
package layout

import "math"

// Orient is the arrangement of a split's two children.
type Orient int

const (
	// Horizontal lays children side by side (A left, B right); the divider is a
	// one-column vertical gutter between them.
	Horizontal Orient = iota
	// Vertical stacks children (A top, B bottom); the divider is a one-row
	// horizontal gutter between them.
	Vertical
)

// minCell is the smallest interior size (cells) a pane may be resized to along
// the split axis. Resize clamps to it so a pane can never collapse to zero.
const minCell = 4

// Node is a leaf or a split. The interface is closed to *Leaf and *Split; both
// are pointers so the host can hold a stable reference and mutate in place.
type Node interface{ isNode() }

// Leaf is a single pane, identified by its pane id ("explorer", "editor", or any
// plugin pane id).
type Leaf struct{ Pane string }

// Split divides a region between two children at Ratio in (0,1): A receives the
// left/top fraction, B the remainder, with one cell reserved for the divider.
type Split struct {
	Orient Orient
	Ratio  float64
	A, B   Node
}

func (*Leaf) isNode()  {}
func (*Split) isNode() {}

// Default reproduces the historical two-pane layout for the given viewport
// width: a horizontal split with the explorer on the left at roughly
// explorerCols columns and the editor filling the rest.
func Default(width, explorerCols int) Node {
	r := 0.3
	if width > 1 {
		r = float64(explorerCols) / float64(width-1)
	}
	r = clampRatio(r)
	return &Split{Orient: Horizontal, Ratio: r, A: &Leaf{Pane: "explorer"}, B: &Leaf{Pane: "editor"}}
}

// Children splits r into the rectangles for A, the divider gutter, and B.
// It is shared by Compute (geometry) and the host renderer so both agree on the
// exact cell boundaries.
func (s *Split) Children(r Rect) (a, div, b Rect) {
	if s.Orient == Horizontal {
		usable := r.W - 1
		if usable < 0 {
			usable = 0
		}
		aw := clampInt(int(math.Round(s.Ratio*float64(usable))), 0, usable)
		bw := usable - aw
		a = Rect{X: r.X, Y: r.Y, W: aw, H: r.H}
		div = Rect{X: r.X + aw, Y: r.Y, W: 1, H: r.H}
		b = Rect{X: r.X + aw + 1, Y: r.Y, W: bw, H: r.H}
		return a, div, b
	}
	usable := r.H - 1
	if usable < 0 {
		usable = 0
	}
	ah := clampInt(int(math.Round(s.Ratio*float64(usable))), 0, usable)
	bh := usable - ah
	a = Rect{X: r.X, Y: r.Y, W: r.W, H: ah}
	div = Rect{X: r.X, Y: r.Y + ah, W: r.W, H: 1}
	b = Rect{X: r.X, Y: r.Y + ah + 1, W: r.W, H: bh}
	return a, div, b
}

// Divider is a draggable gutter between a split's children, paired with the
// geometry Resize needs: the span (usable cells along the split axis) and its
// start offset so a mouse position maps back to a ratio.
type Divider struct {
	Split  *Split
	Rect   Rect
	Orient Orient
	Start  int // cell offset of the usable span's origin (X if Horizontal, Y if Vertical)
	Span   int // usable cells along the axis, excluding the divider
}

// Layout is the result of Compute: every leaf's rectangle plus the live dividers
// for hit-testing and resizing.
type Layout struct {
	Panes    map[string]Rect
	Dividers []Divider
}

// Compute walks root over viewport vp, assigning each leaf an exact integer
// rectangle and collecting the dividers. Children always tile their parent with
// no gaps or overlap.
func Compute(root Node, vp Rect) Layout {
	l := Layout{Panes: map[string]Rect{}}
	var walk func(n Node, r Rect)
	walk = func(n Node, r Rect) {
		switch t := n.(type) {
		case *Leaf:
			l.Panes[t.Pane] = r
		case *Split:
			a, div, b := t.Children(r)
			start, span := r.X, r.W-1
			if t.Orient == Vertical {
				start, span = r.Y, r.H-1
			}
			if span < 0 {
				span = 0
			}
			l.Dividers = append(l.Dividers, Divider{Split: t, Rect: div, Orient: t.Orient, Start: start, Span: span})
			walk(t.A, a)
			walk(t.B, b)
		}
	}
	walk(root, vp)
	return l
}

// Rects is Compute reduced to the leaf rectangle map, for callers that only need
// pane geometry.
func Rects(root Node, vp Rect) map[string]Rect { return Compute(root, vp).Panes }

// Panes returns the set of pane ids present in the tree.
func Panes(root Node) map[string]bool {
	out := map[string]bool{}
	var walk func(Node)
	walk = func(n Node) {
		switch t := n.(type) {
		case *Leaf:
			out[t.Pane] = true
		case *Split:
			walk(t.A)
			walk(t.B)
		}
	}
	walk(root)
	return out
}

// Leaves returns the leaf pane ids in left-to-right / top-to-bottom walk order
// (A before B at every split), so callers have a stable focus-cycle ordering.
func Leaves(root Node) []string {
	var out []string
	var walk func(Node)
	walk = func(n Node) {
		switch t := n.(type) {
		case *Leaf:
			out = append(out, t.Pane)
		case *Split:
			walk(t.A)
			walk(t.B)
		}
	}
	walk(root)
	return out
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampRatio(r float64) float64 {
	const eps = 0.05
	if r < eps {
		return eps
	}
	if r > 1-eps {
		return 1 - eps
	}
	return r
}
