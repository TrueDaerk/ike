package layout

import "encoding/json"

// nodeData is the plain serialised form of a Node. A node is a leaf when Pane is
// set, otherwise a split with two children. Kept separate from Node so the wire
// format is stable and tolerant of unknown shapes.
type nodeData struct {
	Pane   string    `json:"pane,omitempty"`
	Orient string    `json:"orient,omitempty"`
	Ratio  float64   `json:"ratio,omitempty"`
	A      *nodeData `json:"a,omitempty"`
	B      *nodeData `json:"b,omitempty"`
}

// Encode serialises root to plain JSON bytes for persistence.
func Encode(root Node) ([]byte, error) {
	return json.Marshal(toData(root))
}

func toData(n Node) *nodeData {
	switch t := n.(type) {
	case *Leaf:
		return &nodeData{Pane: t.Pane}
	case *Split:
		o := "h"
		if t.Orient == Vertical {
			o = "v"
		}
		return &nodeData{Orient: o, Ratio: t.Ratio, A: toData(t.A), B: toData(t.B)}
	}
	return nil
}

// Decode parses data into a tree, then validates it against the live pane set.
// The decoded tree is accepted only when its leaves are exactly valid (same ids,
// no duplicates, none missing); any mismatch, malformed structure, or unknown
// pane id returns ok=false so the caller falls back to the default layout. A
// stale saved tree therefore never hides a pane or crashes.
func Decode(data []byte, valid map[string]bool) (root Node, ok bool) {
	var d nodeData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, false
	}
	node, structOK := fromData(&d)
	if !structOK {
		return nil, false
	}
	seen := map[string]bool{}
	dup := false
	var walk func(Node)
	walk = func(n Node) {
		if l, isLeaf := n.(*Leaf); isLeaf {
			if seen[l.Pane] {
				dup = true
			}
			seen[l.Pane] = true
		} else if s, isSplit := n.(*Split); isSplit {
			walk(s.A)
			walk(s.B)
		}
	}
	walk(node)
	if dup || len(seen) != len(valid) {
		return nil, false
	}
	for p := range seen {
		if !valid[p] {
			return nil, false
		}
	}
	return node, true
}

// fromData rebuilds a Node, rejecting malformed shapes (a node that is neither a
// well-formed leaf nor a split with two valid children).
func fromData(d *nodeData) (Node, bool) {
	if d == nil {
		return nil, false
	}
	if d.Pane != "" {
		if d.A != nil || d.B != nil {
			return nil, false
		}
		return &Leaf{Pane: d.Pane}, true
	}
	if d.A == nil || d.B == nil {
		return nil, false
	}
	a, okA := fromData(d.A)
	b, okB := fromData(d.B)
	if !okA || !okB {
		return nil, false
	}
	orient := Horizontal
	if d.Orient == "v" {
		orient = Vertical
	}
	ratio := clampRatio(d.Ratio)
	return &Split{Orient: orient, Ratio: ratio, A: a, B: b}, true
}
