package layout

import (
	"fmt"
	"sort"
)

// slots.go implements named slot templates (#1897): an ASCII grid template
// (in the spirit of CSS grid-template-areas) declares named positions for
// tool windows around a reserved editor region. Each slot is a rune in the
// grid; the cells of one rune must form a solid rectangle. The template is
// decomposed into a binary split tree by guillotine cuts, so it can be
// materialized with the ordinary Split/Leaf machinery: ratios come from the
// template's cell fractions, and slots whose tool is closed simply drop out
// of the tree — their sibling absorbs the space, which is exactly the
// collapse behavior the host wants (opening tool T of template "XEEH/XEEH/
// TTZZ" while Z is closed gives T the full bottom strip; opening Z splits
// the strip back into the defined TT/ZZ arrangement).

// EditorSlot is the reserved slot name for the editor region: the template
// cell(s) where the host grafts the live editor subtree instead of a tool
// pane. It is always treated as open.
const EditorSlot = "E"

// Template is a parsed slot template: the grid dimensions, each slot's cell
// rectangle, and the slot tree derived by guillotine decomposition (a Node
// whose leaves are slot names, EditorSlot included).
type Template struct {
	Cols, Rows int
	rects      map[string]Rect
	tree       Node
}

// ParseTemplate parses the grid rows of a slot template. Every row must have
// the same number of cells, each cell names a slot by a single non-space
// rune, each slot's cells must form a solid rectangle, the EditorSlot region
// must be present, and the arrangement must decompose into straight
// full-width/full-height cuts (a slicing layout — a pinwheel of four slots
// around a center cannot be expressed as a split tree and is rejected). The
// error text is written for config diagnostics.
func ParseTemplate(rows []string) (*Template, error) {
	var grid [][]rune
	for _, row := range rows {
		if row == "" {
			continue
		}
		grid = append(grid, []rune(row))
	}
	if len(grid) == 0 {
		return nil, fmt.Errorf("template is empty")
	}
	cols := len(grid[0])
	for _, row := range grid {
		if len(row) != cols {
			return nil, fmt.Errorf("template rows must all have the same width")
		}
	}
	rects := map[string]Rect{}
	counts := map[string]int{}
	for y, row := range grid {
		for x, r := range row {
			if r == ' ' || r == '\t' {
				return nil, fmt.Errorf("template cell (%d,%d) is blank; every cell must name a slot", x, y)
			}
			s := string(r)
			counts[s]++
			bb, seen := rects[s]
			if !seen {
				rects[s] = Rect{X: x, Y: y, W: 1, H: 1}
				continue
			}
			// Grow the bounding box; solidity is checked afterwards.
			x0, y0 := bb.X, bb.Y
			x1, y1 := bb.X+bb.W, bb.Y+bb.H
			if x < x0 {
				x0 = x
			}
			if y < y0 {
				y0 = y
			}
			if x+1 > x1 {
				x1 = x + 1
			}
			if y+1 > y1 {
				y1 = y + 1
			}
			rects[s] = Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
		}
	}
	for s, bb := range rects {
		if counts[s] != bb.W*bb.H {
			return nil, fmt.Errorf("slot %q does not form a solid rectangle", s)
		}
	}
	if _, ok := rects[EditorSlot]; !ok {
		return nil, fmt.Errorf("template has no editor region (%q)", EditorSlot)
	}
	names := make([]string, 0, len(rects))
	for s := range rects {
		names = append(names, s)
	}
	sort.Strings(names)
	tree, err := slotTree(Rect{W: cols, H: len(grid)}, names, rects)
	if err != nil {
		return nil, err
	}
	return &Template{Cols: cols, Rows: len(grid), rects: rects, tree: tree}, nil
}

// slotTree recursively decomposes region (holding the named slots) by the
// first straight cut that severs no slot rectangle: vertical cuts are tried
// left to right, then horizontal cuts top to bottom, so the tree shape is
// deterministic. Alternative valid cuts of the same template yield different
// trees but identical geometry, since every ratio is the cut's cell
// fraction.
func slotTree(region Rect, names []string, rects map[string]Rect) (Node, error) {
	if len(names) == 1 {
		return &Leaf{Pane: names[0]}, nil
	}
	for x := region.X + 1; x < region.X+region.W; x++ {
		a, b, ok := severed(names, rects, func(r Rect) int {
			switch {
			case r.X+r.W <= x:
				return -1
			case r.X >= x:
				return 1
			}
			return 0
		})
		if !ok {
			continue
		}
		left := Rect{X: region.X, Y: region.Y, W: x - region.X, H: region.H}
		right := Rect{X: x, Y: region.Y, W: region.X + region.W - x, H: region.H}
		return cutNode(Horizontal, float64(left.W)/float64(region.W), left, right, a, b, rects)
	}
	for y := region.Y + 1; y < region.Y+region.H; y++ {
		a, b, ok := severed(names, rects, func(r Rect) int {
			switch {
			case r.Y+r.H <= y:
				return -1
			case r.Y >= y:
				return 1
			}
			return 0
		})
		if !ok {
			continue
		}
		top := Rect{X: region.X, Y: region.Y, W: region.W, H: y - region.Y}
		bottom := Rect{X: region.X, Y: y, W: region.W, H: region.Y + region.H - y}
		return cutNode(Vertical, float64(top.H)/float64(region.H), top, bottom, a, b, rects)
	}
	return nil, fmt.Errorf("template cannot be split into straight cuts (slots %v interlock)", names)
}

// severed partitions the named slots by side(rect): -1 groups into a, +1
// into b; 0 means the candidate cut slices through the rect, invalidating
// the cut.
func severed(names []string, rects map[string]Rect, side func(Rect) int) (a, b []string, ok bool) {
	for _, s := range names {
		switch side(rects[s]) {
		case -1:
			a = append(a, s)
		case 1:
			b = append(b, s)
		default:
			return nil, nil, false
		}
	}
	return a, b, true
}

// cutNode builds the split for one accepted cut and recurses into both
// halves.
func cutNode(o Orient, ratio float64, ra, rb Rect, a, b []string, rects map[string]Rect) (Node, error) {
	na, err := slotTree(ra, a, rects)
	if err != nil {
		return nil, err
	}
	nb, err := slotTree(rb, b, rects)
	if err != nil {
		return nil, err
	}
	return &Split{Orient: o, Ratio: clampRatio(ratio), A: na, B: nb}, nil
}

// HasSlot reports whether the template defines the named slot (EditorSlot
// included).
func (t *Template) HasSlot(slot string) bool {
	_, ok := t.rects[slot]
	return ok
}

// SlotNames returns the tool slot names in sorted order, excluding the
// editor region.
func (t *Template) SlotNames() []string {
	var out []string
	for s := range t.rects {
		if s != EditorSlot {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// SlotRect returns the slot's cell rectangle in the template grid. The host
// uses the aspect to pick the split direction when a second, non-tabbable
// occupant must share a slot (wider slots split side by side, taller ones
// stack).
func (t *Template) SlotRect(slot string) (Rect, bool) {
	r, ok := t.rects[slot]
	return r, ok
}

// BuildTree materializes the template for the currently open slots:
// residents maps slot names to the pane ids occupying them, editor is the
// subtree filling the EditorSlot region (the live editor area, splits and
// all). Slots absent from residents are pruned, their space absorbed by the
// surviving sibling — the graceful-collapse half of #1897. Ratios are the
// template's cell fractions; the returned tree shares no structure with the
// template (leaves are fresh, editor is grafted as passed).
func (t *Template) BuildTree(residents map[string]string, editor Node) Node {
	out := substitute(t.tree, residents, editor)
	if out == nil {
		return editor
	}
	return out
}

// substitute clones the slot tree, mapping slot leaves to resident pane
// leaves (nil when the slot is closed) and the editor leaf to the given
// subtree; a split with one surviving child collapses into it.
func substitute(n Node, residents map[string]string, editor Node) Node {
	switch t := n.(type) {
	case *Leaf:
		if t.Pane == EditorSlot {
			return editor
		}
		if key := residents[t.Pane]; key != "" {
			return &Leaf{Pane: key}
		}
		return nil
	case *Split:
		a := substitute(t.A, residents, editor)
		b := substitute(t.B, residents, editor)
		switch {
		case a != nil && b != nil:
			return &Split{Orient: t.Orient, Ratio: t.Ratio, A: a, B: b}
		case a != nil:
			return a
		case b != nil:
			return b
		}
	}
	return nil
}

// RemoveLeaves detaches every leaf whose pane id is in drop, each parent
// split collapsing into the sibling — Close generalized to a set. It is how
// the host peels the slotted panes off the live tree to recover the bare
// editor subtree before rebuilding (#1897). ok=false when the removals
// would empty the tree; the tree may be partially mutated then, so callers
// wanting atomicity pass a Clone.
func RemoveLeaves(root Node, drop map[string]bool) (Node, bool) {
	if root == nil {
		return nil, false
	}
	for _, id := range Leaves(root) {
		if !drop[id] {
			continue
		}
		pruned, _, ok := remove(root, id)
		if !ok {
			return root, false
		}
		root = pruned
	}
	return root, true
}
